package core

// Assisted mode (2.5): STUN + UDP hole punching.
//
// The device showing the code asks two public STUN servers how it is seen
// from the internet and puts that endpoint in a 14-character code. The device
// typing the code does the same, then sends "punch" packets to it and shows a
// reply code with its own endpoint. As soon as packets flow both ways, the
// reliable UDP transport starts and the usual PiqLink handshake runs on top.
// When the router of the code's owner lets the first punches through, the
// reply code is not needed at all.
//
// The STUN servers only see the IP address. They are contacted only with the
// user's consent (SetSTUNAllowed).

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// STUNServers are asked, in parallel, for the public endpoint. Two different
// providers also tell whether the NAT keeps the same port for every
// destination (needed for hole punching).
var STUNServers = []string{"stun.l.google.com:19302", "stun.cloudflare.com:3478", "stun1.l.google.com:19302"}

// test hooks
var (
	stunResolve  = func(hostport string) (*net.UDPAddr, error) { return net.ResolveUDPAddr("udp4", hostport) }
	newP2PSocket = func() (net.PacketConn, error) {
		pc, err := net.ListenPacket("udp4", ":0")
		if err == nil {
			bigBuffers(pc)
		}
		return pc, err
	}
	punchInterval = 150 * time.Millisecond
)

// bigBuffers enlarges the socket buffers (the Windows default is 64 KB,
// far too little for fast links).
func bigBuffers(pc net.PacketConn) {
	if u, ok := pc.(*net.UDPConn); ok {
		u.SetReadBuffer(4 << 20)
		u.SetWriteBuffer(4 << 20)
	}
}

// PunchTimeout is how long the device that typed a P2P code keeps trying.
const PunchTimeout = 5 * time.Minute

var stunAllowed atomic.Bool

// SetSTUNAllowed records the user's consent to the assisted mode.
func SetSTUNAllowed(b bool) { stunAllowed.Store(b) }

// STUNAllowed reports whether the user agreed to the assisted mode.
func STUNAllowed() bool { return stunAllowed.Load() }

var (
	// ErrNeedConsent: the assisted mode needs the user's consent first.
	ErrNeedConsent = errors.New("assisted mode needs consent")
	// ErrNoSTUN: no STUN server answered.
	ErrNoSTUN = errors.New("no STUN server")
	// ErrPunch: the two devices could not reach each other.
	ErrPunch = errors.New("hole punching failed")
)

// NATInfo describes how this device is seen from the internet.
type NATInfo struct {
	Public    *net.UDPAddr
	Symmetric bool // the NAT changes port for every destination
	Checked   bool // two different servers answered, so Symmetric is meaningful
}

func p2pTag(code string) [4]byte {
	h := sha256.Sum256([]byte("PiqLink-p2p-v1|" + code))
	var t [4]byte
	copy(t[:], h[:4])
	return t
}

type punchPkt struct {
	from  net.Addr
	heard bool
	raw   []byte // a transport packet received while punching
}

// udpMux owns the UDP socket: STUN answers, punches, then the transport.
type udpMux struct {
	pc net.PacketConn

	mu     sync.Mutex
	tag    [4]byte
	tagSet bool
	stun   map[[12]byte]chan *net.UDPAddr
	punch  chan punchPkt
	conn   *rudpConn
	closed bool
}

func newUDPMux(pc net.PacketConn) *udpMux {
	m := &udpMux{pc: pc, stun: map[[12]byte]chan *net.UDPAddr{}, punch: make(chan punchPkt, 64)}
	go m.readLoop()
	return m
}

func (m *udpMux) setTag(t [4]byte) {
	m.mu.Lock()
	m.tag, m.tagSet = t, true
	m.mu.Unlock()
}

func (m *udpMux) Close() {
	m.mu.Lock()
	c := m.conn
	m.closed = true
	m.mu.Unlock()
	if c != nil {
		c.abort()
	}
	m.pc.Close()
}

const stunCookie = 0x2112A442

