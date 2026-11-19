package core

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	mrand "math/rand"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------- emulated link

// linkProfile emulates a path: bottleneck rate with a drop-tail queue,
// propagation delay, jitter and random loss.
type linkProfile struct {
	rate   float64       // bytes/s (0 = unlimited)
	queue  time.Duration // max queueing delay before drops
	delay  time.Duration // one-way propagation
	jitter time.Duration
	loss   float64
}

type lossyConn struct {
	net.PacketConn
	lp      linkProfile
	mu      sync.Mutex
	rnd     *mrand.Rand
	nextOut time.Time
}

func (l *lossyConn) WriteTo(b []byte, to net.Addr) (int, error) {
	now := time.Now()
	l.mu.Lock()
	if l.rnd.Float64() < l.lp.loss {
		l.mu.Unlock()
		return len(b), nil
	}
	dep := now
	if l.lp.rate > 0 {
		if l.nextOut.After(now) {
			dep = l.nextOut
		}
		if dep.Sub(now) > l.lp.queue {
			l.mu.Unlock()
			return len(b), nil // queue full: drop
		}
		dep = dep.Add(time.Duration(float64(len(b)+28) / l.lp.rate * float64(time.Second)))
		l.nextOut = dep
	}
	at := dep.Add(l.lp.delay)
	if l.lp.jitter > 0 {
		at = at.Add(time.Duration(l.rnd.Int63n(int64(l.lp.jitter))))
	}
	l.mu.Unlock()
	d := time.Until(at)
	if d <= 0 {
		return l.PacketConn.WriteTo(b, to)
	}
	cp := append([]byte(nil), b...)
	time.AfterFunc(d, func() { l.PacketConn.WriteTo(cp, to) })
	return len(b), nil
}

// Close lets the packets still "on the wire" arrive first.
func (l *lossyConn) Close() error {
	time.AfterFunc(2*(l.lp.delay+l.lp.jitter+l.lp.queue)+20*time.Millisecond, func() { l.PacketConn.Close() })
	return nil
}

func udpPair(t *testing.T, lp linkProfile) (*rudpConn, *rudpConn) {
	t.Helper()
	a, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	b, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	bigBuffers(a)
	bigBuffers(b)
	la := &lossyConn{PacketConn: a, lp: lp, rnd: mrand.New(mrand.NewSource(1))}
	lb := &lossyConn{PacketConn: b, lp: lp, rnd: mrand.New(mrand.NewSource(2))}
	tag := [4]byte{1, 2, 3, 4}
	ma := &udpMux{pc: la, stun: map[[12]byte]chan *net.UDPAddr{}, punch: make(chan punchPkt, 8), tag: tag, tagSet: true}
	mb := &udpMux{pc: lb, stun: map[[12]byte]chan *net.UDPAddr{}, punch: make(chan punchPkt, 8), tag: tag, tagSet: true}
	ca := newRudpConn(la, b.LocalAddr(), tag, true)
	cb := newRudpConn(lb, a.LocalAddr(), tag, true)
	ma.conn, mb.conn = ca, cb
	go ma.readLoop()
	go mb.readLoop()
	return ca, cb
}

func testStream(t *testing.T, name string, lp linkProfile, size int) {
	ca, cb := udpPair(t, lp)
	data := make([]byte, size)
	rand.Read(data)
	start := time.Now()
	errc := make(chan error, 1)
	go func() {
		_, err := ca.Write(data)
		if err == nil {
			err = ca.Close()
		}
		errc <- err
	}()
	got, err := io.ReadAll(cb)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	tRead := time.Since(start)
	if err := <-errc; err != nil {
		t.Fatalf("write: %v", err)
	}
	tClose := time.Since(start)
	cb.Close()
	t.Logf("read %v, sender closed %v, receiver closed %v", tRead.Round(time.Millisecond), tClose.Round(time.Millisecond), time.Since(start).Round(time.Millisecond))
	if !bytes.Equal(got, data) {
		t.Fatalf("data differs: got %d bytes, want %d", len(got), len(data))
	}
	el := time.Since(start)
	speed := float64(size) / el.Seconds()
	util := ""
	if lp.rate > 0 {
		util = fmt.Sprintf(" = %.0f%% of the link", 100*speed/lp.rate)
	}
	ca.mu.Lock()
	t.Logf("%s: %d MB in %v, %.2f MB/s%s | pkts %d retx %d rto %d spur %d minRTT %v srtt %v",
		name, size>>20, el.Round(time.Millisecond), speed/(1<<20), util,
		ca.nextSeq, ca.statRetx, ca.statRTO, ca.statSpurious, ca.minRTT.Round(time.Microsecond), ca.srtt.Round(time.Microsecond))
	ca.mu.Unlock()
}

