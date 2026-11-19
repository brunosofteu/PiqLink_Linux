package main

// The window: a page on this computer only (127.0.0.1), shown by the browser.
// The engine runs here, natively, exactly like the Windows and Android apps.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"piqlink/core"
)

//go:embed ui
var uiFS embed.FS

type itemView struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Files int    `json:"files"`
	Dir   bool   `json:"dir"`
}

type fileView struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type stateView struct {
	Screen    string     `json:"screen"`
	Version   string     `json:"version"`
	Lang      string     `json:"lang"`
	Name      string     `json:"name"`
	Dir       string     `json:"dir"`
	Confirm   bool       `json:"confirm"`
	Picker    bool       `json:"picker"`
	MenuAsk   bool       `json:"menuAsk"`
	Items     []itemView `json:"items"`
	Total     int64      `json:"total"`
	NFiles    int        `json:"nfiles"`
	Notice    string     `json:"notice"`
	Mode      string     `json:"mode"` // lan | net
	RState    string     `json:"rstate"`
	Code      string     `json:"code"`
	QR        []string   `json:"qr"`
	Expires   int64      `json:"expires"` // unix ms
	RErr      string     `json:"rerr"`
	SendErr   string     `json:"sendErr"`
	Busy      bool       `json:"busy"`
	Connect   bool       `json:"connecting"`
	WaitPeer  bool       `json:"waitPeer"`
	Sending   bool       `json:"sending"`
	Peer      string     `json:"peer"`
	PeerOS    string     `json:"peerOS"`
	Offer     []fileView `json:"offer"`
	OfferTot  int64      `json:"offerTotal"`
	AskUntil  int64      `json:"askUntil"`
	Done      int64      `json:"done"`
	PTotal    int64      `json:"ptotal"`
	PFile     string     `json:"pfile"`
	PIndex    int        `json:"pindex"`
	PCount    int        `json:"pcount"`
	Elapsed   float64    `json:"elapsed"`
	ResTitle  string     `json:"resTitle"`
	ResText   string     `json:"resText"`
	ResOK     bool       `json:"resOK"`
	ResOpen   bool       `json:"resOpen"`
	Saved     int        `json:"saved"`
	Assisted  bool       `json:"assisted"`  // consent given
	Assisting bool       `json:"assisting"` // finding the public address
	P2P       bool       `json:"p2p"`       // the code shown is an assisted one
	NeedAsst  bool       `json:"needAssist"`
	Reply     string     `json:"reply"`    // reply code to dictate (connector)
	ReplyMsg  string     `json:"replyMsg"` // owner: "ok" or an error
	Prev      string     `json:"prev"`
}

type gui struct {
	mu          sync.Mutex
	set         *Settings
	st          stateView
	op          int
	stop        context.CancelFunc
	items       []*core.Item
	answer      chan bool
	result      string // what "Apri" opens after receiving
	recv        *core.Receiver
	upload      string // temp folder for files added from the page
	consentNext func()

	subs     map[chan []byte]bool
	clients  int
	lastSeen time.Time
	token    string
}

func newGUI(set *Settings) *gui {
	b := make([]byte, 16)
	rand.Read(b)
	g := &gui{set: set, subs: map[chan []byte]bool{}, token: hex.EncodeToString(b), lastSeen: time.Now()}
	g.st.Screen = "home"
	g.st.MenuAsk = !set.MenuAsked
	return g
}

// ---------------------------------------------------------------- state

func (g *gui) me() core.PeerInfo {
	return core.PeerInfo{Name: g.set.deviceName(), OS: "linux", Ver: core.AppVersion, ID: g.set.DeviceID}
}

// changed sends the state to the open windows (call with g.mu held).
func (g *gui) changed() {
	s := g.st
	s.Version = core.AppVersion
	s.Lang = g.set.Lang
	s.Name = g.set.deviceName()
	s.Dir = g.set.saveDir()
	s.Confirm = g.set.Confirm
	s.Picker = pickerKind() != ""
	s.Items = nil
	s.Total, s.NFiles = 0, 0
	for _, it := range g.items {
		s.Items = append(s.Items, itemView{Name: it.Name, Size: it.Size, Files: it.Files, Dir: it.IsDir})
		s.Total += it.Size
		s.NFiles += it.Files
	}
	s.Busy = g.stop != nil
	s.Assisted = core.STUNAllowed()
	b, _ := json.Marshal(s)
	for ch := range g.subs {
		select {
		case ch <- b:
		default: // slow window: it gets the next one
		}
	}
}

