package main

// Terminal mode: piqlink invia|ricevi (send|receive).

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"piqlink/core"
)

func tr(it, en string) string { return core.Tr(it, en) }

// printQR draws the QR with half blocks (two rows per line), white border.
func printQR(payload string) {
	q, err := core.EncodeQR(payload)
	if err != nil {
		return
	}
	n := q.Size
	dark := func(x, y int) bool { return x >= 0 && y >= 0 && x < n && y < n && q.Modules[y][x] }
	const border = 2
	for y := -border; y < n+border; y += 2 {
		var b strings.Builder
		b.WriteString("  \x1b[47;30m") // white background, black blocks
		for x := -border; x < n+border; x++ {
			top, bot := dark(x, y), dark(x, y+1)
			switch {
			case top && bot:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bot:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\x1b[0m\n")
		os.Stdout.WriteString(b.String())
	}
}

func human(n int64) string {
	u := []string{"B", "KB", "MB", "GB", "TB"}
	f := float64(n)
	i := 0
	for f >= 1000 && i < len(u)-1 {
		f /= 1000
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", f, u[i])
}

type cliProgress struct {
	start time.Time
	last  time.Time
	peer  string
	tty   bool
}

func (c *cliProgress) show(p core.Progress) {
	now := time.Now()
	if c.start.IsZero() {
		c.start = now
	}
	if now.Sub(c.last) < 200*time.Millisecond && p.Done < p.Total {
		return
	}
	c.last = now
	pct := 100.0
	if p.Total > 0 {
		pct = float64(p.Done) * 100 / float64(p.Total)
	}
	rate := ""
	if s := now.Sub(c.start).Seconds(); s > 0.5 {
		rate = human(int64(float64(p.Done)/s)) + "/s"
	}
	const w = 28
	fill := int(pct / 100 * w)
	bar := strings.Repeat("█", fill) + strings.Repeat("░", w-fill)
	line := fmt.Sprintf("  %s %5.1f%%  %s / %s  %s", bar, pct, human(p.Done), human(p.Total), rate)
	if c.tty {
		fmt.Printf("\r%-90s", line)
	} else if p.Done == p.Total {
		fmt.Println(line)
	}
}

func isTTY() bool {
	st, err := os.Stdout.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func cliEvents(pr *cliProgress, confirm bool) core.Events {
	ev := core.Events{
		Peer: func(p core.PeerInfo) {
			pr.peer = p.Name
			fmt.Printf("  %s %s\n", tr("Collegato con", "Connected to"), p.Name)
		},
		Progress: pr.show,
		Waiting: func() {
			fmt.Println("  " + tr("In attesa che l'altro dispositivo accetti…", "Waiting for the other device to accept…"))
		},
		Reply: func(code string, nat core.NATInfo) {
			fmt.Println("\n  " + tr("Collegamento assistito. Detta questo codice di risposta a chi ti ha dato il codice:", "Assisted connection. Dictate this reply code to whoever gave you the code:"))
			fmt.Printf("\n      \x1b[1m%s\x1b[0m\n\n", code)
			fmt.Println("  " + tr("(lo scrive nel riquadro «Codice di risposta»)", "(they type it in the “Reply code” box)"))
		},
	}
	if confirm {
		ev.Confirm = func(p core.PeerInfo, o core.Offer) <-chan bool {
			ch := make(chan bool, 1)
			n := 0
			for _, f := range o.Files {
				if !f.Dir {
					n++
				}
			}
			fmt.Printf("\n  %s %s: %d %s, %s\n", p.Name, tr("vuole inviarti", "wants to send you"), n, tr("file", "files"), human(o.TotalSize()))
			for i, f := range o.Files {
				if i == 8 {
					fmt.Printf("    … (+%d)\n", len(o.Files)-8)
					break
				}
				if !f.Dir {
					fmt.Printf("    %s  %s\n", f.Name, human(f.Size))
				}
			}
			fmt.Printf("  %s [%s/n] ", tr("Accetti?", "Accept?"), tr("S", "Y"))
			go func() {
				l, _ := bufio.NewReader(os.Stdin).ReadString('\n')
				l = strings.ToLower(strings.TrimSpace(l))
				ch <- l == "" || l == "s" || l == "si" || l == "sì" || l == "y" || l == "yes"
			}()
			return ch
		}
	}
	return ev
}

// readReplies: on an assisted code, the owner types the other's reply code.
func readReplies(r *core.Receiver) {
	if r.Mode != core.ModeP2P {
		return
	}
	fmt.Println("  " + tr("Collegamento assistito: quando l'altro ti detta il codice di risposta (10 caratteri), scrivilo qui e premi Invio.", "Assisted connection: when the other person dictates the reply code (10 characters), type it here and press Enter."))
	go func() {
		in := bufio.NewScanner(os.Stdin)
		for {
			fmt.Print("  " + tr("Codice di risposta: ", "Reply code: "))
			if !in.Scan() {
				return
			}
			if err := r.SetReply(in.Text()); err != nil {
				fmt.Println("  " + tr("Codice di risposta non valido: controllalo (10 caratteri).", "Invalid reply code: check it (10 characters)."))
				continue
			}
			fmt.Println("  " + tr("Collegamento in corso…", "Connecting…"))
			return
		}
	}()
}

// cliConsent asks (once) to allow the assisted connection.
func cliConsent(set *Settings, given bool) bool {
	if core.STUNAllowed() {
		return true
	}
	if !given {
		fmt.Println("\n  " + tr("Collegamento assistito: PiqLink chiede a un server pubblico STUN di Google o Cloudflare come i due dispositivi si vedono da Internet. Il server riceve solo l'indirizzo IP: nessun file, nessun nome.", "Assisted connection: PiqLink asks a public STUN server from Google or Cloudflare how the two devices are seen from the Internet. The server only receives the IP address: no files, no names."))
		fmt.Printf("  %s [%s/n] ", tr("Consenti?", "Allow?"), tr("s", "y"))
		l, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		l = strings.ToLower(strings.TrimSpace(l))
		if l != "s" && l != "si" && l != "sì" && l != "y" && l != "yes" {
			return false
		}
	}
	set.Assisted = true
	set.save()
	core.SetSTUNAllowed(true)
	return true
}

func showCode(r *core.Receiver, sending bool) {
	fmt.Println()
	if sending {
		fmt.Println("  " + tr("Dai questo codice a chi riceve (RICEVI › «Ho il codice del mittente»):", "Give this code to the receiver (RECEIVE › “I have the sender's code”):"))
	} else {
		fmt.Println("  " + tr("Dai questo codice a chi invia (INVIA › scrivi il codice):", "Give this code to the sender (SEND › type the code):"))
	}
	fmt.Printf("\n      \x1b[1m%s\x1b[0m\n\n", r.Code.Display())
	printQR(r.Code.QRPayload())
	fmt.Printf("\n  %s %s\n", tr("Valido fino alle", "Valid until"), r.Expires.Format("15:04"))
	fmt.Println("  " + tr("In attesa… (Ctrl+C per annullare)", "Waiting… (Ctrl+C to cancel)"))
	readReplies(r)
}

func cliContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		fmt.Println("\n  " + tr("Annullato.", "Cancelled."))
		cancel()
		time.Sleep(300 * time.Millisecond)
		os.Exit(130)
	}()
	return ctx, cancel
}

type cliOpts struct {
	code     string
	internet bool
	dir      string
	yes      bool
	assisted bool
	files    []string
}

func parseCLI(args []string) (cliOpts, error) {
	var o cliOpts
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s: %s", a, tr("manca il valore", "missing value"))
			}
			i++
			return args[i], nil
		}
		var err error
		switch a {
		case "-c", "--codice", "--code":
			o.code, err = next()
		case "-d", "--cartella", "--dir":
			o.dir, err = next()
		case "-i", "--internet":
			o.internet = true
		case "-s", "--si", "--yes", "-y":
			o.yes = true
		case "-a", "--assistito", "--assisted":
			o.assisted = true
		default:
			if strings.HasPrefix(a, "-") && a != "-" {
				return o, fmt.Errorf("%s: %s", tr("opzione sconosciuta", "unknown option"), a)
			}
			o.files = append(o.files, a)
		}
		if err != nil {
			return o, err
		}
	}
	return o, nil
}