func (m *udpMux) readLoop() {
	buf := make([]byte, 2048)
	for {
		n, from, err := m.pc.ReadFrom(buf)
		if err != nil {
			m.mu.Lock()
			c := m.conn
			m.mu.Unlock()
			if c != nil {
				c.mu.Lock()
				c.failLocked(ErrConnLost)
				c.mu.Unlock()
			}
			return
		}
		b := buf[:n]
		if n >= 20 && b[0] < 2 && binary.BigEndian.Uint32(b[4:]) == stunCookie {
			m.onSTUN(b)
			continue
		}
		if n < rudpHdr {
			continue
		}
		m.mu.Lock()
		ok := m.tagSet && string(b[1:5]) == string(m.tag[:])
		c := m.conn
		m.mu.Unlock()
		if !ok {
			continue
		}
		if c != nil {
			if from.String() == c.peer.String() {
				c.input(b, from)
			} else if b[0] == pktPunch {
				// late punches from another port of the peer: confirm anyway
				p := c.header(pktPunch, 1)
				m.pc.WriteTo(append(p, 1), from)
			}
			continue
		}
		pp := punchPkt{from: from}
		switch b[0] {
		case pktPunch:
			pp.heard = n > rudpHdr && b[rudpHdr]&1 != 0
		case pktData, pktAck, pktPing:
			pp.heard = true // the peer is already past punching
			pp.raw = append([]byte(nil), b...)
		default:
			continue
		}
		select {
		case m.punch <- pp:
		default:
		}
	}
}

// ---------------------------------------------------------------- STUN

func (m *udpMux) onSTUN(b []byte) {
	if binary.BigEndian.Uint16(b[0:]) != 0x0101 {
		return
	}
	var id [12]byte
	copy(id[:], b[8:20])
	m.mu.Lock()
	ch := m.stun[id]
	m.mu.Unlock()
	if ch == nil {
		return
	}
	if a := parseSTUN(b); a != nil {
		select {
		case ch <- a:
		default:
		}
	}
}

func parseSTUN(b []byte) *net.UDPAddr {
	l := int(binary.BigEndian.Uint16(b[2:]))
	if 20+l > len(b) {
		return nil
	}
	attrs := b[20 : 20+l]
	var mapped *net.UDPAddr
	for len(attrs) >= 4 {
		t := binary.BigEndian.Uint16(attrs[0:])
		al := int(binary.BigEndian.Uint16(attrs[2:]))
		if 4+al > len(attrs) {
			break
		}
		v := attrs[4 : 4+al]
		if al >= 8 && v[1] == 0x01 { // IPv4
			port := int(binary.BigEndian.Uint16(v[2:]))
			ip := make(net.IP, 4)
			copy(ip, v[4:8])
			switch t {
			case 0x0020, 0x8020: // XOR-MAPPED-ADDRESS
				port ^= stunCookie >> 16
				x := binary.BigEndian.Uint32(ip) ^ stunCookie
				binary.BigEndian.PutUint32(ip, x)
				return &net.UDPAddr{IP: ip, Port: port}
			case 0x0001: // MAPPED-ADDRESS
				mapped = &net.UDPAddr{IP: ip, Port: port}
			}
		}
		attrs = attrs[4+((al+3)&^3):]
	}
	return mapped
}

// stunQuery asks one server for our public endpoint.
func (m *udpMux) stunQuery(ctx context.Context, server *net.UDPAddr) (*net.UDPAddr, error) {
	var id [12]byte
	rand.Read(id[:])
	req := make([]byte, 20)
	binary.BigEndian.PutUint16(req[0:], 0x0001)
	binary.BigEndian.PutUint32(req[4:], stunCookie)
	copy(req[8:], id[:])
	ch := make(chan *net.UDPAddr, 1)
	m.mu.Lock()
	m.stun[id] = ch
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.stun, id)
		m.mu.Unlock()
	}()
	for try := 0; try < 4; try++ {
		m.pc.WriteTo(req, server)
		t := time.NewTimer(400 * time.Millisecond)
		select {
		case a := <-ch:
			t.Stop()
			return a, nil
		case <-ctx.Done():
			t.Stop()
			return nil, ctx.Err()
		case <-t.C:
		}
	}
	return nil, ErrNoSTUN
}

