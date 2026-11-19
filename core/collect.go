package core

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

// FileID identifies a file on the sender for resuming (path, size, mtime).
func FileID(path string, size int64, mtimeUnix int64) string {
	h := sha256.Sum256([]byte(path + "\x00" + strconv.FormatInt(size, 10) + "\x00" + strconv.FormatInt(mtimeUnix, 10)))
	return hex.EncodeToString(h[:16])
}

// Item is something the user selected: a file or a whole folder.
type Item struct {
	Path    string
	Name    string
	IsDir   bool
	Entries []FileInfo // files (and folders) to send for this item
	Size    int64
	Files   int
}

// CollectItem expands a selected path (file or folder) into offer entries.
func CollectItem(path string) (*Item, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	it := &Item{Path: abs, Name: filepath.Base(abs)}
	if !st.IsDir() {
		it.Entries = []FileInfo{{Name: it.Name, Size: st.Size(), Path: abs, ID: FileID(abs, st.Size(), st.ModTime().Unix())}}
		it.Size, it.Files = st.Size(), 1
		return it, nil
	}
	it.IsDir = true
	root := it.Name
	filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() && p != abs {
				return fs.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(abs, p)
		name := root
		if rel != "." {
			name = root + "/" + filepath.ToSlash(rel)
		}
		if d.IsDir() {
			it.Entries = append(it.Entries, FileInfo{Name: name, Dir: true})
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // links, devices...
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		it.Entries = append(it.Entries, FileInfo{Name: name, Size: info.Size(), Path: p, ID: FileID(p, info.Size(), info.ModTime().Unix())})
		it.Size += info.Size()
		it.Files++
		return nil
	})
	return it, nil
}