func TestRudpClean(t *testing.T) { testStream(t, "loopback", linkProfile{}, 32<<20) }
func TestRudpLoss2(t *testing.T) { testStream(t, "loopback 2% loss", linkProfile{loss: 0.02}, 16<<20) }
func TestRudpMobile(t *testing.T) {
	testStream(t, "5G-like 6 MB/s 40ms rtt 0.5% loss", linkProfile{rate: 6 << 20, queue: 150 * time.Millisecond, delay: 20 * time.Millisecond, jitter: 3 * time.Millisecond, loss: 0.005}, 24<<20)
}
func TestRudpHome(t *testing.T) {
	testStream(t, "home 12 MB/s 20ms rtt", linkProfile{rate: 12 << 20, queue: 50 * time.Millisecond, delay: 10 * time.Millisecond, jitter: time.Millisecond, loss: 0.001}, 32<<20)
}
func TestRudpBad(t *testing.T) {
	testStream(t, "bad 1 MB/s 100ms rtt 3% loss", linkProfile{rate: 1 << 20, queue: 300 * time.Millisecond, delay: 50 * time.Millisecond, jitter: 10 * time.Millisecond, loss: 0.03}, 4<<20)
}

func TestRudpBothWays(t *testing.T) {
	ca, cb := udpPair(t, linkProfile{rate: 4 << 20, queue: 100 * time.Millisecond, delay: 15 * time.Millisecond, loss: 0.01})
	a := make([]byte, 3<<20)
	b := make([]byte, 2<<20)
	rand.Read(a)
	rand.Read(b)
	var wg sync.WaitGroup
	var gotA, gotB []byte
	wg.Add(2)
	go func() {
		defer wg.Done()
		gotA = make([]byte, len(a))
		io.ReadFull(cb, gotA)
	}()
	go func() {
		defer wg.Done()
		gotB = make([]byte, len(b))
		io.ReadFull(ca, gotB)
	}()
	go ca.Write(a)
	go cb.Write(b)
	wg.Wait()
	if !bytes.Equal(gotA, a) || !bytes.Equal(gotB, b) {
		t.Fatal("data differs")
	}
	ca.Close()
	cb.Close()
}

func TestRudpDeadline(t *testing.T) {
	ca, cb := udpPair(t, linkProfile{})
	defer ca.Close()
	defer cb.Close()
	cb.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	_, err := cb.Read(make([]byte, 10))
	var ne net.Error
	if !errors.As(err, &ne) || !ne.Timeout() {
		t.Fatalf("expected timeout, got %v", err)
	}
}

// ---------------------------------------------------------------- codes

func TestP2PCodes(t *testing.T) {
	for i := 0; i < 500; i++ {
		ip := net.IPv4(byte(mrand.Intn(256)), byte(mrand.Intn(256)), byte(mrand.Intn(256)), byte(mrand.Intn(256)))
		port := 1 + mrand.Intn(65535)
		c, err := NewP2PCode(&net.UDPAddr{IP: ip, Port: port})
		if err != nil {
			t.Fatal(err)
		}
		s := c.String()
		if len(s) != 14 {
			t.Fatalf("len %d", len(s))
		}
		d, err := Parse(FormatDisplay(s))
		if err != nil || d != c {
			t.Fatalf("roundtrip %v %+v %+v", err, d, c)
		}
		rip := net.IPv4(10, byte(i), 3, 4)
		r, err := NewReplyCode(c, &net.UDPAddr{IP: rip, Port: 40000 + i})
		if err != nil || len(r) != ReplyLen {
			t.Fatal(err, r)
		}
		a, err := ParseReply(c, FormatReply(r))
		if err != nil || !a.IP.Equal(rip) || a.Port != 40000+i {
			t.Fatalf("reply roundtrip %v %v", err, a)
		}
	}
	// a typo is usually detected
	c, _ := NewP2PCode(&net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 5000})
	r, _ := NewReplyCode(c, &net.UDPAddr{IP: net.IPv4(5, 6, 7, 8), Port: 6000})
	bad := 0
	for i := 0; i < ReplyLen; i++ {
		b := []byte(r)
		if b[i] == 'A' {
			b[i] = 'B'
		} else {
			b[i] = 'A'
		}
		if _, err := ParseReply(c, string(b)); err != nil {
			bad++
		}
	}
	if bad < 5 {
		t.Fatalf("only %d of %d typos detected", bad, ReplyLen)
	}
}

