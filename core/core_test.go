package core

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodeRoundTrip(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 2000; i++ {
		c, err := NewLANCode(net.IPv4(192, 168, 1, byte(i)), 24, i%4)
		if err != nil {
			t.Fatal(err)
		}
		s := c.String()
		if len(s) != 6 {
			t.Fatalf("len %d", len(s))
		}
		seen[s] = true
		d, err := Parse(FormatDisplay(s))
		if err != nil || d != c {
			t.Fatalf("roundtrip %v != %v (%s)", d, c, s)
		}
	}
	if len(seen) < 1990 {
		t.Fatalf("too many duplicates: %d", len(seen))
	}
	c16, _ := NewLANCode(net.IPv4(10, 0, 37, 200), 16, 3)
	if d, _ := Parse(c16.Display()); d != c16 || len(c16.String()) != 8 {
		t.Fatal("lan16")
	}
	cn, _ := NewNETCode(net.IPv4(85, 4, 200, 17), 11)
	if d, _ := Parse(cn.Display()); d != cn || len(cn.String()) != 12 {
		t.Fatal("net")
	}
	// look-alike characters and QR payload
	s := c16.String()
	if d, _ := Parse("piqlink:" + s); d != c16 {
		t.Fatal("qr payload")
	}
	t.Logf("esempi: %s  %s  %s", func() string { c, _ := NewLANCode(net.IPv4(192, 168, 1, 23), 24, 0); return c.Display() }(), c16.Display(), cn.Display())
}

func writeRandom(t *testing.T, p string, n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return b
}

func loopback() {
	TestLocalAddrs = []LocalAddr{{IP: net.IPv4(127, 0, 0, 1).To4(), MaskOnes: 24}}
}

type recvOut struct {
	saved []string
	err   error
}

func startRecv(t *testing.T, st Store, ev Events) (*Receiver, chan recvOut) {
	r, err := StartReceiver(ModeLAN)
	if err != nil {
		t.Fatal(err)
	}
	rc := make(chan recvOut, 1)
	go func() {
		_, saved, err := r.Wait(context.Background(), PeerInfo{Name: "RICEVENTE", ID: "recv-1"}, st, ev, nil)
		rc <- recvOut{saved, err}
	}()
	return r, rc
}

func collect(t *testing.T, paths ...string) []FileInfo {
	var out []FileInfo
	for _, p := range paths {
		it, err := CollectItem(p)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, it.Entries...)
	}
	return out
}

func TestTransferFilesAndFolders(t *testing.T) {
	loopback()
	src, dst := t.TempDir(), t.TempDir()
	big := writeRandom(t, filepath.Join(src, "video.mp4"), 20<<20+123)
	small := writeRandom(t, filepath.Join(src, "foto.jpg"), 1000)
	os.MkdirAll(filepath.Join(src, "Cartella", "sub", "vuota"), 0o755)
	deep := writeRandom(t, filepath.Join(src, "Cartella", "sub", "dati.bin"), 300000)
	os.WriteFile(filepath.Join(src, "Cartella", "nota.txt"), []byte("ciao"), 0o644)
	os.WriteFile(filepath.Join(dst, "foto.jpg"), []byte("esistente"), 0o644)

	st := &DirStore{Dir: dst, IndexPath: filepath.Join(t.TempDir(), "resume.json")}
	r, rc := startRecv(t, st, Events{})
	files := collect(t, filepath.Join(src, "video.mp4"), filepath.Join(src, "foto.jpg"), filepath.Join(src, "Cartella"))
	var last Progress
	_, err := SendTo(context.Background(), r.Code.Display(), PeerInfo{Name: "MITTENTE", ID: "send-1"}, files, Events{Progress: func(p Progress) { last = p }})
	if err != nil {
		t.Fatal("send:", err)
	}
	o := <-rc
	if o.err != nil {
		t.Fatal("recv:", o.err)
	}
	check := func(rel string, want []byte) {
		got, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s differs (%v)", rel, err)
		}
	}
	check("video.mp4", big)
	check("foto (1).jpg", small)
	check(filepath.Join("Cartella", "sub", "dati.bin"), deep)
	check(filepath.Join("Cartella", "nota.txt"), []byte("ciao"))
	if st, err := os.Stat(filepath.Join(dst, "Cartella", "sub", "vuota")); err != nil || !st.IsDir() {
		t.Fatal("empty folder missing")
	}
	if last.FileCount != 4 || last.Done != last.Total {
		t.Fatalf("progress %+v", last)
	}
	t.Logf("%d file salvati, %d byte", len(o.saved), last.Total)
}