// discover finds the public endpoint and whether the NAT is symmetric.
func (m *udpMux) discover(ctx context.Context) (NATInfo, error) {
	type res struct {
		server *net.UDPAddr
		addr   *net.UDPAddr
	}
	servers := STUNServers
	out := make(chan res, len(servers))
	for _, s := range servers {
		go func(s string) {
			sa, err := stunResolve(s)
			if err != nil {
				out <- res{}
				return
			}
			a, err := m.stunQuery(ctx, sa)
			if err != nil {
				out <- res{}
				return
			}
			out <- res{sa, a}
		}(s)
	}
	var got []res
	deadline := time.NewTimer(2500 * time.Millisecond)
	defer deadline.Stop()
wait:
	for range servers {
		select {
		case r := <-out:
			if r.addr != nil {
				got = append(got, r)
				if len(got) >= 2 && !got[0].server.IP.Equal(got[1].server.IP) {
					break wait
				}
			}
		case <-deadline.C:
			break wait
		case <-ctx.Done():
			return NATInfo{}, ErrCancelled
		}
	}
	if len(got) == 0 {
		return NATInfo{}, ErrNoSTUN
	}
	info := NATInfo{Public: got[0].addr}
	for _, r := range got[1:] {
		if !r.server.IP.Equal(got[0].server.IP) {
			info.Checked = true
			if r.addr.Port != got[0].addr.Port || !r.addr.IP.Equal(got[0].addr.IP) {
				info.Symmetric = true
			}
		}
	}
	return info, nil
}

// ---------------------------------------------------------------- punching

func (m *udpMux) sendPunch(to net.Addr, heard bool) {
	p := make([]byte, rudpHdr+1)
	p[0] = pktPunch
	m.mu.Lock()
	copy(p[1:5], m.tag[:])
	m.mu.Unlock()
	if heard {
		p[rudpHdr] = 1
	}
	m.pc.WriteTo(p, to)
}

// punchThrough sends punches to the known targets (more may arrive on
// extra) until packets flow both ways, then starts the transport.
func (m *udpMux) punchThrough(ctx context.Context, targets []*net.UDPAddr, extra <-chan *net.UDPAddr, deadline time.Time) (*rudpConn, error) {
	tick := time.NewTicker(punchInterval)
	defer tick.Stop()
	end := time.NewTimer(time.Until(deadline))
	defer end.Stop()
	var peer net.Addr
	var pending []byte
	send := func() {
		if peer != nil {
			m.sendPunch(peer, true)
			return
		}
		for _, t := range targets {
			m.sendPunch(t, false)
		}
	}
	send()
	for {
		select {
		case <-ctx.Done():
			return nil, ErrCancelled
		case <-end.C:
			return nil, ErrPunch
		case t := <-extra:
			if t != nil {
				targets = append(targets, t)
				m.sendPunch(t, peer != nil)
			}
		case <-tick.C:
			send()
		case p := <-m.punch:
			if peer == nil || p.heard {
				peer = p.from
			}
			if p.raw != nil {
				pending = p.raw
			}
			m.sendPunch(peer, true)
			if !p.heard {
				continue
			}
			// packets flow both ways: confirm a few more times and start
			for i := 0; i < 3; i++ {
				m.sendPunch(peer, true)
			}
			m.mu.Lock()
			c := newRudpConn(m.pc, peer, m.tag, true)
			m.conn = c
			m.mu.Unlock()
			if pending != nil {
				c.input(pending, peer)
			}
			return c, nil
		}
	}
}

// ---------------------------------------------------------------- receiver side

// StartReceiverP2P prepares the assisted mode: asks the STUN servers for the
// public endpoint and creates a 14-character code. send: the reversed flow.
func StartReceiverP2P(ctx context.Context) (*Receiver, error) {
	if !STUNAllowed() {
		return nil, ErrNeedConsent
	}
	pc, err := newP2PSocket()
	if err != nil {
		return nil, err
	}
	m := newUDPMux(pc)
	info, err := m.discover(ctx)
	if err != nil {
		m.Close()
		if errors.Is(err, ErrNoSTUN) {
			return nil, errors.New(Tr("nessun server STUN raggiungibile: controlla la connessione a Internet", "no STUN server reachable: check the internet connection"))
		}
		return nil, err
	}
	code, err := NewP2PCode(info.Public)
	if err != nil {
		m.Close()
		return nil, err
	}
	m.setTag(p2pTag(code.String()))
	return &Receiver{
		Mode: ModeP2P, Code: code, Expires: time.Now().Add(CodeValidity),
		NAT: info, mux: m, replies: make(chan *net.UDPAddr, 8),
	}, nil
}

// SetReply passes the reply code typed by the user (assisted mode).
func (r *Receiver) SetReply(text string) error {
	if r.Mode != ModeP2P {
		return ErrBadCode
	}
	a, err := ParseReply(r.Code, text)
	if err != nil {
		return err
	}
	select {
	case r.replies <- a:
	default:
	}
	return nil
}