func (g *gui) update(fn func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	fn()
	g.changed()
}

// cancelOp stops what is running (call with g.mu held).
func (g *gui) cancelOp() {
	g.op++
	if g.stop != nil {
		g.stop()
		g.stop = nil
	}
	if g.answer != nil {
		select {
		case g.answer <- false:
		default:
		}
		g.answer = nil
	}
	g.recv = nil
	g.st.Connect, g.st.WaitPeer = false, false
}

func (g *gui) store() *core.DirStore {
	os.MkdirAll(g.set.saveDir(), 0o755)
	index := filepath.Join(configDir(), "resume.json")
	if g.set.Dir != "" { // a chosen folder: its own resume list
		h := sha256.Sum256([]byte(g.set.Dir))
		index = filepath.Join(configDir(), "resume-"+hex.EncodeToString(h[:6])+".json")
	}
	return &core.DirStore{Dir: g.set.saveDir(), IndexPath: index}
}

func qrRows(payload string) []string {
	q, err := core.EncodeQR(payload)
	if err != nil {
		return nil
	}
	rows := make([]string, q.Size)
	for y := 0; y < q.Size; y++ {
		var b strings.Builder
		for x := 0; x < q.Size; x++ {
			if q.Modules[y][x] {
				b.WriteByte('1')
			} else {
				b.WriteByte('0')
			}
		}
		rows[y] = b.String()
	}
	return rows
}

func (g *gui) events(op int) core.Events {
	return core.Events{
		Peer: func(p core.PeerInfo) {
			g.update(func() {
				if g.op == op {
					g.st.Peer, g.st.PeerOS = p.Name, p.OS
				}
			})
		},
		Offer: func(o core.Offer) {
			g.update(func() {
				if g.op == op {
					g.st.Offer = offerView(o)
					g.st.OfferTot = o.TotalSize()
				}
			})
		},
		Progress: func(p core.Progress) {
			g.update(func() {
				if g.op != op {
					return
				}
				if g.st.Screen != "transfer" {
					g.st.Screen = "transfer"
					g.st.Connect, g.st.WaitPeer = false, false
				}
				g.st.Done, g.st.PTotal = p.Done, p.Total
				g.st.PFile, g.st.PIndex, g.st.PCount = p.FileName, p.FileIndex, p.FileCount
				g.st.Elapsed = p.Elapsed.Seconds()
			})
		},
		Waiting: func() {
			g.update(func() {
				if g.op == op {
					g.st.WaitPeer = true
				}
			})
		},
	}
}

func offerView(o core.Offer) []fileView {
	var v []fileView
	for _, f := range o.Files {
		if !f.Dir {
			v = append(v, fileView{Name: f.Name, Size: f.Size})
		}
	}
	return v
}

func (g *gui) confirmFn(op int) func(core.PeerInfo, core.Offer) <-chan bool {
	if !g.set.Confirm {
		return nil
	}
	return func(p core.PeerInfo, o core.Offer) <-chan bool {
		ch := make(chan bool, 1)
		g.update(func() {
			if g.op != op {
				ch <- false
				return
			}
			g.answer = ch
			g.st.Peer, g.st.PeerOS = p.Name, p.OS
			g.st.Offer, g.st.OfferTot = offerView(o), o.TotalSize()
			g.st.AskUntil = time.Now().Add(core.ConfirmTimeout).UnixMilli()
			g.st.Screen = "confirm"
		})
		return ch
	}
}

func (g *gui) allEntries() []core.FileInfo {
	var files []core.FileInfo
	for _, it := range g.items {
		files = append(files, it.Entries...)
	}
	return files
}

// ---------------------------------------------------------------- actions

