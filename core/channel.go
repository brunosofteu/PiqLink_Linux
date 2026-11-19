package core

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// Wire protocol
//
//   sender -> receiver : "PIQL" | version(1) | X (256 bytes)
//   receiver -> sender : Y (256 bytes) | confirmReceiver (32)
//   sender -> receiver : confirmSender (32)
//   then both directions: frames  uint32 length | AES-256-GCM ciphertext
//   nonce = 4 zero bytes | 8-byte big endian counter (separate key per direction)

const (
	protoMagic   = "PIQL"
	protoVersion = 2
	maxFrame     = 1 << 20
	handshakeTO  = 20 * time.Second
)

// Channel is an authenticated, encrypted message stream.
type Channel struct {
	conn       net.Conn
	wmu        sync.Mutex // Write may be called by Cancel from another goroutine
	send, recv cipher.AEAD
	sendN      uint64
	recvN      uint64
}

func newAEAD(key []byte) cipher.AEAD {
	b, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		panic(err)
	}
	return g
}

func newChannel(conn net.Conn, k *Keys) *Channel {
	return &Channel{conn: conn, send: newAEAD(k.SendKey), recv: newAEAD(k.RecvKey)}
}

func nonce(n uint64) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint64(b[4:], n)
	return b
}

// Write sends one message.
func (c *Channel) Write(msg []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	ct := c.send.Seal(nil, nonce(c.sendN), msg, nil)
	c.sendN++
	buf := make([]byte, 4+len(ct))
	binary.BigEndian.PutUint32(buf, uint32(len(ct)))
	copy(buf[4:], ct)
	_, err := c.conn.Write(buf)
	return err
}

// Read receives one message.
func (c *Channel) Read() ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(c.conn, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > maxFrame || n < 16 {
		return nil, errors.New(Tr("frame non valido", "invalid frame"))
	}
	ct := make([]byte, n)
	if _, err := io.ReadFull(c.conn, ct); err != nil {
		return nil, err
	}
	pt, err := c.recv.Open(nil, nonce(c.recvN), ct, nil)
	if err != nil {
		return nil, errors.New(Tr("dati alterati o corrotti", "data altered or corrupted"))
	}
	c.recvN++
	return pt, nil
}

func (c *Channel) Close() error { return c.conn.Close() }

// ClientHandshake runs the sender side of the key exchange.
func ClientHandshake(conn net.Conn, code string) (*Channel, error) {
	conn.SetDeadline(time.Now().Add(handshakeTO))
	defer conn.SetDeadline(time.Time{})
	p := NewPake(code, true)
	hello := append([]byte(protoMagic), protoVersion)
	hello = append(hello, p.Msg...)
	if _, err := conn.Write(hello); err != nil {
		return nil, err
	}
	resp := make([]byte, ElemLen+32)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return nil, errors.New(Tr("il destinatario ha chiuso la connessione (codice errato, scaduto o versione di PiqLink diversa?)", "the receiver closed the connection (wrong or expired code, or a different PiqLink version?)"))
	}
	keys, err := p.Finish(resp[:ElemLen])
	if err != nil {
		return nil, err
	}
	if !hmac.Equal(keys.PeerConf, resp[ElemLen:]) {
		return nil, ErrPake
	}
	if _, err := conn.Write(keys.MyConfirm); err != nil {
		return nil, err
	}
	return newChannel(conn, keys), nil
}

// ErrVersion means the other device runs an incompatible PiqLink version.
var ErrVersion = errors.New("incompatible version")

// ErrNotPiqLink means the peer did not speak the PiqLink protocol.
var ErrNotPiqLink = errors.New("connessione non PiqLink")

// ServerHandshake runs the receiver side. ErrPake means a wrong code.
func ServerHandshake(conn net.Conn, code string) (*Channel, error) {
	conn.SetDeadline(time.Now().Add(handshakeTO))
	defer conn.SetDeadline(time.Time{})
	hdr := make([]byte, 5+ElemLen)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return nil, ErrNotPiqLink
	}
	if string(hdr[:4]) != protoMagic {
		return nil, ErrNotPiqLink
	}
	if hdr[4] != protoVersion {
		return nil, ErrVersion
	}
	p := NewPake(code, false)
	keys, err := p.Finish(hdr[5:])
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(append(append([]byte{}, p.Msg...), keys.MyConfirm...)); err != nil {
		return nil, err
	}
	conf := make([]byte, 32)
	if _, err := io.ReadFull(conn, conf); err != nil {
		return nil, ErrPake // sender noticed the wrong code and hung up
	}
	if !hmac.Equal(conf, keys.PeerConf) {
		return nil, ErrPake
	}
	return newChannel(conn, keys), nil
}