func cliSend(set *Settings, args []string) int {
	o, err := parseCLI(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if len(o.files) == 0 {
		fmt.Fprintln(os.Stderr, tr("Indica i file o le cartelle da inviare.", "Give the files or folders to send."))
		return 2
	}
	var files []core.FileInfo
	var total int64
	for _, p := range o.files {
		it, err := core.CollectItem(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", p, err)
			return 1
		}
		files = append(files, it.Entries...)
		total += it.Size
	}
	me := core.PeerInfo{Name: set.deviceName(), OS: "linux", Ver: core.AppVersion, ID: set.DeviceID}
	ctx, cancel := cliContext()
	defer cancel()
	pr := &cliProgress{tty: isTTY()}
	ev := cliEvents(pr, false)
	fmt.Printf("  PiqLink %s · %s %s\n", core.AppVersion, tr("invio di", "sending"), human(total))
	var peer core.PeerInfo
	if o.code != "" {
		if err := checkCode(set, o); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		fmt.Println("  " + tr("Collegamento…", "Connecting…"))
		peer, err = core.SendTo(ctx, o.code, me, files, ev)
	} else {
		var r *core.Receiver
		r, err = startRecv(set, o)
		if err == nil {
			showCode(r, true)
			peer, err = r.WaitSend(ctx, me, files, ev, nil)
		}
	}
	if pr.tty && !pr.start.IsZero() {
		fmt.Println()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "  "+explain(err, true, o.code == ""))
		return 1
	}
	fmt.Printf("  %s %s.\n", tr("Inviato a", "Sent to"), peer.Name)
	return 0
}

