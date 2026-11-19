package core

// Reliable stream over UDP, used after NAT hole punching (PiqLink 2.5).
//
// Every datagram starts with type(1) | tag(4). The tag is derived from the
// transfer code, so stray traffic is ignored. Above this transport the usual
// PiqLink channel (SPAKE2 + AES-256-GCM) runs unchanged, so these headers need
// no protection of their own: forged data would only break the authenticated
// stream, never inject content.
//
//   DATA  seq(4) ts(4) flags(1) payload      ts in microseconds; flags bit0 = FIN
//   ACK   cum(4) tsEcho(4) rwnd(4) n(1) n*(start(4) end(4))
//         cum = next expected seq; blocks = received ranges above cum
//   PING  flags(1)                           bit0 = pong
//   CLOSE (empty)                            the peer gave up / finished
//   PUNCH flags(1)                           bit0 = "I hear you"
//
// Sequence numbers count packets. Loss detection is time based (RACK) with a
// reordering window that grows when retransmissions turn out spurious.
// Congestion control follows BBR: it measures the delivery rate and the
// minimum RTT, paces packets at that rate and keeps about two
// bandwidth-delay products in flight. Random losses on radio links therefore
// do not slow it down; only the measured bandwidth does.

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sort"
	"sync"
	"time"
)

const (
	pktPunch = 0x50
	pktData  = 0x51
	pktAck   = 0x52
	pktPing  = 0x53
	pktClose = 0x54

	rudpHdr        = 5
	rudpDataHdr    = rudpHdr + 9
	rudpMaxPayload = 1180
	rudpMaxBlocks  = 32
	rudpRecvWindow = 8192 // packets (~9.6 MB)
	rudpMaxCwnd    = 8192
	rudpMinCwnd    = 16
	rudpSendBuf    = 4 << 20
	rudpMinRTO     = 250 * time.Millisecond
	rudpMaxRTO     = 4 * time.Second
	rudpPingEvery  = 2 * time.Second
	rudpIdleLimit  = 25 * time.Second
	rudpMaxTx      = 40
	rudpTick       = 2 * time.Millisecond

	bbrStartup  = 0
	bbrDrain    = 1
	bbrProbe    = 2
	bbrHighGain = 2.885
	bbrBwRounds = 10
)

var bbrCycle = [8]float64{1.25, 0.75, 1, 1, 1, 1, 1, 1}

var (
	errRudpClosed = errors.New("connessione chiusa")
	// ErrConnLost: no news from the other device for too long.
	ErrConnLost = errors.New("connection lost")
)

type rudpTimeout struct{}

func (rudpTimeout) Error() string   { return "i/o timeout" }
func (rudpTimeout) Timeout() bool   { return true }
func (rudpTimeout) Temporary() bool { return true }

type outPkt struct {
	seq    uint32
	data   []byte // payload (nil for FIN)
	fin    bool
	sentAt time.Time
	tx     int
	sacked bool
	lost   bool
	// delivery rate sampling (BBR)
	delivered     int64
	deliveredTime time.Time
	firstSent     time.Time
	appLimited    bool
}

type seqRange struct{ start, end uint32 } // [start, end)