func (g *gui) addPaths(paths []string) {
	seen := map[string]bool{}
	for _, it := range g.items {
		seen[it.Path] = true
	}
	bad := 0
	for _, p := range paths {
		it, err := core.CollectItem(p)
		if err != nil {
			bad++
			continue
		}
		if seen[it.Path] {
			continue
		}
		seen[it.Path] = true
		g.items = append(g.items, it)
	}
	g.st.Notice = ""
	if bad > 0 {
		g.st.Notice = core.Tr("Alcuni elementi non sono leggibili e sono stati saltati.", "Some items are not readable and were skipped.")
	}
}

// startReceive: this computer shows a code (mode lan: same network, net: router port).
func (g *gui) startReceive(mode string, sending bool) {
	g.cancelOp()
	g.st.Mode, g.st.RState, g.st.RErr, g.st.Code, g.st.QR, g.st.SendErr = mode, "starting", "", "", nil, ""
	g.st.Assisting, g.st.P2P, g.st.NeedAsst, g.st.ReplyMsg, g.st.Reply = false, false, false, "", ""
	g.st.Peer, g.st.Sending = "", sending
	g.st.Screen = "recv"
	if sending {
		g.st.Screen = "sendwait"
	}
	op := g.op
	ctx, cancel := context.WithCancel(context.Background())
	g.stop = cancel
	files := g.allEntries()
	go func() {
		m := core.ModeLAN
		if mode == "net" {
			m = core.ModeNET
		}
		r, err := core.StartReceiver(m)
		var na *core.ErrNeedAssisted
		if err != nil && errors.As(err, &na) && core.STUNAllowed() {
			g.update(func() {
				if g.op == op {
					g.st.Assisting = true
				}
			})
			r, err = core.StartReceiverP2P(ctx)
			if err != nil {
				err = errors.New(err.Error())
			}
		}
		g.mu.Lock()
		if g.op != op {
			g.mu.Unlock()
			if r != nil {
				r.Close()
			}
			return
		}
		if err != nil {
			g.st.RState, g.st.RErr, g.st.Assisting = "failed", friendly(err), false
			if errors.As(err, &na) {
				g.st.RErr = friendly(errors.New(na.Reason))
				g.st.NeedAsst = true
			}
			g.stop = nil
			g.changed()
			g.mu.Unlock()
			return
		}
		g.recv = r
		g.st.RState, g.st.Assisting = "waiting", false
		g.st.P2P = r.Mode == core.ModeP2P
		g.st.Code = r.Code.Display()
		g.st.QR = qrRows(r.Code.QRPayload())
		g.st.Expires = r.Expires.UnixMilli()
		g.changed()
		g.mu.Unlock()
		ev := g.events(op)
		if sending {
			peer, err := r.WaitSend(ctx, g.me(), files, ev, nil)
			g.finish(op, peer, nil, err, true, true)
		} else {
			ev.Confirm = g.confirmFn(op)
			peer, saved, err := r.Wait(ctx, g.me(), g.store(), ev, nil)
			g.finish(op, peer, saved, err, false, true)
		}
	}()
}

// askConsent shows the consent of the assisted connection; next runs on "yes".
func (g *gui) askConsent(next func()) {
	g.consentNext = next
	if g.st.Screen != "consent" {
		g.st.Prev = g.st.Screen
	}
	g.st.Screen = "consent"
}

// startConnect: this computer types the other's code (send: to a receiver, else from a sender).
func (g *gui) startConnect(text string, sending bool) {
	code, err := core.Parse(text)
	switch {
	case err != nil:
		g.st.SendErr = core.Tr("Codice non valido: controlla i caratteri.", "Invalid code: check the characters.")
		if len(core.Normalize(text)) == 10 {
			g.st.SendErr = core.Tr("Questo è un codice di risposta (10 caratteri): va scritto nel riquadro «Codice di risposta» del dispositivo che mostra il codice lungo, non qui.", "This is a reply code (10 characters): it goes in the “Reply code” box of the device showing the long code, not here.")
		}
		return
	case code.Kind == core.KindP2P && !core.STUNAllowed():
		g.askConsent(func() { g.startConnect(text, sending) })
		return
	}
	g.cancelOp()
	g.st.SendErr, g.st.Connect, g.st.Sending, g.st.WaitPeer, g.st.Peer = "", true, sending, false, ""
	g.st.Reply = ""
	op := g.op
	ctx, cancel := context.WithCancel(context.Background())
	g.stop = cancel
	files := g.allEntries()
	ev := g.events(op)
	ev.Reply = func(code string, nat core.NATInfo) {
		g.update(func() {
			if g.op == op {
				g.st.Reply = code
			}
		})
	}
	go func() {
		if sending {
			peer, err := core.SendTo(ctx, text, g.me(), files, ev)
			g.finish(op, peer, nil, err, true, false)
		} else {
			ev.Confirm = g.confirmFn(op)
			peer, saved, err := core.ReceiveFrom(ctx, text, g.me(), g.store(), ev)
			g.finish(op, peer, saved, err, false, false)
		}
	}()
}

