// Package core contains the platform independent part of PiqLink:
// transfer codes, key exchange, encrypted channel and file transfer protocol.
package core

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
)

// Alphabet: Crockford base32 (no I, L, O, U) so codes are easy to read aloud.
const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// Kind of code, recognised from its length.
type Kind int

const (
	KindLAN24 Kind = iota // 6 chars: same /24 network (typical home / office)
	KindLAN16             // 8 chars: same /16 network (large networks)
	KindNET               // 12 chars: public IPv4 over the internet
	KindP2P               // 14 chars: public UDP endpoint seen by STUN (assisted mode, 2.5)
)

// Ports
const (
	LANBasePort = 47760 // 47760..47763 (2 bits)
	NETBasePort = 47770 // 47770..47785 external (4 bits)
)

type layout struct {
	chars, hostBits, portBits, secretBits int
}

var layouts = map[Kind]layout{
	KindLAN24: {6, 8, 2, 20},
	KindLAN16: {8, 16, 2, 22},
	KindNET:   {12, 32, 4, 24},
	KindP2P:   {14, 32, 16, 22},
}

// Code is a decoded transfer code.
type Code struct {
	Kind    Kind
	Host    uint32 // low host bits (LAN) or full IPv4 (NET, P2P)
	PortIdx int    // port index, or the full UDP port (P2P)
	Secret  uint32
}

// Port returns the TCP port encoded in the code.
func (c Code) Port() int {
	if c.Kind == KindP2P {
		return c.PortIdx
	}
	if c.Kind == KindNET {
		return NETBasePort + c.PortIdx
	}
	return LANBasePort + c.PortIdx
}

func mixMask(secret uint32, bits int) uint64 {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], secret)
	h := sha256.Sum256(append([]byte("PiqLink-mix-v1"), b[:]...))
	m := binary.BigEndian.Uint64(h[:8])
	if bits >= 64 {
		return m
	}
	return m & ((1 << uint(bits)) - 1)
}

// String returns the compact form (no separators), e.g. "K7QM4X".
func (c Code) String() string {
	l := layouts[c.Kind]
	hp := uint64(c.Host)<<uint(l.portBits) | uint64(c.PortIdx)
	hpBits := l.hostBits + l.portBits
	// scramble address bits with the secret, so every code looks different
	hp ^= mixMask(c.Secret, hpBits)
	return encodeBits(hp, uint64(c.Secret), l.secretBits, l.chars)
}

// encodeBits writes hi<<loBits | lo in base32 with n digits (up to 128 bits).
func encodeBits(hi, lo uint64, loBits, n int) string {
	v := new(big.Int).SetUint64(hi)
	v.Lsh(v, uint(loBits))
	v.Or(v, new(big.Int).SetUint64(lo))
	out := make([]byte, n)
	d := new(big.Int)
	for i := n - 1; i >= 0; i-- {
		d.And(v, big.NewInt(31))
		out[i] = alphabet[d.Int64()]
		v.Rsh(v, 5)
	}
	return string(out)
}

// decodeBits is the inverse of encodeBits.
func decodeBits(s string, loBits int) (hi, lo uint64) {
	v := new(big.Int)
	for _, r := range s {
		v.Lsh(v, 5)
		v.Or(v, big.NewInt(int64(strings.IndexRune(alphabet, r))))
	}
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(loBits)), big.NewInt(1))
	lo = new(big.Int).And(v, mask).Uint64()
	hi = v.Rsh(v, uint(loBits)).Uint64()
	return
}

// Display returns the code with dashes: K7Q-M4X, K7QM-4XRT, K7QM-4XRT-9PWA.
func (c Code) Display() string { return FormatDisplay(c.String()) }

// FormatDisplay groups a compact code for display.
func FormatDisplay(s string) string {
	switch len(s) {
	case 6:
		return s[:3] + "-" + s[3:]
	case 8:
		return s[:4] + "-" + s[4:]
	case 12:
		return s[:4] + "-" + s[4:8] + "-" + s[8:]
	case 14:
		return s[:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:]
	}
	return s
}