// rudpConn implements net.Conn over a UDP socket shared with a demultiplexer.
type rudpConn struct {
	pc    net.PacketConn
	peer  net.Addr
	tag   [4]byte
	epoch time.Time

	mu   sync.Mutex
	wake chan struct{} // sender loop
	rd   chan struct{} // readers
	wr   chan struct{} // writers

	// sender
	nextSeq   uint32
	sendBuf   []byte
	finQueued bool
	finSeq    uint32
	inflight  []*outPkt // ordered by seq
	nSacked   int
	nLost     int
	srtt      time.Duration
	rttvar    time.Duration
	rto       time.Duration
	peerWnd   int
	lastSend  time.Time
	ackedAll  chan struct{}
	reoFactor int // reordering window in eighths of the min RTT
	reoCalm   int // rounds without spurious retransmissions

	// congestion control (BBR)
	cwnd          float64
	delivered     int64
	deliveredTime time.Time
	firstSent     time.Time // send time of the latest delivered packet
	appLimited    bool
	minRTT        time.Duration
	minRTTStamp   time.Time
	bw            [bbrBwRounds]float64 // max delivery rate (packets/s) per round
	round         int
	roundEnd      uint32
	state         int
	fullBw        float64
	fullBwRounds  int
	cycle         int
	cycleStart    time.Time
	tokens        float64
	lastPace      time.Time
	sentRound     int
	lostRound     int

	// receiver
	expect   uint32
	ooo      map[uint32][]byte
	blocks   []seqRange
	readBuf  []byte
	finAt    int64 // seq of FIN, -1 unknown
	gotFin   bool
	ackDue   int
	lastTs   uint32
	lastRecv time.Time
	sentWnd  int

	err       error
	closed    bool
	rdDead    time.Time
	wrDead    time.Time
	done      chan struct{}
	closeOnce sync.Once
	ownsPC    bool

	statRetx, statRTO, statSpurious int
}

func newRudpConn(pc net.PacketConn, peer net.Addr, tag [4]byte, ownsPC bool) *rudpConn {
	now := time.Now()
	c := &rudpConn{
		pc: pc, peer: peer, tag: tag, epoch: now,
		wake: make(chan struct{}, 1), rd: make(chan struct{}, 1), wr: make(chan struct{}, 1),
		cwnd: rudpMinCwnd, rto: time.Second, peerWnd: rudpRecvWindow, reoFactor: 2,
		ooo: map[uint32][]byte{}, finAt: -1, lastRecv: now, deliveredTime: now, firstSent: now,
		done: make(chan struct{}), ownsPC: ownsPC, sentWnd: rudpRecvWindow,
	}
	go c.loop()
	return c
}

func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// now32 is the timestamp sent with data packets: microseconds, wrapping.
func (c *rudpConn) now32() uint32 {
	us := uint32(time.Since(c.epoch) / time.Microsecond)
	if us == 0 {
		us = 1
	}
	return us
}

func (c *rudpConn) header(t byte, extra int) []byte {
	b := make([]byte, rudpHdr, rudpHdr+extra)
	b[0] = t
	copy(b[1:5], c.tag[:])
	return b
}

func (c *rudpConn) send(b []byte) { c.pc.WriteTo(b, c.peer) }

// ---------------------------------------------------------------- input