func (g *gui) finish(op int, peer core.PeerInfo, saved []string, err error, sending, listener bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.op != op {
		return
	}
	g.stop, g.answer, g.recv = nil, nil, nil
	g.st.Connect, g.st.WaitPeer = false, false
	if errors.Is(err, core.ErrCancelled) || errors.Is(err, context.Canceled) {
		g.changed()
		return
	}
	if err != nil {
		msg := explain(err, sending, listener)
		if g.st.Screen == "transfer" || g.st.Screen == "confirm" || g.st.Screen == "recv" || g.st.Screen == "sendwait" {
			g.st.Screen = "result"
			g.st.ResOK, g.st.ResOpen = false, false
			g.st.ResTitle = core.Tr("Non riuscito", "Failed")
			g.st.ResText = msg
		} else {
			g.st.SendErr = msg // still on the code screen: show it there
		}
		g.changed()
		return
	}
	g.st.Screen = "result"
	g.st.ResOK = true
	name := peer.Name
	if name == "" {
		name = core.Tr("l'altro dispositivo", "the other device")
	}
	if sending {
		g.st.ResTitle = core.Tr("Inviato", "Sent")
		g.st.ResText = name
		g.st.ResOpen = false
		g.st.Saved = 0
		for _, it := range g.items {
			g.st.Saved += it.Files
		}
		g.items = nil
		g.cleanUpload()
	} else {
		g.st.ResTitle = core.Tr("Ricevuto", "Received")
		g.st.ResText = name
		g.st.Saved = len(saved)
		g.result = openTarget(g.set.saveDir(), saved)
		g.st.ResOpen = true
	}
	g.changed()
}

func (g *gui) cleanUpload() {
	if g.upload != "" {
		os.RemoveAll(g.upload)
		g.upload = ""
	}
}

// openTarget: the single file, the common top folder, or the destination folder.
func openTarget(root string, saved []string) string {
	if len(saved) == 1 {
		return filepath.Dir(saved[0])
	}
	return root
}

func explain(err error, sending, listener bool) string {
	switch {
	case errors.Is(err, core.ErrOldPeer):
		return core.Tr("L'altro dispositivo ha una versione di PiqLink precedente alla 2.0, che non conosce questo modo. Aggiornalo, oppure fai generare il codice a chi riceve.", "The other device has a PiqLink version older than 2.0, which does not know this mode. Update it, or let the receiver generate the code.")
	case errors.Is(err, core.ErrSameRole):
		switch {
		case sending && listener:
			return core.Tr("L'altro dispositivo ha inserito il codice in INVIA. Per ricevere i tuoi file deve scegliere RICEVI › «Ho il codice del mittente».", "The other device entered the code in SEND. To receive your files it must choose RECEIVE › “I have the sender's code”.")
		case sending:
			return core.Tr("Quel codice è di un dispositivo che sta inviando. Per ricevere i suoi file usa RICEVI › «Ho il codice del mittente».", "That code belongs to a device that is sending. To receive its files use RECEIVE › “I have the sender's code”.")
		case listener:
			return core.Tr("L'altro dispositivo ha inserito il codice in RICEVI. Per mandarti dei file deve usare INVIA.", "The other device entered the code in RECEIVE. To send you files it must use SEND.")
		default:
			return core.Tr("Quel codice è di un dispositivo che sta ricevendo. Per mandargli dei file usa INVIA.", "That code belongs to a device that is receiving. To send it files use SEND.")
		}
	}
	return friendly(err)
}