func TestRejectAndTimeoutAnswer(t *testing.T) {
	loopback()
	src := t.TempDir()
	writeRandom(t, filepath.Join(src, "a.bin"), 5000)
	st := &DirStore{Dir: t.TempDir()}
	var offered Offer
	r, rc := startRecv(t, st, Events{Confirm: func(p PeerInfo, o Offer) <-chan bool {
		offered = o
		ch := make(chan bool, 1)
		ch <- false
		return ch
	}})
	waiting := false
	_, err := SendTo(context.Background(), r.Code.String(), PeerInfo{Name: "M"}, collect(t, filepath.Join(src, "a.bin")), Events{Waiting: func() { waiting = true }})
	if !errors.Is(err, ErrPeerRejects) || !waiting {
		t.Fatalf("sender: %v waiting=%v", err, waiting)
	}
	if o := <-rc; !errors.Is(o.err, ErrRejected) || len(offered.Files) != 1 {
		t.Fatalf("receiver: %v", o.err)
	}
	// accepted
	r, rc = startRecv(t, st, Events{Confirm: func(p PeerInfo, o Offer) <-chan bool {
		ch := make(chan bool, 1)
		go func() { time.Sleep(200 * time.Millisecond); ch <- true }()
		return ch
	}})
	if _, err := SendTo(context.Background(), r.Code.String(), PeerInfo{Name: "M"}, collect(t, filepath.Join(src, "a.bin")), Events{}); err != nil {
		t.Fatal(err)
	}
	if o := <-rc; o.err != nil {
		t.Fatal(o.err)
	}
}

func TestResumeInterrupted(t *testing.T) {
	loopback()
	src, dst := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(src, "Album"), 0o755)
	first := writeRandom(t, filepath.Join(src, "Album", "01.jpg"), 2<<20)
	big := writeRandom(t, filepath.Join(src, "Album", "film.mkv"), 40<<20)
	index := filepath.Join(t.TempDir(), "resume.json")
	me := PeerInfo{Name: "MITTENTE", ID: "send-xyz"}
	files := collect(t, filepath.Join(src, "Album"))

	// 1st attempt: cancel the sender at ~50%
	testSendDelay = 3 * time.Millisecond
	defer func() { testSendDelay = 0 }()
	st := &DirStore{Dir: dst, IndexPath: index}
	r, rc := startRecv(t, st, Events{})
	ctx, cancel := context.WithCancel(context.Background())
	_, err := SendTo(ctx, r.Code.String(), me, files, Events{Progress: func(p Progress) {
		if p.Done > p.Total/2 {
			cancel()
		}
	}})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("expected cancel, got %v", err)
	}
	recvErr := (<-rc).err
	part, _ := filepath.Glob(filepath.Join(dst, "Album", "*.piqpart"))
	if len(part) != 1 {
		all, _ := filepath.Glob(filepath.Join(dst, "*", "*"))
		idx, _ := os.ReadFile(index)
		t.Logf("files: %v\nindex: %s\nrecv err: %v", all, idx, recvErr)
		t.Fatalf("partial not kept: %v", part)
	}
	pst, _ := os.Stat(part[0])
	t.Logf("parziale: %d byte", pst.Size())

	// 2nd attempt with a new code: resumes
	testSendDelay = 0
	st2 := &DirStore{Dir: dst, IndexPath: index}
	r, rc = startRecv(t, st2, Events{})
	var last Progress
	if _, err := SendTo(context.Background(), r.Code.String(), me, files, Events{Progress: func(p Progress) { last = p }}); err != nil {
		t.Fatal(err)
	}
	if o := <-rc; o.err != nil {
		t.Fatal(o.err)
	}
	if last.Resumed < pst.Size()+int64(len(first)) {
		t.Fatalf("resume not used: resumed=%d", last.Resumed)
	}
	got, _ := os.ReadFile(filepath.Join(dst, "Album", "film.mkv"))
	if !bytes.Equal(got, big) {
		t.Fatal("resumed file differs")
	}
	got, _ = os.ReadFile(filepath.Join(dst, "Album", "01.jpg"))
	if !bytes.Equal(got, first) {
		t.Fatal("first file differs")
	}
	if dup, _ := filepath.Glob(filepath.Join(dst, "Album*", "* (1)*")); len(dup) > 0 {
		t.Fatalf("duplicates created: %v", dup)
	}
	if left, _ := filepath.Glob(filepath.Join(dst, "Album", "*.piqpart")); len(left) > 0 {
		t.Fatal("partial left behind")
	}
	t.Logf("ripresi %d byte su %d", last.Resumed, last.Total)
}