// input handles one datagram from the peer (called by the demultiplexer).
func (c *rudpConn) input(b []byte, from net.Addr) {
	if len(b) < rudpHdr {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.lastRecv = time.Now()
	switch b[0] {
	case pktData:
		if len(b) < rudpDataHdr {
			return
		}
		seq := binary.BigEndian.Uint32(b[5:])
		ts := binary.BigEndian.Uint32(b[9:])
		fin := b[13]&1 != 0
		c.onData(seq, ts, fin, b[rudpDataHdr:])
	case pktAck:
		c.onAck(b[rudpHdr:])
	case pktPing:
		if len(b) > rudpHdr && b[rudpHdr]&1 == 0 {
			p := c.header(pktPing, 1)
			c.send(append(p, 1))
		}
	case pktPunch:
		// the peer may not have seen our last punch: confirm again
		p := c.header(pktPunch, 1)
		c.send(append(p, 1))
	case pktClose:
		if c.gotFin || c.finQueued {
			c.failLocked(errRudpClosed) // normal end
		} else {
			c.failLocked(errors.New(Tr("connessione interrotta dall'altro dispositivo", "connection closed by the other device")))
		}
	}
}

func seqLess(a, b uint32) bool { return int32(a-b) < 0 }

func (c *rudpConn) onData(seq, ts uint32, fin bool, payload []byte) {
	c.lastTs = ts
	switch {
	case seqLess(seq, c.expect):
		c.sendAckLocked() // duplicate: our ack was lost
		return
	case int(seq-c.expect) >= rudpRecvWindow:
		return
	}
	if fin {
		c.finAt = int64(seq)
	}
	if seq == c.expect {
		c.deliver(payload, fin)
		for len(c.blocks) > 0 && c.blocks[0].start == c.expect {
			blk := c.blocks[0]
			for s := blk.start; s != blk.end; s++ {
				c.deliver(c.ooo[s], int64(s) == c.finAt)
				delete(c.ooo, s)
			}
			c.blocks = c.blocks[1:]
		}
		signal(c.rd)
		if len(c.blocks) == 0 && !fin {
			c.ackDue++
			if c.ackDue >= 2 {
				c.sendAckLocked()
			}
			return
		}
		c.sendAckLocked()
		return
	}
	if _, dup := c.ooo[seq]; !dup {
		c.ooo[seq] = append([]byte(nil), payload...)
		c.addBlock(seq)
	}
	c.sendAckLocked()
}

func (c *rudpConn) deliver(payload []byte, fin bool) {
	c.readBuf = append(c.readBuf, payload...)
	c.expect++
	if fin {
		c.gotFin = true
	}
}

func (c *rudpConn) addBlock(seq uint32) {
	i := sort.Search(len(c.blocks), func(i int) bool { return !seqLess(c.blocks[i].end, seq) })
	if i < len(c.blocks) {
		b := &c.blocks[i]
		if !seqLess(seq, b.start) && seqLess(seq, b.end) {
			return
		}
		if b.end == seq {
			b.end++
			if i+1 < len(c.blocks) && c.blocks[i+1].start == b.end {
				b.end = c.blocks[i+1].end
				c.blocks = append(c.blocks[:i+1], c.blocks[i+2:]...)
			}
			return
		}
		if b.start == seq+1 {
			b.start = seq
			return
		}
	}
	c.blocks = append(c.blocks, seqRange{})
	copy(c.blocks[i+1:], c.blocks[i:])
	c.blocks[i] = seqRange{seq, seq + 1}
}

func (c *rudpConn) recvWindow() int {
	w := rudpRecvWindow - len(c.ooo) - len(c.readBuf)/rudpMaxPayload
	if w < 0 {
		w = 0
	}
	return w
}

func (c *rudpConn) sendAckLocked() {
	c.ackDue = 0
	n := len(c.blocks)
	if n > rudpMaxBlocks {
		n = rudpMaxBlocks
	}
	p := c.header(pktAck, 13+8*n)
	var tmp [13]byte
	binary.BigEndian.PutUint32(tmp[0:], c.expect)
	binary.BigEndian.PutUint32(tmp[4:], c.lastTs)
	w := c.recvWindow()
	c.sentWnd = w
	binary.BigEndian.PutUint32(tmp[8:], uint32(w))
	tmp[12] = byte(n)
	p = append(p, tmp[:]...)
	add := func(r seqRange) {
		var x [8]byte
		binary.BigEndian.PutUint32(x[0:], r.start)
		binary.BigEndian.PutUint32(x[4:], r.end)
		p = append(p, x[:]...)
	}
	if len(c.blocks) <= rudpMaxBlocks {
		for _, r := range c.blocks {
			add(r)
		}
	} else { // the lowest ones plus the highest (for loss detection)
		for _, r := range c.blocks[:rudpMaxBlocks-1] {
			add(r)
		}
		add(c.blocks[len(c.blocks)-1])
	}
	c.send(p)
}

// ---------------------------------------------------------------- acks

func (c *rudpConn) onAck(b []byte) {
	if len(b) < 13 {
		return
	}
	cum := binary.BigEndian.Uint32(b[0:])
	tsEcho := binary.BigEndian.Uint32(b[4:])
	c.peerWnd = int(binary.BigEndian.Uint32(b[8:]))
	n := int(b[12])
	if len(b) < 13+8*n {
		return
	}
	now := time.Now()
	newly := 0
	high := cum - 1 // highest delivered seq
	var rack time.Time
	var rs *outPkt // most recently sent among the newly delivered (rate sample)
	spurious := false
	got := func(p *outPkt) {
		newly++
		if p.sentAt.After(rack) {
			rack = p.sentAt
		}
		if rs == nil || p.sentAt.After(rs.sentAt) {
			rs = p
		}
		// delivered almost at once after a retransmission: the original
		// copy arrived, it was only late (reordering)
		if p.tx > 1 && c.minRTT > 0 && now.Sub(p.sentAt) < c.minRTT/2 {
			spurious = true
		}
	}
	for len(c.inflight) > 0 && seqLess(c.inflight[0].seq, cum) {
		p := c.inflight[0]
		if p.sacked {
			c.nSacked--
		} else {
			if p.lost {
				c.nLost--
			}
			got(p)
		}
		c.inflight[0] = nil
		c.inflight = c.inflight[1:]
	}
	for k := 0; k < n; k++ {
		s := binary.BigEndian.Uint32(b[13+8*k:])
		e := binary.BigEndian.Uint32(b[17+8*k:])
		if seqLess(high, e-1) {
			high = e - 1
		}
		i := sort.Search(len(c.inflight), func(i int) bool { return !seqLess(c.inflight[i].seq, s) })
		for ; i < len(c.inflight) && seqLess(c.inflight[i].seq, e); i++ {
			p := c.inflight[i]
			if !p.sacked {
				p.sacked = true
				c.nSacked++
				if p.lost {
					p.lost = false
					c.nLost--
				}
				got(p)
			}
		}
	}
	if tsEcho != 0 {
		if rtt := time.Duration(c.now32()-tsEcho) * time.Microsecond; rtt < time.Minute {
			c.rttSample(rtt, now)
		}
	}
	if spurious {
		c.statSpurious++
		c.reoCalm = 0
		if c.reoFactor < 8 {
			c.reoFactor++
		}
	}
	// loss detection (RACK): sent before a delivered packet, beyond the
	// reordering window
	if !rack.IsZero() {
		reo := c.minRTT * time.Duration(c.reoFactor) / 8
		if reo < 200*time.Microsecond {
			reo = 200 * time.Microsecond
		}
		limit := rack.Add(-reo)
		for _, p := range c.inflight {
			if !seqLess(p.seq, high) {
				break
			}
			if !p.sacked && !p.lost && p.sentAt.Before(limit) {
				p.lost = true
				c.nLost++
				c.lostRound++
			}
		}
	}
	if newly > 0 {
		c.delivered += int64(newly)
		c.deliveredTime = now
		c.bbrUpdate(rs, now, high)
		signal(c.wr)
	}
	if len(c.inflight) == 0 && c.ackedAll != nil && len(c.sendBuf) == 0 && c.finQueued && !seqLess(cum, c.finSeq+1) {
		close(c.ackedAll)
		c.ackedAll = nil
	}
	signal(c.wake)
}

func (c *rudpConn) rttSample(r time.Duration, now time.Time) {
	if r <= 0 {
		r = 10 * time.Microsecond
	}
	if c.minRTT == 0 || r < c.minRTT || now.Sub(c.minRTTStamp) > 10*time.Second {
		c.minRTT, c.minRTTStamp = r, now
	}
	if c.srtt == 0 {
		c.srtt, c.rttvar = r, r/2
	} else {
		d := c.srtt - r
		if d < 0 {
			d = -d
		}
		c.rttvar = (3*c.rttvar + d) / 4
		c.srtt = (7*c.srtt + r) / 8
	}
	c.rto = c.srtt + 4*c.rttvar
	if c.rto < rudpMinRTO {
		c.rto = rudpMinRTO
	}
	if c.rto > rudpMaxRTO {
		c.rto = rudpMaxRTO
	}
}

// ---------------------------------------------------------------- BBR

func (c *rudpConn) btlBw() float64 {
	m := 0.0
	for _, v := range c.bw {
		if v > m {
			m = v
		}
	}
	return m
}

func (c *rudpConn) bdp() float64 { return c.btlBw() * c.minRTT.Seconds() }

func (c *rudpConn) pacingGain() float64 {
	switch c.state {
	case bbrStartup:
		return bbrHighGain
	case bbrDrain:
		return 1 / bbrHighGain
	}
	return bbrCycle[c.cycle]
}

func (c *rudpConn) bbrUpdate(rs *outPkt, now time.Time, high uint32) {
	// delivery rate sample: the longer of the send and the ack intervals,
	// so bursts of acks do not inflate the estimate
	if rs != nil {
		el := now.Sub(rs.deliveredTime)
		if snd := rs.sentAt.Sub(rs.firstSent); snd > el {
			el = snd
		}
		c.firstSent = rs.sentAt
		// shorter than the min RTT: ack compression, not a real rate
		if el > 0 && el >= c.minRTT {
			rate := float64(c.delivered-rs.delivered) / el.Seconds()
			slot := &c.bw[c.round%bbrBwRounds]
			if rate > *slot && (!rs.appLimited || rate > c.btlBw()) {
				*slot = rate
			}
		}
	}
	// a round ends when a packet sent after the previous round end is delivered
	if !seqLess(high, c.roundEnd) {
		if c.sentRound > 20 && float64(c.lostRound) > 0.15*float64(c.sentRound) {
			// heavy loss: the measured rate is too optimistic
			for i := range c.bw {
				c.bw[i] *= 0.85
			}
		}
		c.round++
		c.roundEnd = c.nextSeq
		c.bw[c.round%bbrBwRounds] = 0
		c.sentRound, c.lostRound = 0, 0
		if c.reoCalm++; c.reoCalm > 16 && c.reoFactor > 2 {
			c.reoFactor--
			c.reoCalm = 0
		}
		if c.state == bbrStartup {
			if bw := c.btlBw(); bw >= c.fullBw*1.25 {
				c.fullBw, c.fullBwRounds = bw, 0
			} else if c.fullBwRounds++; c.fullBwRounds >= 3 {
				c.state = bbrDrain
			}
		}
	}
	if c.state == bbrDrain && float64(c.pipe()) <= c.bdp() {
		c.state, c.cycle, c.cycleStart = bbrProbe, 2, now
	}
	if c.state == bbrProbe && c.minRTT > 0 && now.Sub(c.cycleStart) > c.minRTT {
		c.cycle = (c.cycle + 1) % len(bbrCycle)
		c.cycleStart = now
	}
	// congestion window: about two bandwidth-delay products
	if bdp := c.bdp(); bdp > 0 {
		gain := 2.0
		if c.state != bbrProbe {
			gain = bbrHighGain
		}
		c.cwnd = gain*bdp + 4
	}
	if c.cwnd < rudpMinCwnd {
		c.cwnd = rudpMinCwnd
	}
	if c.cwnd > rudpMaxCwnd {
		c.cwnd = rudpMaxCwnd
	}
}

// ---------------------------------------------------------------- sender

func (c *rudpConn) pipe() int { return len(c.inflight) - c.nSacked - c.nLost }

func (c *rudpConn) transmit(p *outPkt, now time.Time) {
	b := c.header(pktData, 9+len(p.data))
	var tmp [9]byte
	binary.BigEndian.PutUint32(tmp[0:], p.seq)
	binary.BigEndian.PutUint32(tmp[4:], c.now32())
	if p.fin {
		tmp[8] = 1
	}
	b = append(append(b, tmp[:]...), p.data...)
	if p.lost {
		p.lost = false
		c.nLost--
	}
	if p.tx > 0 {
		c.statRetx++
	}
	if len(c.inflight) == 0 || (len(c.inflight) == 1 && c.inflight[0] == p && p.tx == 0) {
		// restarting after idle: do not count the idle time in the rate
		c.deliveredTime, c.firstSent = now, now
	}
	p.sentAt = now
	p.tx++
	p.delivered, p.deliveredTime, p.firstSent, p.appLimited = c.delivered, c.deliveredTime, c.firstSent, c.appLimited
	c.sentRound++
	c.lastSend = now
	c.tokens--
	c.send(b)
}

// pump sends what the windows and the pacing allow; called with the lock held.
func (c *rudpConn) pump(now time.Time) {
	if bw := c.btlBw(); bw > 0 {
		rate := c.pacingGain() * bw
		if !c.lastPace.IsZero() {
			c.tokens += rate * now.Sub(c.lastPace).Seconds()
		}
		burst := rate * 0.004 // 4 ms worth of packets
		if burst < 4 {
			burst = 4
		}
		if c.tokens > burst {
			c.tokens = burst
		}
	} else {
		c.tokens = c.cwnd // no estimate yet: the window alone
	}
	c.lastPace = now
	limit := int(c.cwnd)
	if c.peerWnd < limit {
		limit = c.peerWnd
	}
	if limit < 1 {
		limit = 1 // probe a closed window
	}
	pipe := c.pipe()
	for _, p := range c.inflight {
		if c.nLost == 0 || pipe >= limit || c.tokens < 1 {
			break
		}
		if p.lost {
			if p.tx >= rudpMaxTx {
				c.failLocked(ErrConnLost)
				return
			}
			c.transmit(p, now)
			pipe++
		}
	}
	for pipe < limit && c.tokens >= 1 && int(c.nextSeq-c.ackBase()) < rudpRecvWindow {
		var p *outPkt
		switch {
		case len(c.sendBuf) > 0:
			n := len(c.sendBuf)
			if n > rudpMaxPayload {
				n = rudpMaxPayload
			}
			p = &outPkt{seq: c.nextSeq, data: append([]byte(nil), c.sendBuf[:n]...)}
			c.sendBuf = c.sendBuf[n:]
			if len(c.sendBuf) == 0 {
				c.sendBuf = nil
			}
			signal(c.wr)
		case c.finQueued && c.finSeq == c.nextSeq:
			p = &outPkt{seq: c.nextSeq, fin: true}
		default:
			c.appLimited = pipe < limit
			return
		}
		c.appLimited = false
		c.nextSeq++
		c.inflight = append(c.inflight, p)
		c.transmit(p, now)
		pipe++
	}
}

func (c *rudpConn) ackBase() uint32 {
	if len(c.inflight) > 0 {
		return c.inflight[0].seq
	}
	return c.nextSeq
}

func (c *rudpConn) loop() {
	tick := time.NewTicker(rudpTick)
	defer tick.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-c.wake:
		case <-tick.C:
		}
		c.mu.Lock()
		if c.err != nil {
			c.mu.Unlock()
			return
		}
		now := time.Now()
		if c.ackDue > 0 {
			c.sendAckLocked()
		} else if c.sentWnd < rudpRecvWindow/4 && c.recvWindow() > rudpRecvWindow/2 {
			c.sendAckLocked() // the application read: reopen the window
		}
		// retransmission timeout on the oldest packet still missing
		for _, p := range c.inflight {
			if p.sacked || p.lost {
				continue
			}
			if now.Sub(p.sentAt) > c.rto {
				c.statRTO++
				c.rto *= 2
				if c.rto > rudpMaxRTO {
					c.rto = rudpMaxRTO
				}
				for _, q := range c.inflight {
					if !q.sacked && !q.lost {
						q.lost = true
						c.nLost++
					}
				}
				c.cwnd = rudpMinCwnd
			}
			break
		}
		c.pump(now)
		if now.Sub(c.lastRecv) > rudpIdleLimit {
			c.failLocked(ErrConnLost)
		} else if now.Sub(c.lastSend) > rudpPingEvery {
			c.lastSend = now
			p := c.header(pktPing, 1)
			c.send(append(p, 0))
		}
		c.mu.Unlock()
	}
}