// FormatTyping groups a normalized code while it is being typed.
func FormatTyping(s string) string {
	if len(s) > 14 {
		s = s[:14]
	}
	n := len(s)
	switch {
	case n <= 3:
		return s
	case n <= 6:
		return s[:3] + "-" + s[3:]
	case n <= 8:
		return s[:4] + "-" + s[4:]
	case n <= 12:
		return s[:4] + "-" + s[4:8] + "-" + s[8:]
	}
	return s[:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:]
}

// IsCodeChar reports whether r counts as a code character after Normalize.
func IsCodeChar(r rune) bool {
	if r >= 'a' && r <= 'z' {
		r -= 32
	}
	return strings.ContainsRune(alphabet+"OILU", r)
}

// Normalize cleans user input: uppercase, removes separators and fixes
// look-alike characters (O->0, I/L->1, U->V). Also accepts the QR payload.
func Normalize(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "PIQLINK:")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == 'O':
			r = '0'
		case r == 'I' || r == 'L':
			r = '1'
		case r == 'U':
			r = 'V'
		}
		if strings.ContainsRune(alphabet, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

var ErrBadCode = errors.New("codice non valido")

// Parse decodes a code typed by the user.
func Parse(input string) (Code, error) {
	s := Normalize(input)
	var kind Kind
	switch len(s) {
	case 6:
		kind = KindLAN24
	case 8:
		kind = KindLAN16
	case 12:
		kind = KindNET
	case 14:
		kind = KindP2P
	default:
		return Code{}, ErrBadCode
	}
	l := layouts[kind]
	hp, lo := decodeBits(s, l.secretBits)
	secret := uint32(lo)
	hp ^= mixMask(secret, l.hostBits+l.portBits)
	c := Code{
		Kind:    kind,
		Secret:  secret,
		PortIdx: int(hp & ((1 << uint(l.portBits)) - 1)),
		Host:    uint32(hp >> uint(l.portBits)),
	}
	return c, nil
}

// QRPayload is the text stored in the QR code.
func (c Code) QRPayload() string { return "PIQLINK:" + c.String() }

func randomBits(bits int) uint32 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return binary.BigEndian.Uint32(b[:]) & ((1 << uint(bits)) - 1)
}

// NewLANCode creates a fresh code for a receiver listening on ip with the given
// network mask size (ones) and port index.
func NewLANCode(ip net.IP, maskOnes int, portIdx int) (Code, error) {
	ip4 := ip.To4()
	if ip4 == nil {
		return Code{}, errors.New(Tr("indirizzo IPv4 non disponibile", "IPv4 address not available"))
	}
	v := binary.BigEndian.Uint32(ip4)
	if maskOnes >= 24 {
		return Code{Kind: KindLAN24, Host: v & 0xFF, PortIdx: portIdx, Secret: randomBits(20)}, nil
	}
	if maskOnes >= 16 {
		return Code{Kind: KindLAN16, Host: v & 0xFFFF, PortIdx: portIdx, Secret: randomBits(22)}, nil
	}
	return Code{}, fmt.Errorf(Tr("rete troppo grande (/%d): usa la modalità Internet", "network too large (/%d): use Internet mode"), maskOnes)
}

// NewNETCode creates a fresh internet code for a public IPv4 and external port index.
func NewNETCode(publicIP net.IP, portIdx int) (Code, error) {
	ip4 := publicIP.To4()
	if ip4 == nil {
		return Code{}, errors.New(Tr("indirizzo pubblico IPv4 non disponibile", "public IPv4 address not available"))
	}
	return Code{Kind: KindNET, Host: binary.BigEndian.Uint32(ip4), PortIdx: portIdx, Secret: randomBits(24)}, nil
}

