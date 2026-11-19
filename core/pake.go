package core

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/big"
)

// SPAKE2 over the RFC 3526 2048-bit MODP group (generator 2, order q=(p-1)/2).
// A short code is enough because an attacker can only test one guess per
// connection, and the receiver blocks the code after a few failed attempts.

const modpHex = "FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD1" +
	"29024E088A67CC74020BBEA63B139B22514A08798E3404DD" +
	"EF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245" +
	"E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7ED" +
	"EE386BFB5A899FA5AE9F24117C4B1FE649286651ECE45B3D" +
	"C2007CB8A163BF0598DA48361C55D39A69163FA8FD24CF5F" +
	"83655D23DCA3AD961C62F356208552BB9ED529077096966D" +
	"670C354E4ABC9804F1746C08CA18217C32905E462E36CE3B" +
	"E39E772C180E86039B2783A2EC07A28FB5C55DF06F4C52C9" +
	"DE2BCBF6955817183995497CEA956AE515D2261898FA0510" +
	"15728E5A8AACAA68FFFFFFFFFFFFFFFF"

const ElemLen = 256

var (
	grpP, grpQ, grpG, grpM, grpN *big.Int
)

func init() {
	grpP, _ = new(big.Int).SetString(modpHex, 16)
	grpQ = new(big.Int).Rsh(grpP, 1)
	grpG = big.NewInt(2)
	grpM = hashToGroup("PiqLink SPAKE2 M")
	grpN = hashToGroup("PiqLink SPAKE2 N")
}

// hashToGroup maps a label to an element of the order-q subgroup (a square).
func hashToGroup(label string) *big.Int {
	buf := make([]byte, 0, 320)
	for i := 0; len(buf) < 320; i++ {
		h := sha256.Sum256(append([]byte{byte(i)}, label...))
		buf = append(buf, h[:]...)
	}
	x := new(big.Int).SetBytes(buf)
	x.Mod(x, grpP)
	return x.Exp(x, big.NewInt(2), grpP)
}

func elemBytes(x *big.Int) []byte {
	out := make([]byte, ElemLen)
	x.FillBytes(out)
	return out
}

func passwordScalar(code string) *big.Int {
	h := sha256.Sum256([]byte("PiqLink-v1-password:" + Normalize(code)))
	w := new(big.Int).SetBytes(h[:])
	return w.Mod(w, grpQ)
}

func randomScalar() *big.Int {
	for {
		k, err := rand.Int(rand.Reader, grpQ)
		if err != nil {
			panic(err)
		}
		if k.Sign() > 0 {
			return k
		}
	}
}

func validElem(x *big.Int) bool {
	one := big.NewInt(1)
	pm1 := new(big.Int).Sub(grpP, one)
	if x.Cmp(one) <= 0 || x.Cmp(pm1) >= 0 {
		return false
	}
	return new(big.Int).Exp(x, grpQ, grpP).Cmp(one) == 0
}

// Pake holds one side of the exchange.
type Pake struct {
	isSender bool
	w, x     *big.Int
	Msg      []byte // our public message (X for sender, Y for receiver)
}

// NewPake starts the exchange. The sender uses M, the receiver N.
func NewPake(code string, isSender bool) *Pake {
	w := passwordScalar(code)
	x := randomScalar()
	blind := grpN
	if isSender {
		blind = grpM
	}
	t := new(big.Int).Exp(grpG, x, grpP)
	t.Mul(t, new(big.Int).Exp(blind, w, grpP)).Mod(t, grpP)
	return &Pake{isSender: isSender, w: w, x: x, Msg: elemBytes(t)}
}

// Keys derived after a successful exchange.
type Keys struct {
	SendKey, RecvKey    []byte // AES-256 keys for each direction
	MyConfirm, PeerConf []byte // key confirmation MACs
}

var ErrPake = errors.New("codice errato")

// Finish computes the shared keys from the peer's message.
func (p *Pake) Finish(peer []byte) (*Keys, error) {
	if len(peer) != ElemLen {
		return nil, ErrPake
	}
	Y := new(big.Int).SetBytes(peer)
	if !validElem(Y) {
		return nil, ErrPake
	}
	peerBlind := grpM
	if p.isSender {
		peerBlind = grpN
	}
	// K = (Y / peerBlind^w)^x
	inv := new(big.Int).Exp(peerBlind, p.w, grpP)
	inv.ModInverse(inv, grpP)
	k := new(big.Int).Mul(Y, inv)
	k.Mod(k, grpP).Exp(k, p.x, grpP)

	var sMsg, rMsg []byte
	if p.isSender {
		sMsg, rMsg = p.Msg, peer
	} else {
		sMsg, rMsg = peer, p.Msg
	}
	tt := sha256.New()
	for _, part := range [][]byte{[]byte("PiqLink-SPAKE2-v1"), sMsg, rMsg, elemBytes(k), elemBytes(p.w)} {
		var l [8]byte
		binary.BigEndian.PutUint64(l[:], uint64(len(part)))
		tt.Write(l[:])
		tt.Write(part)
	}
	secret := tt.Sum(nil)
	derive := func(info string) []byte {
		b, err := hkdf.Key(sha256.New, secret, nil, "PiqLink "+info, 32)
		if err != nil {
			panic(err)
		}
		return b
	}
	s2r, r2s := derive("key sender->receiver"), derive("key receiver->sender")
	cs, cr := derive("confirm sender"), derive("confirm receiver")
	transcript := append(append([]byte{}, sMsg...), rMsg...)
	mac := func(k []byte) []byte {
		m := hmac.New(sha256.New, k)
		m.Write(transcript)
		return m.Sum(nil)
	}
	keys := &Keys{}
	if p.isSender {
		keys.SendKey, keys.RecvKey = s2r, r2s
		keys.MyConfirm, keys.PeerConf = mac(cs), mac(cr)
	} else {
		keys.SendKey, keys.RecvKey = r2s, s2r
		keys.MyConfirm, keys.PeerConf = mac(cr), mac(cs)
	}
	return keys, nil
}