func (c *rudpConn) failLocked(err error) {
	if c.err == nil {
		c.err = err
	}
	c.closed = true
	if c.ackedAll != nil {
		close(c.ackedAll)
		c.ackedAll = nil
	}
	c.closeOnce.Do(func() { close(c.done) })
	signal(c.rd)
	signal(c.wr)
}

// ---------------------------------------------------------------- net.Conn

func (c *rudpConn) Read(b []byte) (int, error) {
	for {
		c.mu.Lock()
		if len(c.readBuf) > 0 {
			n := copy(b, c.readBuf)
			c.readBuf = c.readBuf[n:]
			if len(c.readBuf) == 0 {
				c.readBuf = nil
			}
			if c.sentWnd < rudpRecvWindow/4 {
				signal(c.wake)
			}
			c.mu.Unlock()
			return n, nil
		}
		if c.gotFin {
			c.mu.Unlock()
			return 0, io.EOF
		}
		if c.err != nil {
			err := c.err
			c.mu.Unlock()
			return 0, err
		}
		dl := c.rdDead
		c.mu.Unlock()
		if err := waitSignal(c.rd, dl); err != nil {
			return 0, err
		}
	}
}

func waitSignal(ch chan struct{}, dl time.Time) error {
	if dl.IsZero() {
		<-ch
		return nil
	}
	d := time.Until(dl)
	if d <= 0 {
		return rudpTimeout{}
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ch:
		return nil
	case <-t.C:
		return rudpTimeout{}
	}
}

