package core

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ResumeKeep is how long partial files are kept for a later resume.
const ResumeKeep = 7 * 24 * time.Hour

type resumeEntry struct {
	Final    string `json:"final"`
	Part     string `json:"part"`
	Size     int64  `json:"size"`
	Bytes    int64  `json:"bytes"`
	Complete bool   `json:"complete"`
	Time     int64  `json:"time"`
}

type planned struct {
	final, part, key string
}

// DirStore saves received files under Dir, recreating folders, and keeps a
// resume index (IndexPath; empty disables resuming).
type DirStore struct {
	Dir       string
	IndexPath string

	mu   sync.Mutex
	idx  map[string]*resumeEntry
	plan []planned
}

func (s *DirStore) load() {
	s.idx = map[string]*resumeEntry{}
	if s.IndexPath == "" {
		return
	}
	if b, err := os.ReadFile(s.IndexPath); err == nil {
		json.Unmarshal(b, &s.idx)
	}
	if s.idx == nil {
		s.idx = map[string]*resumeEntry{}
	}
	limit := time.Now().Add(-ResumeKeep).Unix()
	for k, e := range s.idx {
		if e.Time < limit {
			if !e.Complete && e.Part != "" {
				os.Remove(e.Part)
			}
			delete(s.idx, k)
		}
	}
}

func (s *DirStore) save() {
	if s.IndexPath == "" {
		return
	}
	os.MkdirAll(filepath.Dir(s.IndexPath), 0o755)
	b, _ := json.Marshal(s.idx)
	tmp := s.IndexPath + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, s.IndexPath)
	}
}

// uniquePath returns dir/name, adding " (1)", " (2)"... if it is taken.
func uniquePath(dir, name string, reserved map[string]bool) string {
	p := filepath.Join(dir, name)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		_, e1 := os.Stat(p)
		_, e2 := os.Stat(p + ".piqpart")
		if os.IsNotExist(e1) && os.IsNotExist(e2) && !reserved[strings.ToLower(p)] {
			return p
		}
		p = filepath.Join(dir, stemN(stem, i)+ext)
	}
}

func stemN(stem string, i int) string { return stem + " (" + itoa(i) + ")" }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func (s *DirStore) Prepare(peer PeerInfo, files []FileInfo) (map[int]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return nil, err
	}
	s.load()
	s.plan = make([]planned, len(files))
	offsets := map[int]int64{}
	reserved := map[string]bool{}
	for i, f := range files {
		segs := SafeRelPath(f.Name)
		if f.Dir {
			s.plan[i].final = filepath.Join(append([]string{s.Dir}, segs...)...)
			continue
		}
		key := ""
		if peer.ID != "" && f.ID != "" {
			key = peer.ID + ":" + f.ID
		}
		if e := s.idx[key]; key != "" && e != nil && e.Size == f.Size && f.Size > 0 {
			if e.Complete {
				if st, err := os.Stat(e.Final); err == nil && st.Size() == f.Size {
					offsets[i] = f.Size
					s.plan[i] = planned{final: e.Final, key: key}
					continue
				}
			} else if st, err := os.Stat(e.Part); err == nil && st.Size() > 0 && st.Size() <= f.Size {
				offsets[i] = st.Size()
				s.plan[i] = planned{final: e.Final, part: e.Part, key: key}
				reserved[strings.ToLower(e.Final)] = true
				continue
			}
		}
		dir := filepath.Join(append([]string{s.Dir}, segs[:len(segs)-1]...)...)
		final := uniquePath(dir, segs[len(segs)-1], reserved)
		reserved[strings.ToLower(final)] = true
		s.plan[i] = planned{final: final, part: final + ".piqpart", key: key}
	}
	return offsets, nil
}

func (s *DirStore) MakeDir(index int, f FileInfo) error {
	return os.MkdirAll(s.plan[index].final, 0o755)
}

func (s *DirStore) Existing(index int) string { return s.plan[index].final }

func (s *DirStore) Root() string { return s.Dir }

func (s *DirStore) Open(index int, f FileInfo, offset int64) (Target, io.ReadCloser, error) {
	pl := s.plan[index]
	if err := os.MkdirAll(filepath.Dir(pl.final), 0o755); err != nil {
		return nil, nil, err
	}
	var fh *os.File
	var prefix io.ReadCloser
	var err error
	if offset > 0 {
		fh, err = os.OpenFile(pl.part, os.O_WRONLY, 0o644)
		if err == nil {
			err = fh.Truncate(offset)
		}
		if err == nil {
			_, err = fh.Seek(offset, io.SeekStart)
		}
		if err == nil {
			prefix, err = os.Open(pl.part)
		}
	} else {
		fh, err = os.Create(pl.part)
	}
	if err != nil {
		if fh != nil {
			fh.Close()
		}
		return nil, nil, err
	}
	t := &dirTarget{s: s, pl: pl, f: fh, w: bufio.NewWriterSize(fh, 256*1024), written: offset}
	if pl.key != "" {
		s.mu.Lock()
		s.idx[pl.key] = &resumeEntry{Final: pl.final, Part: pl.part, Size: f.Size, Bytes: offset, Time: time.Now().Unix()}
		s.save()
		s.mu.Unlock()
	}
	return t, prefix, nil
}

type dirTarget struct {
	s       *DirStore
	pl      planned
	f       *os.File
	w       *bufio.Writer
	written int64
	done    bool
}

func (t *dirTarget) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	t.written += int64(n)
	return n, err
}

func (t *dirTarget) Commit() (string, error) {
	if err := t.w.Flush(); err != nil {
		t.f.Close()
		return "", err
	}
	if err := t.f.Close(); err != nil {
		return "", err
	}
	t.done = true
	final := t.pl.final
	if _, err := os.Stat(final); err == nil {
		final = uniquePath(filepath.Dir(final), filepath.Base(final), nil)
	}
	if err := os.Rename(t.pl.part, final); err != nil {
		return "", err
	}
	if t.pl.key != "" {
		t.s.mu.Lock()
		if e := t.s.idx[t.pl.key]; e != nil {
			e.Final, e.Complete, e.Bytes, e.Time = final, true, e.Size, time.Now().Unix()
		}
		t.s.save()
		t.s.mu.Unlock()
	}
	return final, nil
}

func (t *dirTarget) Interrupt(written int64) {
	if t.done {
		return
	}
	t.done = true
	t.w.Flush()
	t.f.Close()
	st, err := os.Stat(t.pl.part)
	if t.pl.key == "" || err != nil || st.Size() == 0 {
		os.Remove(t.pl.part)
		t.forget()
		return
	}
	t.s.mu.Lock()
	if e := t.s.idx[t.pl.key]; e != nil {
		e.Bytes, e.Time = st.Size(), time.Now().Unix()
	}
	t.s.save()
	t.s.mu.Unlock()
}

func (t *dirTarget) Discard() {
	if t.done {
		return
	}
	t.done = true
	t.f.Close()
	os.Remove(t.pl.part)
	t.forget()
}

func (t *dirTarget) forget() {
	if t.pl.key == "" {
		return
	}
	t.s.mu.Lock()
	delete(t.s.idx, t.pl.key)
	t.s.save()
	t.s.mu.Unlock()
}