func friendly(err error) string {
	s := err.Error()
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "connection reset"), strings.Contains(low, "eof"), strings.Contains(low, "broken pipe"), strings.Contains(low, "use of closed"):
		return core.Tr("La connessione con l'altro dispositivo si è interrotta.", "The connection with the other device was interrupted.")
	case strings.Contains(low, "no space"):
		return core.Tr("Spazio su disco insufficiente.", "Not enough disk space.")
	}
	if len(s) > 0 {
		s = strings.ToUpper(s[:1]) + s[1:]
	}
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

// ---------------------------------------------------------------- HTTP

func (g *gui) authorized(r *http.Request) bool {
	h := r.Host
	if i := strings.LastIndex(h, ":"); i >= 0 {
		h = h[:i]
	}
	if h != "127.0.0.1" && h != "localhost" {
		return false // DNS rebinding protection
	}
	t := r.Header.Get("X-Piq-Token")
	if t == "" {
		t = r.URL.Query().Get("t")
	}
	return subtle.ConstantTimeCompare([]byte(t), []byte(g.token)) == 1
}

func (g *gui) handler(icon []byte) http.Handler {
	mux := http.NewServeMux()
	ui, _ := fs.Sub(uiFS, "ui")
	files := http.FileServer(http.FS(ui))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'")
		files.ServeHTTP(w, r)
	}))
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		if !g.authorized(r) {
			http.Error(w, "forbidden", 403)
			return
		}
		fl, ok := w.(http.Flusher)
		if !ok {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		ch := make(chan []byte, 8)
		g.mu.Lock()
		g.subs[ch] = true
		g.clients++
		g.changed()
		g.mu.Unlock()
		defer func() {
			g.mu.Lock()
			delete(g.subs, ch)
			g.clients--
			g.lastSeen = time.Now()
			g.mu.Unlock()
		}()
		tick := time.NewTicker(15 * time.Second)
		defer tick.Stop()
		for {
			select {
			case b := <-ch:
				w.Write([]byte("data: "))
				w.Write(b)
				w.Write([]byte("\n\n"))
				fl.Flush()
			case <-tick.C:
				w.Write([]byte(": ping\n\n"))
				fl.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	mux.HandleFunc("/api/do", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !g.authorized(r) {
			http.Error(w, "forbidden", 403)
			return
		}
		var req struct {
			Act  string `json:"act"`
			Arg  string `json:"arg"`
			Bool bool   `json:"bool"`
			N    int    `json:"n"`
		}
		json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req)
		g.do(req.Act, req.Arg, req.Bool, req.N, icon)
		w.Write([]byte("{}"))
	})
	// files added from the page (browser picker or drag & drop)
	mux.HandleFunc("/api/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !g.authorized(r) {
			http.Error(w, "forbidden", 403)
			return
		}
		rel := cleanRel(r.URL.Query().Get("path"))
		if rel == "" {
			http.Error(w, "path", 400)
			return
		}
		g.mu.Lock()
		if g.upload == "" {
			g.upload, _ = os.MkdirTemp(cacheDir(), "upload-")
		}
		dir := g.upload
		g.mu.Unlock()
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(dst), 0o700)
		f, err := os.Create(dst)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_, err = io.Copy(f, r.Body)
		f.Close()
		if err != nil {
			os.Remove(dst)
			http.Error(w, err.Error(), 500)
			return
		}
		w.Write([]byte("{}"))
	})
	return mux
}

func cleanRel(p string) string {
	var parts []string
	for _, x := range strings.Split(strings.ReplaceAll(p, "\\", "/"), "/") {
		if x == "" || x == "." || x == ".." {
			continue
		}
		parts = append(parts, x)
	}
	return strings.Join(parts, "/")
}