func TestSTUNParse(t *testing.T) {
	resp := stunResponse([12]byte{1}, &net.UDPAddr{IP: net.IPv4(93, 184, 216, 34), Port: 54321})
	a := parseSTUN(resp)
	if a == nil || !a.IP.Equal(net.IPv4(93, 184, 216, 34)) || a.Port != 54321 {
		t.Fatalf("%v", a)
	}
}

// ---------------------------------------------------------------- fake internet

func stunResponse(id [12]byte, a *net.UDPAddr) []byte {
	b := make([]byte, 32)
	binary.BigEndian.PutUint16(b[0:], 0x0101)
	binary.BigEndian.PutUint16(b[2:], 12)
	binary.BigEndian.PutUint32(b[4:], stunCookie)
	copy(b[8:], id[:])
	binary.BigEndian.PutUint16(b[20:], 0x0020)
	binary.BigEndian.PutUint16(b[22:], 8)
	b[25] = 1
	binary.BigEndian.PutUint16(b[26:], uint16(a.Port)^(stunCookie>>16))
	binary.BigEndian.PutUint32(b[28:], binary.BigEndian.Uint32(a.IP.To4())^stunCookie)
	return b
}

func fakeSTUN(t *testing.T, ip string) string {
	pc, err := net.ListenPacket("udp4", ip+":0")
	if err != nil {
		t.Skipf("cannot bind %s: %v", ip, err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if n >= 20 && binary.BigEndian.Uint16(buf) == 1 {
				var id [12]byte
				copy(id[:], buf[8:20])
				pc.WriteTo(stunResponse(id, from.(*net.UDPAddr)), from)
			}
		}
	}()
	return pc.LocalAddr().String()
}

type natKind int

const (
	natOpen       natKind = iota // full cone
	natRestricted                // port-restricted cone (typical home router)
	natSymmetric                 // new port for every destination
)

type natIn struct {
	b    []byte
	from net.Addr
}

// natConn is the "inside" of a simulated NAT router.
type natConn struct {
	kind   natKind
	ip     string
	mu     sync.Mutex
	outs   map[string]net.PacketConn // destination -> outside socket (symmetric)
	main   net.PacketConn
	sentTo map[string]bool
	in     chan natIn
	closed chan struct{}
	once   sync.Once
}

func newNAT(t *testing.T, kind natKind, ip string) *natConn {
	n := &natConn{kind: kind, ip: ip, outs: map[string]net.PacketConn{}, sentTo: map[string]bool{}, in: make(chan natIn, 4096), closed: make(chan struct{})}
	pc, err := net.ListenPacket("udp4", ip+":0")
	if err != nil {
		t.Skipf("cannot bind %s: %v", ip, err)
	}
	n.main = pc
	go n.pump(pc)
	return n
}

func (n *natConn) pump(pc net.PacketConn) {
	buf := make([]byte, 2048)
	for {
		k, from, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		n.mu.Lock()
		allowed := n.kind == natOpen || n.sentTo[pc.LocalAddr().String()+">"+from.String()]
		n.mu.Unlock()
		if !allowed {
			continue
		}
		select {
		case n.in <- natIn{append([]byte(nil), buf[:k]...), from}:
		case <-n.closed:
			return
		}
	}
}

func (n *natConn) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case p := <-n.in:
		return copy(b, p.b), p.from, nil
	case <-n.closed:
		return 0, nil, net.ErrClosed
	}
}

func (n *natConn) WriteTo(b []byte, to net.Addr) (int, error) {
	n.mu.Lock()
	out := n.main
	if n.kind == natSymmetric {
		out = n.outs[to.String()]
		if out == nil {
			pc, err := net.ListenPacket("udp4", n.ip+":0")
			if err != nil {
				n.mu.Unlock()
				return 0, err
			}
			n.outs[to.String()] = pc
			out = pc
			go n.pump(pc)
		}
	}
	n.sentTo[out.LocalAddr().String()+">"+to.String()] = true
	n.mu.Unlock()
	return out.WriteTo(b, to)
}