func TestWrongCodeBlocks(t *testing.T) {
	TestLocalAddrs = []LocalAddr{{IP: net.IPv4(127, 0, 0, 1).To4(), MaskOnes: 24}}
	r, err := StartReceiver(ModeLAN)
	if err != nil {
		t.Fatal(err)
	}
	rc := make(chan error, 1)
	go func() {
		_, _, err := r.Wait(context.Background(), PeerInfo{}, &DirStore{Dir: t.TempDir()}, Events{}, nil)
		rc <- err
	}()
	// same address and port, different secret
	bad := r.Code
	bad.Secret ^= 0x5A5A5
	for i := 0; i < MaxAttempts; i++ {
		_, err := SendTo(context.Background(), bad.String(), PeerInfo{}, nil, Events{})
		if err == nil || err.Error() != "codice errato" {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if err := <-rc; !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("expected block, got %v", err)
	}
	// now even the right code must fail
	if _, err := SendTo(context.Background(), r.Code.String(), PeerInfo{}, nil, Events{}); err == nil {
		t.Fatal("blocked code still accepted")
	}
}

func TestCancelReceiver(t *testing.T) {
	TestLocalAddrs = []LocalAddr{{IP: net.IPv4(127, 0, 0, 1).To4(), MaskOnes: 24}}
	r, err := StartReceiver(ModeLAN)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	rc := make(chan error, 1)
	go func() { _, _, err := r.Wait(ctx, PeerInfo{}, &DirStore{Dir: t.TempDir()}, Events{}, nil); rc <- err }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	if err := <-rc; !errors.Is(err, ErrCancelled) {
		t.Fatalf("got %v", err)
	}
}

func TestFormatTyping(t *testing.T) {
	cases := map[string]string{"k7q": "K7Q", "k7qm": "K7Q-M", "K7QM4X": "K7Q-M4X", "K7QM4XR": "K7QM-4XR", "K7QM4XRT9P": "K7QM-4XRT-9P", "K7QM4XRT9PWA77": "K7QM-4XRT-9PWA-77", "K7QM4XRT9PWA77ZZ": "K7QM-4XRT-9PWA-77", "k7o-ml": "K70-M1"}
	for in, want := range cases {
		if got := FormatTyping(Normalize(in)); got != want {
			t.Errorf("%s: %s != %s", in, got, want)
		}
	}
}

func TestReversedFlow(t *testing.T) {
	loopback()
	src, dst := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(src, "Foto"), 0o755)
	big := writeRandom(t, filepath.Join(src, "Foto", "a.jpg"), 3<<20)
	small := writeRandom(t, filepath.Join(src, "doc.pdf"), 1234)
	files := collect(t, filepath.Join(src, "Foto"), filepath.Join(src, "doc.pdf"))

	// the SENDER shows the code and waits
	r, err := StartReceiver(ModeLAN)
	if err != nil {
		t.Fatal(err)
	}
	sc := make(chan error, 1)
	go func() {
		_, err := r.WaitSend(context.Background(), PeerInfo{Name: "PC", ID: "pc-1"}, files, Events{}, nil)
		sc <- err
	}()
	// the RECEIVER types the code
	st := &DirStore{Dir: dst, IndexPath: filepath.Join(t.TempDir(), "r.json")}
	peer, saved, err := ReceiveFrom(context.Background(), r.Code.Display(), PeerInfo{Name: "Telefono", ID: "tel-1"}, st, Events{})
	if err != nil {
		t.Fatal("receive:", err)
	}
	if err := <-sc; err != nil {
		t.Fatal("send:", err)
	}
	if peer.Name != "PC" || len(saved) != 2 {
		t.Fatalf("peer %q saved %v", peer.Name, saved)
	}
	got, _ := os.ReadFile(filepath.Join(dst, "Foto", "a.jpg"))
	got2, _ := os.ReadFile(filepath.Join(dst, "doc.pdf"))
	if !bytes.Equal(got, big) || !bytes.Equal(got2, small) {
		t.Fatal("content differs")
	}
}