func (r *Receiver) acceptP2P(ctx context.Context) (*Channel, error) {
	conn, err := r.mux.punchThrough(ctx, nil, r.replies, r.Expires)
	if err != nil {
		if errors.Is(err, ErrPunch) {
			return nil, ErrExpired
		}
		return nil, err
	}
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			conn.abort()
		case <-stop:
		}
	}()
	ch, err := ServerHandshake(conn, r.Code.String())
	close(stop)
	if err != nil {
		conn.abort()
		if ctx.Err() != nil {
			return nil, ErrCancelled
		}
		return nil, err
	}
	return ch, nil
}

// ---------------------------------------------------------------- connector side

func connectP2P(ctx context.Context, code Code, ev Events) (*Channel, error) {
	if !STUNAllowed() {
		return nil, ErrNeedConsent
	}
	pc, err := newP2PSocket()
	if err != nil {
		return nil, err
	}
	m := newUDPMux(pc)
	m.setTag(p2pTag(code.String()))
	target := code.UDPAddr()
	// punch at once (an open router needs nothing else) while STUN answers
	m.sendPunch(target, false)
	info, err := m.discover(ctx)
	if err != nil {
		m.Close()
		if errors.Is(err, ErrNoSTUN) {
			return nil, errors.New(Tr("nessun server STUN raggiungibile: controlla la connessione a Internet", "no STUN server reachable: check the internet connection"))
		}
		return nil, err
	}
	reply, err := NewReplyCode(code, info.Public)
	if err != nil {
		m.Close()
		return nil, err
	}
	if ev.Reply != nil {
		ev.Reply(FormatReply(reply), info)
	}
	conn, err := m.punchThrough(ctx, []*net.UDPAddr{target}, nil, time.Now().Add(PunchTimeout))
	if err != nil {
		m.Close()
		if errors.Is(err, ErrPunch) {
			msg := Tr("collegamento non riuscito: i due router non si lasciano attraversare. Prova il codice inverso, o una rete diversa (per esempio il Wi-Fi).", "connection failed: the two routers cannot be crossed. Try the reversed code, or a different network (for example Wi-Fi).")
			if info.Public.IP.Equal(target.IP) {
				msg = Tr("collegamento non riuscito: i due dispositivi sembrano sulla stessa rete, usa «Stessa rete».", "connection failed: the two devices seem to be on the same network, use “Same network”.")
			}
			return nil, errors.New(msg)
		}
		return nil, err
	}
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			conn.abort()
		case <-stop:
		}
	}()
	ch, err := ClientHandshake(conn, code.String())
	close(stop)
	if err != nil {
		conn.abort()
		if ctx.Err() != nil {
			return nil, ErrCancelled
		}
		if errors.Is(err, ErrPake) {
			return nil, errors.New(Tr("codice errato", "wrong code"))
		}
		return nil, err
	}
	return ch, nil
}

// ---------------------------------------------------------------- network test

// NetReport is the result of the network test.
type NetReport struct {
	NAT       NATInfo
	STUNError string // empty when a STUN server answered
	UPnP      bool   // the router opens ports and has a public address
	UPnPInfo  string
}

// TestNetwork checks how this device can be reached (needs consent: it
// contacts the STUN servers).
func TestNetwork(ctx context.Context) (NetReport, error) {
	if !STUNAllowed() {
		return NetReport{}, ErrNeedConsent
	}
	var rep NetReport
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		addrs := LocalIPv4s()
		if len(addrs) == 0 {
			rep.UPnPInfo = Tr("nessuna rete", "no network")
			return
		}
		gw, err := DiscoverGateway(addrs[0].IP, 3*time.Second)
		if err != nil {
			rep.UPnPInfo = err.Error()
			return
		}
		ext, err := gw.ExternalIP()
		if err != nil {
			rep.UPnPInfo = err.Error()
			return
		}
		if !IsPublicIPv4(ext) {
			rep.UPnPInfo = Tr("indirizzo del router non pubblico", "router address not public") + " (" + ext.String() + ")"
			return
		}
		rep.UPnP = true
		rep.UPnPInfo = ext.String()
	}()
	pc, err := newP2PSocket()
	if err != nil {
		wg.Wait()
		return rep, err
	}
	m := newUDPMux(pc)
	info, err := m.discover(ctx)
	m.Close()
	wg.Wait()
	if err != nil {
		if errors.Is(err, ErrCancelled) {
			return rep, err
		}
		rep.STUNError = Tr("nessun server STUN raggiungibile", "no STUN server reachable")
	}
	rep.NAT = info
	return rep, nil
}
