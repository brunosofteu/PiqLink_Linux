package core

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Message types inside the encrypted channel (protocol v2).
const (
	msgHello   = 0x01 // JSON PeerInfo
	msgOffer   = 0x02 // JSON Offer
	msgAccept  = 0x03 // JSON Accept (resume offsets)
	msgReject  = 0x04 // UTF-8 reason
	msgData    = 0x10 // raw file bytes (current file)
	msgFileEnd = 0x11 // 32 bytes SHA-256 of the whole current file
	msgAcked   = 0x12 // 8 bytes: bytes the receiver has stored so far (2.5+)
	msgDone    = 0x20 // empty, sender finished
	msgResult  = 0x21 // JSON Result
	msgCancel  = 0x30 // UTF-8 reason
)

const chunkSize = 256 * 1024

// testSendDelay slows the sender down in tests (0 in the app).
var testSendDelay time.Duration

// ConfirmTimeout is how long the receiver has to accept an offer.
const ConfirmTimeout = 2 * time.Minute

// AppVersion is shown to the peer. A build may lower it (the Linux 2.1
// release) to behave exactly like that version.
var AppVersion = "3.0"

// peerAtLeast reports whether the peer runs PiqLink major.minor or later.
func peerAtLeast(p PeerInfo, major, minor int) bool {
	var ma, mi int
	if _, err := fmt.Sscanf(p.Ver, "%d.%d", &ma, &mi); err != nil {
		return false
	}
	return ma > major || (ma == major && mi >= minor)
}

// ackedProgress: the receiver reports what it stored, so both devices show
// the same percentage.
func ackedProgress(p PeerInfo) bool {
	return peerAtLeast(p, 2, 5) && peerAtLeast(PeerInfo{Ver: AppVersion}, 2, 5)
}

type PeerInfo struct {
	Name string `json:"name"`
	OS   string `json:"os"`
	Ver  string `json:"ver"`
	ID   string `json:"id,omitempty"`   // stable random device id
	Role string `json:"role,omitempty"` // "send" or "recv" (2.0+; empty = 1.5)
}

const (
	RoleSend = "send"
	RoleRecv = "recv"
)

var (
	// ErrSameRole: both devices want to send (or both to receive).
	ErrSameRole = errors.New("same role")
	// ErrOldPeer: the other device runs PiqLink 1.5, which cannot use the reversed flow.
	ErrOldPeer = errors.New("old peer")
)

// checkRoles verifies that one device sends and the other receives.
// strict: the reversed flow, which needs a 2.0 peer.
func checkRoles(c *Channel, me, peer PeerInfo, strict bool) error {
	if peer.Role == "" && strict {
		c.Cancel(Tr("aggiorna PiqLink alla versione 2.0", "update PiqLink to version 2.0"))
		return ErrOldPeer
	}
	if peer.Role != "" && peer.Role == me.Role {
		if me.Role == RoleSend {
			c.Cancel(Tr("anche l'altro dispositivo sta inviando", "the other device is sending too"))
		} else {
			c.Cancel(Tr("anche l'altro dispositivo sta ricevendo", "the other device is receiving too"))
		}
		return ErrSameRole
	}
	return nil
}

// FileInfo is one entry of an offer. Name is a relative path with '/'
// separators (e.g. "Foto/2024/img.jpg"). Dir entries create (empty) folders.
type FileInfo struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Dir  bool   `json:"dir,omitempty"`
	ID   string `json:"id,omitempty"` // sender-side identity for resuming
	Path string `json:"-"`
}

type Offer struct {
	Files []FileInfo `json:"files"`
}

// TotalSize of the non-directory entries.
func (o Offer) TotalSize() (n int64) {
	for _, f := range o.Files {
		if !f.Dir {
			n += f.Size
		}
	}
	return
}

// FileCount of the non-directory entries.
func (o Offer) FileCount() (n int) {
	for _, f := range o.Files {
		if !f.Dir {
			n++
		}
	}
	return
}

type accept struct {
	Offsets map[string]int64 `json:"offsets,omitempty"` // index -> bytes the receiver already has
}

type Result struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Progress is reported during a transfer. Indices refer to the data files
// only (directories excluded).
type Progress struct {
	Done, Total int64
	Resumed     int64 // bytes not sent again thanks to resume
	FileIndex   int
	FileCount   int
	FileName    string
	FileDone    []bool
	Elapsed     time.Duration
}