func TestSameRoleRejected(t *testing.T) {
	loopback()
	src := t.TempDir()
	writeRandom(t, filepath.Join(src, "x.bin"), 100)
	files := collect(t, filepath.Join(src, "x.bin"))
	r, err := StartReceiver(ModeLAN)
	if err != nil {
		t.Fatal(err)
	}
	sc := make(chan error, 1)
	go func() {
		_, err := r.WaitSend(context.Background(), PeerInfo{Name: "A"}, files, Events{}, nil)
		sc <- err
	}()
	_, err = SendTo(context.Background(), r.Code.String(), PeerInfo{Name: "B"}, files, Events{})
	e2 := <-sc
	if err == nil || e2 == nil {
		t.Fatalf("expected errors, got %v / %v", err, e2)
	}
	if !errors.Is(err, ErrSameRole) && !errors.Is(e2, ErrSameRole) {
		t.Fatalf("expected ErrSameRole: %v / %v", err, e2)
	}
	t.Logf("messaggi: %v | %v", err, e2)
}

// slowStore simulates a slow disk on the receiver.
type slowStore struct{ *DirStore }

type slowTarget struct{ Target }

func (s slowStore) Open(i int, f FileInfo, off int64) (Target, io.ReadCloser, error) {
	t, r, err := s.DirStore.Open(i, f, off)
	if err != nil {
		return nil, nil, err
	}
	return slowTarget{t}, r, nil
}

func (s slowTarget) Write(b []byte) (int, error) {
	time.Sleep(4 * time.Millisecond)
	return s.Target.Write(b)
}

// TestAckedProgress: the sender shows what the receiver stored, never more.
func TestAckedProgress(t *testing.T) {
	loopback()
	src := t.TempDir()
	writeRandom(t, filepath.Join(src, "a.bin"), 12<<20)
	writeRandom(t, filepath.Join(src, "b.bin"), 6<<20)
	var recvDone atomic.Int64
	st := slowStore{&DirStore{Dir: t.TempDir()}}
	r, err := StartReceiver(ModeLAN)
	if err != nil {
		t.Fatal(err)
	}
	rc := make(chan error, 1)
	go func() {
		_, _, err := r.Wait(context.Background(), PeerInfo{Name: "R", Ver: AppVersion}, st,
			Events{Progress: func(p Progress) { recvDone.Store(p.Done) }}, nil)
		rc <- err
	}()
	var ahead, last int64
	var lastFile int
	_, err = SendTo(context.Background(), r.Code.String(), PeerInfo{Name: "S", Ver: AppVersion}, collect(t, filepath.Join(src, "a.bin"), filepath.Join(src, "b.bin")),
		Events{Progress: func(p Progress) {
			if d := p.Done - recvDone.Load(); d > ahead {
				ahead = d
			}
			last, lastFile = p.Done, p.FileIndex
		}})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-rc; err != nil {
		t.Fatal(err)
	}
	if ahead > 0 {
		t.Fatalf("sender showed %d bytes more than the receiver had", ahead)
	}
	if last != 18<<20 || lastFile != 1 {
		t.Fatalf("final progress %d file %d", last, lastFile)
	}
}