func (g *gui) do(act, arg string, b bool, n int, icon []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	defer g.changed()
	if g.stop != nil && (g.st.Screen == "transfer" || g.st.Screen == "confirm") {
		switch act {
		case "cancel", "answer", "open", "lang", "name", "confirm", "quit", "menu":
		default:
			return // a transfer is running: only these
		}
	}
	switch act {
	case "screen":
		if g.stop != nil && arg != g.st.Screen {
			if g.st.Screen == "transfer" {
				return // a transfer runs: only "cancel" stops it
			}
			g.cancelOp()
		}
		g.st.SendErr, g.st.Notice = "", ""
		g.st.Screen = arg
	case "recv":
		g.startReceive(arg, false)
	case "sendwait":
		if len(g.items) == 0 {
			return
		}
		g.startReceive(arg, true)
	case "connect":
		if b && len(g.items) == 0 {
			g.st.SendErr = core.Tr("Scegli prima i file da inviare.", "Choose the files to send first.")
			return
		}
		g.startConnect(arg, b)
	case "answer":
		if g.answer != nil {
			g.answer <- b
			g.answer = nil
			if !b {
				g.st.Screen = "home"
			} else {
				g.st.Screen = "transfer"
				g.st.Done, g.st.PTotal = 0, g.st.OfferTot
			}
		}
	case "cancel":
		g.cancelOp()
		if g.st.Screen == "sendwait" || g.st.Screen == "sendcode" {
			g.st.Screen = "send"
		} else {
			g.st.Screen = "home"
		}
	case "pick":
		if g.stop != nil {
			return
		}
		g.mu.Unlock()
		title := core.Tr("Scegli i file da inviare", "Choose the files to send")
		if b {
			title = core.Tr("Scegli una cartella da inviare", "Choose a folder to send")
		}
		paths := pickPaths(b, title)
		g.mu.Lock()
		if len(paths) > 0 {
			g.addPaths(paths)
			if g.st.Screen == "home" {
				g.st.Screen = "send"
			}
		}
	case "uploaded": // the page finished uploading these top-level names
		if g.upload == "" {
			return
		}
		var names []string
		json.Unmarshal([]byte(arg), &names)
		var paths []string
		for _, nm := range names {
			if nm = cleanRel(nm); nm != "" && !strings.Contains(nm, "/") {
				paths = append(paths, filepath.Join(g.upload, nm))
			}
		}
		g.addPaths(paths)
		if g.st.Screen == "home" {
			g.st.Screen = "send"
		}
	case "assist": // failed long code: use the assisted connection
		sending := g.st.Screen == "sendwait"
		run := func() { g.startReceive("net", sending) }
		if core.STUNAllowed() {
			run()
		} else {
			g.askConsent(run)
		}
	case "consent":
		next := g.consentNext
		g.consentNext = nil
		if b {
			g.set.Assisted = true
			g.set.save()
			core.SetSTUNAllowed(true)
			g.st.Screen = g.st.Prev
			if next != nil {
				next()
			}
		} else {
			g.st.Screen = g.st.Prev
			if g.st.Screen == "" {
				g.st.Screen = "home"
			}
		}
	case "assisted":
		g.set.Assisted = b
		g.set.save()
		core.SetSTUNAllowed(b)
	case "reply": // owner of an assisted code: the other device's reply code
		if g.recv != nil && g.recv.Mode == core.ModeP2P {
			if err := g.recv.SetReply(arg); err != nil {
				g.st.ReplyMsg = core.Tr("Codice di risposta non valido: controllalo (10 caratteri).", "Invalid reply code: check it (10 characters).")
			} else {
				g.st.ReplyMsg = "ok"
			}
		}
	case "remove":
		if n >= 0 && n < len(g.items) && g.stop == nil {
			g.items = append(g.items[:n], g.items[n+1:]...)
		}
	case "clear":
		if g.stop == nil {
			g.items = nil
			g.cleanUpload()
		}
	case "open":
		target := g.result
		if arg == "dir" || target == "" {
			target = g.set.saveDir()
		}
		os.MkdirAll(target, 0o755)
		xdgOpen(target)
	case "lang":
		switch arg {
		case "it", "en", "es", "zh":
			g.set.Lang = arg
			core.SetLang(arg)
			g.set.save()
		}
	case "name":
		g.set.Name = strings.TrimSpace(arg)
		if len(g.set.Name) > 40 {
			g.set.Name = g.set.Name[:40]
		}
		g.set.save()
	case "confirm":
		g.set.Confirm = b
		g.set.save()
	case "pickdir":
		g.mu.Unlock()
		p := pickPaths(true, core.Tr("Cartella dove salvare i file ricevuti", "Folder for received files"))
		g.mu.Lock()
		if len(p) == 1 {
			g.set.Dir = p[0]
			g.set.save()
		}
	case "setdir":
		if p := strings.TrimSpace(arg); filepath.IsAbs(p) {
			if os.MkdirAll(p, 0o755) == nil {
				g.set.Dir = p
				g.set.save()
			}
		}
	case "resetdir":
		g.set.Dir = ""
		g.set.save()
	case "menu":
		g.set.MenuAsked = true
		g.st.MenuAsk = false
		if b {
			if err := installMenu(icon); err != nil {
				g.st.Notice = friendly(err)
			} else {
				g.st.Notice = core.Tr("PiqLink è nel menu delle applicazioni.", "PiqLink is in the applications menu.")
			}
		}
		g.set.save()
	case "quit":
		go func() { time.Sleep(300 * time.Millisecond); os.Exit(0) }()
	}
}