// Events lets the UI follow a transfer. All callbacks are optional.
type Events struct {
	Peer     func(PeerInfo)
	Offer    func(Offer)
	Progress func(Progress)
	// Waiting is called on the sender while the receiver decides.
	Waiting func()
	// Confirm is called on the receiver; the answer is read from the channel.
	// No answer within ConfirmTimeout means "rejected".
	Confirm func(PeerInfo, Offer) <-chan bool
	// Reply is called on the device that typed an assisted-mode code: the
	// reply code to show (only needed if the peer's router is strict).
	Reply func(code string, nat NATInfo)
}

var (
	ErrRejected    = errors.New("rejected")
	ErrNoAnswer    = errors.New("no answer")
	ErrPeerRejects = errors.New("peer rejected")
)

func (c *Channel) writeMsg(t byte, payload []byte) error {
	return c.Write(append([]byte{t}, payload...))
}

func (c *Channel) writeJSON(t byte, v any) error {
	b, _ := json.Marshal(v)
	return c.writeMsg(t, b)
}

func (c *Channel) readMsg() (byte, []byte, error) {
	m, err := c.Read()
	if err != nil {
		return 0, nil, err
	}
	if len(m) == 0 {
		return 0, nil, errors.New(Tr("messaggio vuoto", "empty message"))
	}
	if m[0] == msgCancel {
		return 0, nil, fmt.Errorf(Tr("annullato dall'altro dispositivo: %s", "cancelled by the other device: %s"), string(m[1:]))
	}
	return m[0], m[1:], nil
}

func (c *Channel) expect(t byte) ([]byte, error) {
	got, p, err := c.readMsg()
	if err != nil {
		return nil, err
	}
	if got != t {
		return nil, fmt.Errorf(Tr("protocollo: atteso %#x, ricevuto %#x", "protocol: expected %#x, got %#x"), t, got)
	}
	return p, nil
}

func (c *Channel) exchangeHello(me PeerInfo) (PeerInfo, error) {
	var peer PeerInfo
	if err := c.writeJSON(msgHello, me); err != nil {
		return peer, err
	}
	p, err := c.expect(msgHello)
	if err != nil {
		return peer, err
	}
	json.Unmarshal(p, &peer)
	return peer, nil
}

// Cancel tells the peer we are stopping.
func (c *Channel) Cancel(reason string) { c.writeMsg(msgCancel, []byte(reason)) }

type reporter struct {
	ev    Events
	p     Progress
	start time.Time
	last  time.Time
}

func (r *reporter) report(force bool) {
	if r.ev.Progress != nil && (force || time.Since(r.last) > 100*time.Millisecond) {
		r.last = time.Now()
		r.p.Elapsed = time.Since(r.start)
		cp := r.p
		cp.FileDone = append([]bool(nil), r.p.FileDone...)
		r.ev.Progress(cp)
	}
}

// ---------------------------------------------------------------- sender

// SendFiles runs the sender side over an established channel.
func SendFiles(c *Channel, me PeerInfo, files []FileInfo, ev Events) (PeerInfo, error) {
	if me.Role == "" {
		me.Role = RoleSend
	}
	return sendFiles(c, me, files, ev, false)
}