func (n *natConn) Close() error {
	n.once.Do(func() {
		close(n.closed)
		n.main.Close()
		n.mu.Lock()
		for _, o := range n.outs {
			o.Close()
		}
		n.mu.Unlock()
	})
	return nil
}
func (n *natConn) LocalAddr() net.Addr                { return &net.UDPAddr{IP: net.IPv4(192, 168, 1, 2), Port: 5000} }
func (n *natConn) SetDeadline(t time.Time) error      { return nil }
func (n *natConn) SetReadDeadline(t time.Time) error  { return nil }
func (n *natConn) SetWriteDeadline(t time.Time) error { return nil }

// setupInternet installs two fake STUN servers and hands out the given NATs
// in order (first: the device showing the code).
func setupInternet(t *testing.T, kinds ...natKind) {
	s1 := fakeSTUN(t, "127.0.0.1")
	s2 := fakeSTUN(t, "127.0.0.2")
	oldServers, oldSock := STUNServers, newP2PSocket
	STUNServers = []string{s1, s2}
	var mu sync.Mutex
	next := 0
	newP2PSocket = func() (net.PacketConn, error) {
		mu.Lock()
		defer mu.Unlock()
		k := kinds[next%len(kinds)]
		ip := []string{"127.0.0.3", "127.0.0.4"}[next%2]
		next++
		return newNAT(t, k, ip), nil
	}
	SetSTUNAllowed(true)
	t.Cleanup(func() {
		STUNServers, newP2PSocket = oldServers, oldSock
		SetSTUNAllowed(false)
	})
}

// runP2P sends a folder from the code owner (reversed flow) to the device
// that types the code; useReply: type the reply code on the owner.
func runP2P(t *testing.T, useReply bool, timeout time.Duration) error {
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "Album", "sub"), 0o755)
	big := make([]byte, 3<<20)
	rand.Read(big)
	os.WriteFile(filepath.Join(src, "Album", "foto.bin"), big, 0o644)
	os.WriteFile(filepath.Join(src, "Album", "sub", "nota.txt"), []byte("ciao"), 0o644)
	item, err := CollectItem(filepath.Join(src, "Album"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	r, err := StartReceiverP2P(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r.Code.Kind != KindP2P || !r.NAT.Checked {
		t.Fatalf("code %v nat %+v", r.Code.Kind, r.NAT)
	}
	sendErr := make(chan error, 1)
	go func() {
		_, err := r.WaitSend(ctx, PeerInfo{Name: "PC", Ver: AppVersion}, item.Entries, Events{}, nil)
		sendErr <- err
	}()
	dst := t.TempDir()
	ev := Events{Reply: func(code string, nat NATInfo) {
		if useReply {
			if err := r.SetReply(code); err != nil {
				t.Errorf("reply: %v", err)
			}
		}
	}}
	_, _, err = ReceiveFrom(ctx, r.Code.Display(), PeerInfo{Name: "Telefono", Ver: AppVersion}, &DirStore{Dir: dst}, ev)
	if err != nil {
		cancel()
		<-sendErr
		return err
	}
	if err := <-sendErr; err != nil {
		return err
	}
	got, _ := os.ReadFile(filepath.Join(dst, "Album", "foto.bin"))
	if !bytes.Equal(got, big) {
		t.Fatal("file differs")
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "Album", "sub", "nota.txt")); string(b) != "ciao" {
		t.Fatal("small file differs")
	}
	return nil
}

func TestP2POpenRouterNoReply(t *testing.T) {
	setupInternet(t, natOpen, natRestricted)
	if err := runP2P(t, false, 20*time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestP2PBothRestrictedWithReply(t *testing.T) {
	setupInternet(t, natRestricted, natRestricted)
	if err := runP2P(t, true, 20*time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestP2PSymmetricDetected(t *testing.T) {
	setupInternet(t, natSymmetric)
	ctx := context.Background()
	rep, err := TestNetwork(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.NAT.Checked || !rep.NAT.Symmetric {
		t.Fatalf("%+v", rep.NAT)
	}
}

func TestP2PRestrictedWithoutReplyFails(t *testing.T) {
	setupInternet(t, natRestricted, natRestricted)
	err := runP2P(t, false, 3*time.Second)
	if err == nil {
		t.Fatal("expected failure without the reply code")
	}
}

func TestP2PNeedsConsent(t *testing.T) {
	SetSTUNAllowed(false)
	if _, err := StartReceiverP2P(context.Background()); !errors.Is(err, ErrNeedConsent) {
		t.Fatal(err)
	}
	if _, err := SendTo(context.Background(), "K7QM-4XRT-9PWA-77", PeerInfo{}, nil, Events{}); !errors.Is(err, ErrNeedConsent) {
		t.Fatal(err)
	}
}