// ---------------------------------------------------------------- start

type lockInfo struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

// runGUI starts the window (or reuses the one already open).
func runGUI(set *Settings, paths []string, icon []byte, noBrowser bool) error {
	lockPath := filepath.Join(configDir(), "gui.json")
	if b, err := os.ReadFile(lockPath); err == nil {
		var li lockInfo
		if json.Unmarshal(b, &li) == nil && li.Port > 0 {
			base := "http://127.0.0.1:" + strconv.Itoa(li.Port)
			c := http.Client{Timeout: 2 * time.Second}
			if len(paths) > 0 {
				body, _ := json.Marshal(map[string]string{"act": "addpaths", "arg": strings.Join(paths, "\n")})
				req, _ := http.NewRequest("POST", base+"/api/add", strings.NewReader(string(body)))
				req.Header.Set("X-Piq-Token", li.Token)
				if resp, err := c.Do(req); err == nil {
					resp.Body.Close()
				}
			}
			req, _ := http.NewRequest("GET", base+"/api/ping?t="+li.Token, nil)
			if resp, err := c.Do(req); err == nil {
				resp.Body.Close()
				if resp.StatusCode == 200 {
					if !noBrowser {
						openWindow(base + "/?t=" + li.Token)
					}
					return nil
				}
			}
		}
	}
	g := newGUI(set)
	if len(paths) > 0 {
		g.addPaths(paths)
		g.st.Screen = "send"
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	lb, _ := json.Marshal(lockInfo{Port: port, Token: g.token})
	os.WriteFile(lockPath, lb, 0o600)
	defer os.Remove(lockPath)

	h := g.handler(icon)
	mux := http.NewServeMux()
	mux.Handle("/", h)
	mux.HandleFunc("/api/ping", func(w http.ResponseWriter, r *http.Request) {
		if !g.authorized(r) {
			http.Error(w, "forbidden", 403)
			return
		}
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/api/add", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !g.authorized(r) {
			http.Error(w, "forbidden", 403)
			return
		}
		var req struct{ Arg string }
		json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
		g.update(func() {
			if g.stop == nil {
				g.addPaths(strings.Split(req.Arg, "\n"))
				g.st.Screen = "send"
			}
		})
		w.Write([]byte("{}"))
	})
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/?t=" + g.token
	if !noBrowser {
		openWindow(url)
	}
	os.Stdout.WriteString("PiqLink " + core.AppVersion + ": " + url + "\n")
	// quit when the window has been closed for a while and nothing is running
	go func() {
		for {
			time.Sleep(5 * time.Second)
			g.mu.Lock()
			idle := g.clients == 0 && g.stop == nil && time.Since(g.lastSeen) > 45*time.Second
			g.mu.Unlock()
			if idle && !noBrowser {
				g.mu.Lock()
				g.cleanUpload()
				g.mu.Unlock()
				os.Remove(lockPath)
				os.Exit(0)
			}
		}
	}()
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	return srv.Serve(ln)
}