func cliReceive(set *Settings, args []string) int {
	o, err := parseCLI(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if len(o.files) > 0 && o.code == "" {
		o.code = o.files[0] // "piqlink ricevi ABC-123"
	}
	dir := set.saveDir()
	if o.dir != "" {
		dir = o.dir
	}
	os.MkdirAll(dir, 0o755)
	index := configDir() + "/resume.json"
	if o.dir != "" { // another folder: its own resume list
		h := sha256.Sum256([]byte(dir))
		index = configDir() + "/resume-" + hex.EncodeToString(h[:6]) + ".json"
	}
	store := &core.DirStore{Dir: dir, IndexPath: index}
	me := core.PeerInfo{Name: set.deviceName(), OS: "linux", Ver: core.AppVersion, ID: set.DeviceID}
	ctx, cancel := cliContext()
	defer cancel()
	pr := &cliProgress{tty: isTTY()}
	ev := cliEvents(pr, set.Confirm && !o.yes)
	fmt.Printf("  PiqLink %s · %s %s\n", core.AppVersion, tr("i file arrivano in", "files go to"), dir)
	var peer core.PeerInfo
	var saved []string
	if o.code != "" {
		if err := checkCode(set, o); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		fmt.Println("  " + tr("Collegamento…", "Connecting…"))
		peer, saved, err = core.ReceiveFrom(ctx, o.code, me, store, ev)
	} else {
		var r *core.Receiver
		r, err = startRecv(set, o)
		if err == nil {
			showCode(r, false)
			peer, saved, err = r.Wait(ctx, me, store, ev, nil)
		}
	}
	if pr.tty && !pr.start.IsZero() {
		fmt.Println()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "  "+explain(err, false, o.code == ""))
		return 1
	}
	fmt.Printf("  %s %d %s %s:\n", tr("Ricevuti", "Received"), len(saved), tr("file da", "files from"), peer.Name)
	for i, s := range saved {
		if i == 20 {
			fmt.Printf("    … (+%d)\n", len(saved)-20)
			break
		}
		fmt.Println("    " + s)
	}
	return 0
}

func startRecv(set *Settings, o cliOpts) (*core.Receiver, error) {
	m := core.ModeLAN
	if o.internet {
		m = core.ModeNET
	}
	r, err := core.StartReceiver(m)
	var na *core.ErrNeedAssisted
	if errors.As(err, &na) {
		fmt.Println("  " + friendly(errors.New(na.Reason)))
		if !cliConsent(set, o.assisted) {
			return nil, errors.New(tr("collegamento assistito non consentito", "assisted connection not allowed"))
		}
		fmt.Println("  " + tr("Collegamento assistito: ricerca dell'indirizzo pubblico…", "Assisted connection: finding the public address…"))
		return core.StartReceiverP2P(context.Background())
	}
	return r, err
}

func checkCode(set *Settings, o cliOpts) error {
	c, err := core.Parse(o.code)
	if err != nil {
		if len(core.Normalize(o.code)) == 10 {
			return errors.New(tr("Questo è un codice di risposta (10 caratteri): va scritto sul dispositivo che mostra il codice lungo.", "This is a reply code (10 characters): it goes on the device showing the long code."))
		}
		return errors.New(tr("Codice non valido: controlla i caratteri.", "Invalid code: check the characters."))
	}
	if c.Kind == core.KindP2P && !cliConsent(set, o.assisted) {
		return errors.New(tr("Questo codice richiede il collegamento assistito.", "This code needs the assisted connection."))
	}
	return nil
}