func sendFiles(c *Channel, me PeerInfo, files []FileInfo, ev Events, strict bool) (PeerInfo, error) {
	peer, err := c.exchangeHello(me)
	if err != nil {
		return peer, err
	}
	if err := checkRoles(c, me, peer, strict); err != nil {
		return peer, err
	}
	if ev.Peer != nil {
		ev.Peer(peer)
	}
	offer := Offer{Files: files}
	if err := c.writeJSON(msgOffer, offer); err != nil {
		return peer, err
	}
	if ev.Waiting != nil {
		ev.Waiting()
	}
	t, p, err := c.readMsg()
	if err != nil {
		return peer, err
	}
	if t == msgReject {
		if string(p) == "timeout" {
			return peer, ErrNoAnswer
		}
		return peer, ErrPeerRejects
	}
	if t != msgAccept {
		return peer, errors.New(Tr("errore di protocollo", "protocol error"))
	}
	var acc accept
	json.Unmarshal(p, &acc)

	r := &reporter{ev: ev, start: time.Now()}
	r.p.Total = offer.TotalSize()
	r.p.FileCount = offer.FileCount()
	r.p.FileDone = make([]bool, r.p.FileCount)

	// From now on the receiver may talk while we send: read in the background.
	type inMsg struct {
		t   byte
		p   []byte
		err error
	}
	in := make(chan inMsg, 64)
	go func() {
		for {
			t, p, err := c.readMsg()
			in <- inMsg{t, p, err}
			if err != nil || t == msgResult {
				return
			}
		}
	}()
	acks := ackedProgress(peer)
	var ends []int64 // cumulative end of each data file, for acked progress
	var names []string
	var cum int64
	for _, f := range files {
		if !f.Dir {
			cum += f.Size
			ends = append(ends, cum)
			names = append(names, f.Name)
		}
	}
	var result *inMsg
	// showAcked turns the receiver's count into the progress shown here
	showAcked := func(n int64, force bool) {
		r.p.Done = n
		r.p.FileIndex = len(ends) - 1
		for i, e := range ends {
			r.p.FileDone[i] = n >= e
			if n < e && i < r.p.FileIndex {
				r.p.FileIndex = i
			}
		}
		if r.p.FileIndex >= 0 {
			r.p.FileName = names[r.p.FileIndex]
		}
		r.report(force)
	}
	// poll handles what the receiver said so far (without waiting)
	poll := func() error {
		for {
			select {
			case m := <-in:
				if m.err != nil {
					return m.err
				}
				switch m.t {
				case msgAcked:
					if acks && len(m.p) == 8 {
						showAcked(int64(binary.BigEndian.Uint64(m.p)), false)
					}
				case msgResult:
					result = &m
					return nil
				}
			default:
				return nil
			}
		}
	}

	buf := make([]byte, chunkSize)
	var sentTotal int64
	di := -1
	for i, f := range files {
		if f.Dir {
			continue
		}
		di++
		off := acc.Offsets[strconv.Itoa(i)]
		if off < 0 || off > f.Size {
			off = 0
		}
		if !acks {
			r.p.FileIndex, r.p.FileName = di, f.Name
		}
		if off == f.Size && f.Size > 0 { // receiver already has the whole file
			sentTotal += f.Size
			r.p.Resumed += f.Size
			if !acks {
				r.p.Done = sentTotal
				r.p.FileDone[di] = true
				r.report(true)
			}
			continue
		}
		if !acks {
			r.report(true)
		}
		fh, err := os.Open(f.Path)
		if err != nil {
			c.Cancel(Tr("file non leggibile", "file not readable"))
			return peer, fmt.Errorf(Tr("impossibile aprire %s: %v", "cannot open %s: %v"), f.Name, err)
		}
		h := sha256.New()
		// hash the part the receiver already has, without sending it
		if off > 0 {
			if _, err := io.CopyN(h, fh, off); err != nil {
				fh.Close()
				c.Cancel(Tr("errore di lettura", "read error"))
				return peer, err
			}
			sentTotal += off
			r.p.Resumed += off
		}
		sent := off
		for {
			n, rerr := fh.Read(buf)
			if n > 0 {
				h.Write(buf[:n])
				if err := c.writeMsg(msgData, buf[:n]); err != nil {
					fh.Close()
					return peer, err
				}
				sent += int64(n)
				sentTotal += int64(n)
				if !acks {
					r.p.Done = sentTotal
					r.report(false)
				}
				if err := poll(); err != nil {
					fh.Close()
					return peer, err
				}
				if result != nil {
					fh.Close()
					return peer, resultErr(result.p)
				}
				if testSendDelay > 0 {
					time.Sleep(testSendDelay)
				}
			}
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				fh.Close()
				c.Cancel(Tr("errore di lettura", "read error"))
				return peer, rerr
			}
		}
		fh.Close()
		if sent != f.Size {
			c.Cancel(Tr("il file è cambiato durante l'invio", "the file changed while being sent"))
			return peer, fmt.Errorf(Tr("%s è cambiato durante l'invio", "%s changed while being sent"), f.Name)
		}
		if err := c.writeMsg(msgFileEnd, h.Sum(nil)); err != nil {
			return peer, err
		}
		if !acks {
			r.p.FileDone[di] = true
			r.report(true)
		}
	}
	if err := c.writeMsg(msgDone, nil); err != nil {
		return peer, err
	}
	// wait for the result, following the receiver's progress meanwhile
	for result == nil {
		m := <-in
		if m.err != nil {
			return peer, m.err
		}
		switch m.t {
		case msgAcked:
			if acks && len(m.p) == 8 {
				showAcked(int64(binary.BigEndian.Uint64(m.p)), false)
			}
		case msgResult:
			result = &m
		default:
			return peer, fmt.Errorf(Tr("protocollo: messaggio inatteso %#x", "protocol: unexpected message %#x"), m.t)
		}
	}
	if err := resultErr(result.p); err != nil {
		return peer, err
	}
	if acks {
		showAcked(r.p.Total, true)
	}
	return peer, nil
}

func resultErr(p []byte) error {
	var res Result
	json.Unmarshal(p, &res)
	if !res.OK {
		return fmt.Errorf(Tr("il destinatario segnala: %s", "the receiver reports: %s"), res.Error)
	}
	return nil
}