func (c *rudpConn) Write(b []byte) (int, error) {
	total := 0
	for len(b) > 0 {
		c.mu.Lock()
		if c.err != nil || c.finQueued {
			err := c.err
			if err == nil {
				err = errRudpClosed
			}
			c.mu.Unlock()
			return total, err
		}
		room := rudpSendBuf - len(c.sendBuf)
		if room > 0 {
			n := len(b)
			if n > room {
				n = room
			}
			c.sendBuf = append(c.sendBuf, b[:n]...)
			b = b[n:]
			total += n
			signal(c.wake)
			c.mu.Unlock()
			continue
		}
		dl := c.wrDead
		c.mu.Unlock()
		if err := waitSignal(c.wr, dl); err != nil {
			return total, err
		}
	}
	return total, nil
}

// Close sends what is still queued, waits (briefly) for it to be
// acknowledged, then releases the socket.
func (c *rudpConn) Close() error {
	c.mu.Lock()
	if c.err == nil && !c.finQueued && c.gotFin && len(c.sendBuf) == 0 && len(c.inflight) == 0 {
		// the peer finished and we have nothing left: just say goodbye
		c.finQueued = true
	} else if c.err == nil && !c.finQueued {
		c.finQueued = true
		c.finSeq = c.nextSeq + uint32((len(c.sendBuf)+rudpMaxPayload-1)/rudpMaxPayload)
		ch := make(chan struct{})
		c.ackedAll = ch
		signal(c.wake)
		c.mu.Unlock()
		select {
		case <-ch:
		case <-time.After(8 * time.Second):
		}
		c.mu.Lock()
	}
	if c.err == nil || c.err == errRudpClosed {
		p := c.header(pktClose, 0)
		c.send(p)
		c.send(p)
	}
	c.failLocked(errRudpClosed)
	c.mu.Unlock()
	if c.ownsPC {
		c.pc.Close()
	}
	return nil
}

// abort drops the connection at once (cancel).
func (c *rudpConn) abort() {
	c.mu.Lock()
	if !c.closed {
		c.send(c.header(pktClose, 0))
	}
	c.failLocked(errRudpClosed)
	c.mu.Unlock()
	if c.ownsPC {
		c.pc.Close()
	}
}

func (c *rudpConn) LocalAddr() net.Addr  { return c.pc.LocalAddr() }
func (c *rudpConn) RemoteAddr() net.Addr { return c.peer }
func (c *rudpConn) SetDeadline(t time.Time) error {
	c.SetReadDeadline(t)
	return c.SetWriteDeadline(t)
}
func (c *rudpConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.rdDead = t
	c.mu.Unlock()
	signal(c.rd)
	return nil
}
func (c *rudpConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	c.wrDead = t
	c.mu.Unlock()
	signal(c.wr)
	return nil
}
