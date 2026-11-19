package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	CodeValidity = 10 * time.Minute
	MaxAttempts  = 3
)

type Mode int

const (
	ModeLAN Mode = iota
	ModeNET
	ModeP2P // assisted mode: STUN + hole punching (2.5)
)

var (
	ErrExpired         = errors.New("codice scaduto")
	ErrTooManyAttempts = errors.New("troppi tentativi con un codice errato: ricezione bloccata per sicurezza")
	ErrCancelled       = errors.New("annullato")
)

// ErrNeedAssisted means a direct internet connection is not possible
// from this network (the assisted mode would be required).
type ErrNeedAssisted struct{ Reason string }

func (e *ErrNeedAssisted) Error() string {
	return Tr("collegamento diretto da internet non possibile: ", "direct connection from the internet not possible: ") + e.Reason
}

// Receiver waits for one incoming transfer.
type Receiver struct {
	Mode    Mode
	Code    Code
	Expires time.Time
	NAT     NATInfo // assisted mode only
	ln      net.Listener
	mapping *PortMapping
	mux     *udpMux
	replies chan *net.UDPAddr
	once    sync.Once
}

func listenOn(ports []int) (net.Listener, int, error) {
	var last error
	for i, p := range ports {
		ln, err := net.Listen("tcp4", fmt.Sprintf(":%d", p))
		if err == nil {
			return ln, i, nil
		}
		last = err
	}
	return nil, 0, last
}

// StartReceiver opens the listening socket and creates a fresh code.
func StartReceiver(mode Mode) (*Receiver, error) {
	addrs := LocalIPv4s()
	if len(addrs) == 0 {
		return nil, errors.New(Tr("nessuna rete disponibile: collegati a un Wi-Fi o a una rete", "no network available: connect to Wi-Fi or a network"))
	}
	primary := addrs[0]
	r := &Receiver{Mode: mode, Expires: time.Now().Add(CodeValidity)}
	if mode == ModeLAN {
		ln, idx, err := listenOn([]int{LANBasePort, LANBasePort + 1, LANBasePort + 2, LANBasePort + 3})
		if err != nil {
			return nil, fmt.Errorf(Tr("porta di ricezione occupata: %v", "receiving port busy: %v"), err)
		}
		code, err := NewLANCode(primary.IP, primary.MaskOnes, idx)
		if err != nil {
			ln.Close()
			return nil, err
		}
		r.ln, r.Code = ln, code
		return r, nil
	}
	// Internet: listen locally, then ask the router to open a port.
	ln, _, err := listenOn([]int{NETBasePort, NETBasePort + 1, NETBasePort + 2, 0})
	if err != nil {
		return nil, err
	}
	internal := ln.Addr().(*net.TCPAddr).Port
	gw, err := DiscoverGateway(primary.IP, 3*time.Second)
	if err != nil {
		ln.Close()
		return nil, &ErrNeedAssisted{err.Error()}
	}
	ext, err := gw.ExternalIP()
	if err != nil {
		ln.Close()
		return nil, &ErrNeedAssisted{err.Error()}
	}
	if !IsPublicIPv4(ext) {
		ln.Close()
		return nil, &ErrNeedAssisted{fmt.Sprintf(Tr("il router ha un indirizzo non pubblico (%s): la rete dell'operatore blocca le connessioni in arrivo", "the router has a non-public address (%s): the provider's network blocks incoming connections"), ext)}
	}
	m, err := gw.AddMapping(internal)
	if err != nil {
		ln.Close()
		return nil, &ErrNeedAssisted{err.Error()}
	}
	m.ExternalIP = ext
	code, err := NewNETCode(ext, m.ExternalPort-NETBasePort)
	if err != nil {
		m.Remove()
		ln.Close()
		return nil, err
	}
	r.ln, r.mapping, r.Code = ln, m, code
	return r, nil
}

// Close stops listening and removes the router mapping.
func (r *Receiver) Close() {
	r.once.Do(func() {
		if r.mux != nil {
			r.mux.Close()
			return
		}
		r.ln.Close()
		if r.mapping != nil {
			go r.mapping.Remove()
		}
	})
}

// accept waits for a peer with the right code and returns the channel.
func (r *Receiver) accept(ctx context.Context) (*Channel, error) {
	if r.mux != nil {
		return r.acceptP2P(ctx)
	}
	stop := make(chan struct{})
	defer close(stop)
	timer := time.NewTimer(time.Until(r.Expires))
	defer timer.Stop()
	expired := false
	var mu sync.Mutex
	go func() {
		select {
		case <-ctx.Done():
		case <-timer.C:
			mu.Lock()
			expired = true
			mu.Unlock()
		case <-stop:
			return
		}
		r.ln.Close()
	}()
	attempts := 0
	code := r.Code.String()
	for {
		conn, err := r.ln.Accept()
		if err != nil {
			mu.Lock()
			exp := expired
			mu.Unlock()
			if exp {
				return nil, ErrExpired
			}
			if ctx.Err() != nil {
				return nil, ErrCancelled
			}
			return nil, err
		}
		ch, err := ServerHandshake(conn, code)
		if err != nil {
			conn.Close()
			if errors.Is(err, ErrPake) {
				attempts++
				if attempts >= MaxAttempts {
					return nil, ErrTooManyAttempts
				}
			}
			continue
		}
		// one transfer per code: stop listening
		r.ln.Close()
		if r.mapping != nil {
			go r.mapping.Remove()
			r.mapping = nil
		}
		return ch, nil
	}
}