// ---------------------------------------------------------------- receiver

// Store decides where received files go and remembers partial files.
type Store interface {
	// Prepare is called once per transfer, before accepting. It returns, for
	// each offer index, the bytes already present (== size: complete).
	Prepare(peer PeerInfo, files []FileInfo) (map[int]int64, error)
	// MakeDir creates a directory entry.
	MakeDir(index int, f FileInfo) error
	// Open returns a writer positioned at offset and a reader over the first
	// offset bytes already stored (nil when offset is 0).
	Open(index int, f FileInfo, offset int64) (Target, io.ReadCloser, error)
	// Existing returns the location of a file skipped because complete.
	Existing(index int) string
	// Root returns the main location to show/open after the transfer.
	Root() string
}

// Target is a file being received.
type Target interface {
	io.Writer
	// Commit makes the file final and returns its location.
	Commit() (string, error)
	// Interrupt keeps the partial file for a later resume.
	Interrupt(written int64)
	// Discard deletes the partial file.
	Discard()
}

// SafeName strips any path component and forbidden characters from a name.
func SafeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base("/" + name)
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimRight(strings.TrimSpace(name), ". ")
	if name == "" || name == "." || name == ".." {
		name = "file"
	}
	base := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "LPT1", "LPT2", "LPT3":
		name = "_" + name
	}
	return name
}

// SafeRelPath turns a received relative path into safe segments
// (no "..", no absolute paths, no forbidden characters).
func SafeRelPath(name string) []string {
	name = strings.ReplaceAll(name, "\\", "/")
	var out []string
	for _, s := range strings.Split(name, "/") {
		s = strings.TrimSpace(s)
		if s == "" || s == "." || s == ".." {
			continue
		}
		out = append(out, SafeName(s))
	}
	if len(out) == 0 {
		out = []string{"file"}
	}
	if len(out) > 32 {
		out = out[len(out)-32:]
	}
	return out
}

// ReceiveFiles runs the receiver side. cancel aborts a pending confirmation.
func ReceiveFiles(c *Channel, me PeerInfo, st Store, ev Events, cancel <-chan struct{}) (PeerInfo, []string, error) {
	if me.Role == "" {
		me.Role = RoleRecv
	}
	return receiveFiles(c, me, st, ev, cancel, false)
}