// Candidates returns the IP addresses the sender should try for this code.
func (c Code) Candidates() []net.IP {
	if c.Kind == KindNET || c.Kind == KindP2P {
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, c.Host)
		return []net.IP{ip}
	}
	var out []net.IP
	seen := map[string]bool{}
	for _, li := range LocalIPv4s() {
		base := binary.BigEndian.Uint32(li.IP.To4())
		var v uint32
		if c.Kind == KindLAN24 {
			v = base&0xFFFFFF00 | c.Host&0xFF
		} else {
			v = base&0xFFFF0000 | c.Host&0xFFFF
		}
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, v)
		if !seen[ip.String()] {
			seen[ip.String()] = true
			out = append(out, ip)
		}
	}
	return out
}

// NewP2PCode creates a code for the assisted mode: the public UDP endpoint
// of this device as seen by a STUN server.
func NewP2PCode(public *net.UDPAddr) (Code, error) {
	ip4 := public.IP.To4()
	if ip4 == nil {
		return Code{}, errors.New(Tr("indirizzo pubblico IPv4 non disponibile", "public IPv4 address not available"))
	}
	return Code{Kind: KindP2P, Host: binary.BigEndian.Uint32(ip4), PortIdx: public.Port, Secret: randomBits(22)}, nil
}

// UDPAddr of a P2P code.
func (c Code) UDPAddr() *net.UDPAddr {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, c.Host)
	return &net.UDPAddr{IP: ip, Port: c.Port()}
}

// ---------------------------------------------------------------- reply code

// ReplyLen is the length of the reply code: the public endpoint of the device
// that typed a P2P code (32 + 16 bits) and 2 check bits, scrambled with the
// secret of the main code.
const ReplyLen = 10

func replyCheck(c Code, ip uint32, port int) uint64 {
	var b [10]byte
	binary.BigEndian.PutUint32(b[0:], c.Secret)
	binary.BigEndian.PutUint32(b[4:], ip)
	binary.BigEndian.PutUint16(b[8:], uint16(port))
	h := sha256.Sum256(append([]byte("PiqLink-reply-v1"), b[:]...))
	return uint64(h[0] & 3)
}

func replyMask(c Code) uint64 {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], c.Secret)
	h := sha256.Sum256(append([]byte("PiqLink-reply-mask-v1"), b[:]...))
	return binary.BigEndian.Uint64(h[:8]) & (1<<50 - 1)
}

// NewReplyCode encodes the public endpoint of this device for the peer that
// showed the P2P code c.
func NewReplyCode(c Code, public *net.UDPAddr) (string, error) {
	ip4 := public.IP.To4()
	if ip4 == nil {
		return "", errors.New(Tr("indirizzo pubblico IPv4 non disponibile", "public IPv4 address not available"))
	}
	ip := binary.BigEndian.Uint32(ip4)
	v := uint64(ip)<<18 | uint64(public.Port)<<2 | replyCheck(c, ip, public.Port)
	v ^= replyMask(c)
	return encodeBits(0, v, 50, ReplyLen), nil
}

// ParseReply decodes a reply code typed on the device that showed c.
func ParseReply(c Code, input string) (*net.UDPAddr, error) {
	s := Normalize(input)
	if len(s) != ReplyLen {
		return nil, ErrBadCode
	}
	_, v := decodeBits(s, 50)
	v ^= replyMask(c)
	ip := uint32(v >> 18)
	port := int(v>>2) & 0xFFFF
	if v&3 != replyCheck(c, ip, port) || port == 0 {
		return nil, ErrBadCode
	}
	a := make(net.IP, 4)
	binary.BigEndian.PutUint32(a, ip)
	return &net.UDPAddr{IP: a, Port: port}, nil
}

// FormatReply groups a reply code: K7QM4-XRT9P.
func FormatReply(s string) string {
	if len(s) > ReplyLen {
		s = s[:ReplyLen]
	}
	if len(s) <= 5 {
		return s
	}
	return s[:5] + "-" + s[5:]
}