// watch closes the channel when ctx is cancelled; call the returned func when done.
func watch(ctx context.Context, ch *Channel, reason string) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			ch.Cancel(reason)
			ch.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

// Wait accepts connections until a sender with the right code arrives, then
// receives the files into the store. started is called once connected.
func (r *Receiver) Wait(ctx context.Context, me PeerInfo, store Store, ev Events, started func()) (PeerInfo, []string, error) {
	defer r.Close()
	ch, err := r.accept(ctx)
	if err != nil {
		return PeerInfo{}, nil, err
	}
	if started != nil {
		started()
	}
	me.Role = RoleRecv
	stopWatch := watch(ctx, ch, Tr("annullato dal destinatario", "cancelled by the receiver"))
	peer, saved, err := receiveFiles(ch, me, store, ev, ctx.Done(), false)
	stopWatch()
	ch.Close()
	if ctx.Err() != nil {
		err = ErrCancelled
	}
	return peer, saved, err
}

// WaitSend is the reversed flow: this device shows the code and waits, the
// other device types it and receives the files.
func (r *Receiver) WaitSend(ctx context.Context, me PeerInfo, files []FileInfo, ev Events, started func()) (PeerInfo, error) {
	defer r.Close()
	ch, err := r.accept(ctx)
	if err != nil {
		return PeerInfo{}, err
	}
	if started != nil {
		started()
	}
	me.Role = RoleSend
	stopWatch := watch(ctx, ch, Tr("annullato dal mittente", "cancelled by the sender"))
	peer, err := sendFiles(ch, me, files, ev, true)
	stopWatch()
	ch.Close()
	if ctx.Err() != nil {
		err = ErrCancelled
	}
	return peer, err
}

// connect reaches the device that shows the code and runs the handshake.
func connect(ctx context.Context, codeText string, toReceiver bool, ev Events) (*Channel, error) {
	code, err := Parse(codeText)
	if err != nil {
		return nil, ErrBadCode
	}
	if code.Kind == KindP2P {
		return connectP2P(ctx, code, ev)
	}
	cands := code.Candidates()
	if len(cands) == 0 {
		return nil, errors.New(Tr("nessuna rete disponibile: collegati a un Wi-Fi o a una rete", "no network available: connect to Wi-Fi or a network"))
	}
	timeout := 4 * time.Second
	if code.Kind == KindNET {
		timeout = 10 * time.Second
	}
	dctx, cancel := context.WithTimeout(ctx, timeout)
	type res struct {
		c   net.Conn
		err error
	}
	results := make(chan res, len(cands))
	for _, ip := range cands {
		go func(ip net.IP) {
			var d net.Dialer
			c, err := d.DialContext(dctx, "tcp4", net.JoinHostPort(ip.String(), fmt.Sprint(code.Port())))
			results <- res{c, err}
		}(ip)
	}
	var conn net.Conn
	for range cands {
		r := <-results
		if r.err == nil {
			if conn == nil {
				conn = r.c
				cancel()
			} else {
				r.c.Close()
			}
		}
	}
	cancel()
	if ctx.Err() != nil {
		if conn != nil {
			conn.Close()
		}
		return nil, ErrCancelled
	}
	if conn == nil {
		switch {
		case code.Kind == KindNET && toReceiver:
			return nil, errors.New(Tr("destinatario non raggiungibile: controlla il codice e che il destinatario sia ancora in attesa", "receiver not reachable: check the code and that the receiver is still waiting"))
		case code.Kind == KindNET:
			return nil, errors.New(Tr("mittente non raggiungibile: controlla il codice e che il mittente sia ancora in attesa", "sender not reachable: check the code and that the sender is still waiting"))
		case toReceiver:
			return nil, errors.New(Tr("nessun destinatario trovato: controlla il codice e di essere sulla stessa rete del destinatario", "no receiver found: check the code and that you are on the same network as the receiver"))
		}
		return nil, errors.New(Tr("nessun mittente trovato: controlla il codice e di essere sulla stessa rete del mittente", "no sender found: check the code and that you are on the same network as the sender"))
	}
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-stop:
		}
	}()
	ch, err := ClientHandshake(conn, code.String())
	close(stop)
	if err != nil {
		conn.Close()
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

// SendTo connects to the receiver identified by the code and sends files.
func SendTo(ctx context.Context, codeText string, me PeerInfo, files []FileInfo, ev Events) (PeerInfo, error) {
	ch, err := connect(ctx, codeText, true, ev)
	if err != nil {
		return PeerInfo{}, err
	}
	defer ch.Close()
	me.Role = RoleSend
	stopWatch := watch(ctx, ch, Tr("annullato dal mittente", "cancelled by the sender"))
	defer stopWatch()
	peer, err := sendFiles(ch, me, files, ev, false)
	if err != nil && ctx.Err() != nil {
		return peer, ErrCancelled
	}
	return peer, err
}

// ReceiveFrom is the reversed flow on the other side: type the sender's code
// and receive its files.
func ReceiveFrom(ctx context.Context, codeText string, me PeerInfo, store Store, ev Events) (PeerInfo, []string, error) {
	ch, err := connect(ctx, codeText, false, ev)
	if err != nil {
		return PeerInfo{}, nil, err
	}
	defer ch.Close()
	me.Role = RoleRecv
	stopWatch := watch(ctx, ch, Tr("annullato dal destinatario", "cancelled by the receiver"))
	defer stopWatch()
	peer, saved, err := receiveFiles(ch, me, store, ev, ctx.Done(), true)
	if err != nil && ctx.Err() != nil {
		return peer, saved, ErrCancelled
	}
	return peer, saved, err
}