func receiveFiles(c *Channel, me PeerInfo, st Store, ev Events, cancel <-chan struct{}, strict bool) (PeerInfo, []string, error) {
	peer, err := c.exchangeHello(me)
	if err != nil {
		return peer, nil, err
	}
	if err := checkRoles(c, me, peer, strict); err != nil {
		return peer, nil, err
	}
	if ev.Peer != nil {
		ev.Peer(peer)
	}
	p, err := c.expect(msgOffer)
	if err != nil {
		return peer, nil, err
	}
	var offer Offer
	if err := json.Unmarshal(p, &offer); err != nil || len(offer.Files) == 0 || len(offer.Files) > 100000 {
		c.Cancel(Tr("offerta non valida", "invalid offer"))
		return peer, nil, errors.New(Tr("offerta non valida", "invalid offer"))
	}
	for _, f := range offer.Files {
		if f.Size < 0 {
			c.Cancel(Tr("offerta non valida", "invalid offer"))
			return peer, nil, errors.New(Tr("offerta non valida", "invalid offer"))
		}
	}
	if ev.Offer != nil {
		ev.Offer(offer)
	}
	if ev.Confirm != nil {
		answer := ev.Confirm(peer, offer)
		select {
		case ok := <-answer:
			if !ok {
				c.writeMsg(msgReject, []byte("no"))
				return peer, nil, ErrRejected
			}
		case <-time.After(ConfirmTimeout):
			c.writeMsg(msgReject, []byte("timeout"))
			return peer, nil, ErrNoAnswer
		case <-cancel:
			c.writeMsg(msgReject, []byte("no"))
			return peer, nil, ErrCancelled
		}
	}
	offsets, err := st.Prepare(peer, offer.Files)
	if err != nil {
		c.Cancel(Tr("cartella di destinazione non disponibile", "destination folder not available"))
		return peer, nil, err
	}
	acc := accept{Offsets: map[string]int64{}}
	for i, o := range offsets {
		if o > 0 && i >= 0 && i < len(offer.Files) && o <= offer.Files[i].Size {
			acc.Offsets[strconv.Itoa(i)] = o
		} else {
			delete(offsets, i)
		}
	}
	if err := c.writeJSON(msgAccept, acc); err != nil {
		return peer, nil, err
	}

	var saved []string
	acks := ackedProgress(peer)
	var lastAck time.Time
	r := &reporter{ev: ev, start: time.Now()}
	sendAck := func(force bool) {
		if acks && (force || time.Since(lastAck) > 200*time.Millisecond) {
			lastAck = time.Now()
			var b [8]byte
			binary.BigEndian.PutUint64(b[:], uint64(r.p.Done))
			r.report(true) // show here exactly what the sender will show
			c.writeMsg(msgAcked, b[:])
		}
	}
	r.p.Total = offer.TotalSize()
	r.p.FileCount = offer.FileCount()
	r.p.FileDone = make([]bool, r.p.FileCount)
	result := func(ok bool, why string) {
		if !ok {
			c.writeJSON(msgResult, Result{OK: false, Error: why})
		}
	}
	di := -1
	for i, f := range offer.Files {
		if f.Dir {
			if err := st.MakeDir(i, f); err != nil {
				result(false, Tr("impossibile scrivere il file", "cannot write the file"))
				return peer, saved, err
			}
			continue
		}
		di++
		name := strings.Join(SafeRelPath(f.Name), "/")
		r.p.FileIndex, r.p.FileName = di, name
		off := offsets[i]
		if off == f.Size && f.Size > 0 {
			saved = append(saved, st.Existing(i))
			r.p.Done += f.Size
			r.p.Resumed += f.Size
			r.p.FileDone[di] = true
			r.report(true)
			continue
		}
		r.report(true)
		tg, prefix, err := st.Open(i, f, off)
		if err != nil {
			result(false, Tr("impossibile scrivere il file", "cannot write the file"))
			return peer, saved, err
		}
		h := sha256.New()
		if off > 0 && prefix != nil {
			n, err := io.CopyN(h, prefix, off)
			prefix.Close()
			if err != nil || n != off {
				tg.Discard()
				result(false, Tr("errore di lettura", "read error"))
				return peer, saved, errors.New(Tr("file parziale non leggibile", "partial file not readable"))
			}
			r.p.Done += off
			r.p.Resumed += off
		}
		got := off
		var fileHash []byte
		for {
			t, data, err := c.readMsg()
			if err != nil {
				tg.Interrupt(got) // connection lost or cancelled: keep for resume
				return peer, saved, err
			}
			if t == msgData {
				if got+int64(len(data)) > f.Size {
					tg.Discard()
					result(false, Tr("dimensione errata", "wrong size"))
					return peer, saved, errors.New(Tr("dati oltre la dimensione dichiarata", "data beyond the declared size"))
				}
				h.Write(data)
				if _, err := tg.Write(data); err != nil {
					tg.Interrupt(got)
					result(false, Tr("errore di scrittura sul destinatario", "write error on the receiver"))
					return peer, saved, fmt.Errorf(Tr("errore di scrittura (disco pieno?): %v", "write error (disk full?): %v"), err)
				}
				got += int64(len(data))
				r.p.Done += int64(len(data))
				if acks {
					sendAck(false)
				} else {
					r.report(false)
				}
				continue
			}
			if t == msgFileEnd {
				fileHash = data
				break
			}
			tg.Discard()
			result(false, Tr("errore di protocollo", "protocol error"))
			return peer, saved, fmt.Errorf(Tr("protocollo: messaggio inatteso %#x", "protocol: unexpected message %#x"), t)
		}
		if got != f.Size {
			tg.Discard()
			result(false, Tr("file incompleto", "incomplete file"))
			return peer, saved, errors.New(Tr("file incompleto", "incomplete file"))
		}
		if !equal32(fileHash, h.Sum(nil)) {
			tg.Discard()
			result(false, Tr("verifica d'integrità fallita", "integrity check failed"))
			return peer, saved, fmt.Errorf(Tr("verifica d'integrità fallita per %s", "integrity check failed for %s"), name)
		}
		loc, err := tg.Commit()
		if err != nil {
			result(false, Tr("impossibile salvare il file", "cannot save the file"))
			return peer, saved, err
		}
		saved = append(saved, loc)
		r.p.FileDone[di] = true
		r.report(true)
		sendAck(true)
	}
	if _, err := c.expect(msgDone); err != nil {
		return peer, saved, err
	}
	c.writeJSON(msgResult, Result{OK: true})
	return peer, saved, nil
}

func equal32(a, b []byte) bool {
	if len(a) != 32 || len(b) != 32 {
		return false
	}
	var d byte
	for i := range a {
		d |= a[i] ^ b[i]
	}
	return d == 0
}
