package p2p

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type storage struct {
	mu             sync.Mutex
	meta           TorrentFile
	root, dir      string
	selected       map[int]bool
	verified, busy []bool
	bytes          int64
	manifest       ownership
}
type ownership struct {
	Hash   string          `json:"hash"`
	Paths  map[string]bool `json:"paths"`
	Pieces []bool          `json:"pieces"`
}

func atomicJSON(path string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".checkpoint-*")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(tmp, path)
}
func noSymlink(path string) error {
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		s, e := os.Lstat(p)
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if e == nil && s.Mode()&os.ModeSymlink != 0 {
			return errors.New("destination contains a symbolic link")
		}
		parent := filepath.Dir(p)
		if parent == p {
			break
		}
	}
	return nil
}
func openStorage(t TorrentFile, root string, selected []int) (*storage, error) {
	root, e := filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	if e = noSymlink(root); e != nil {
		return nil, e
	}
	stat, e := os.Stat(root)
	if e != nil || !stat.IsDir() {
		return nil, errors.New("choose an existing destination folder")
	}
	s := &storage{meta: t, root: root, dir: filepath.Join(root, ".bitgorrent", t.ID()), selected: map[int]bool{}, verified: make([]bool, len(t.PieceHashes)), busy: make([]bool, len(t.PieceHashes))}
	if len(selected) == 0 {
		return nil, errors.New("select at least one file")
	}
	for _, i := range selected {
		if i < 0 || i >= len(t.Files) {
			return nil, errors.New("invalid file selection")
		}
		s.selected[i] = true
	}
	if e = noSymlink(s.dir); e != nil {
		return nil, e
	}
	s.manifest = ownership{Hash: t.ID(), Paths: map[string]bool{}}
	checkpoint := filepath.Join(s.dir, "state.json")
	if e = noSymlink(checkpoint); e != nil {
		return nil, e
	}
	if b, e := os.ReadFile(checkpoint); e == nil {
		if len(b) > maxMetadata || json.Unmarshal(b, &s.manifest) != nil || s.manifest.Hash != t.ID() || s.manifest.Paths == nil {
			return nil, errors.New("invalid resume checkpoint; destination left untouched")
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	for i := range s.selected {
		p := filepath.Join(root, t.Files[i].Path)
		if e = noSymlink(p); e != nil {
			return nil, e
		}
		if _, e = os.Stat(p); e == nil && !s.manifest.Paths[t.Files[i].Path] {
			return nil, fmt.Errorf("destination already exists: %s; choose another folder", t.Files[i].Path)
		} else if e != nil && !os.IsNotExist(e) {
			return nil, e
		}
	}
	if e = os.MkdirAll(s.dir, 0700); e != nil {
		return nil, e
	}
	if e = noSymlink(filepath.Join(s.dir, "boundary.part")); e != nil {
		return nil, e
	}
	for i := range s.selected {
		file := t.Files[i]
		p := filepath.Join(root, file.Path)
		if e = os.MkdirAll(filepath.Dir(p), 0755); e != nil {
			return nil, e
		}
		flags := os.O_CREATE | os.O_RDWR
		if !s.manifest.Paths[file.Path] {
			flags |= os.O_EXCL
		}
		f, e := os.OpenFile(p, flags, 0644)
		if e != nil {
			return nil, e
		}
		if e = f.Close(); e != nil {
			return nil, e
		}
		s.manifest.Paths[file.Path] = true
		if e = atomicJSON(checkpoint, s.manifest); e != nil {
			return nil, e
		}
	}
	return s, nil
}
func (s *storage) close() {}
func (s *storage) pieceSize(i int) int {
	return int(min(s.meta.PieceLength, s.meta.Length-int64(i)*s.meta.PieceLength))
}
func (s *storage) wanted(i int) bool {
	a := int64(i) * s.meta.PieceLength
	b := a + int64(s.pieceSize(i))
	for j := range s.selected {
		f := s.meta.Files[j]
		if f.Offset < b && f.Offset+f.Length > a {
			return true
		}
	}
	return false
}
func (s *storage) selectedBytes(i int) int64 {
	a := int64(i) * s.meta.PieceLength
	b := a + int64(s.pieceSize(i))
	var n int64
	for j := range s.selected {
		f := s.meta.Files[j]
		n += max(int64(0), min(b, f.Offset+f.Length)-max(a, f.Offset))
	}
	return n
}
func (s *storage) total() int64 {
	var n int64
	for j := range s.selected {
		n += s.meta.Files[j].Length
	}
	return n
}
func (s *storage) completed() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for i, v := range s.verified {
		if v {
			n += s.selectedBytes(i)
		}
	}
	return n
}
func (s *storage) allCompleted() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for i, v := range s.verified {
		if v {
			n += int64(s.pieceSize(i))
		}
	}
	return n
}
func (s *storage) received() int64 { s.mu.Lock(); defer s.mu.Unlock(); return s.bytes }
func (s *storage) complete() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, v := range s.verified {
		if s.wanted(i) && !v {
			return false
		}
	}
	return true
}
func (s *storage) claim(bf Bitfield) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, v := range s.verified {
		if !v && !s.busy[i] && s.wanted(i) && bf.HasPiece(i) {
			s.busy[i] = true
			return i
		}
	}
	return -1
}
func (s *storage) release(i int) { s.mu.Lock(); defer s.mu.Unlock(); s.busy[i] = false }
func (s *storage) checkpointLocked() error {
	s.manifest.Pieces = append([]bool(nil), s.verified...)
	return atomicJSON(filepath.Join(s.dir, "state.json"), s.manifest)
}
func (s *storage) checkpoint() error { s.mu.Lock(); defer s.mu.Unlock(); return s.checkpointLocked() }

// Sparse boundary storage retains skipped-file bytes needed to verify full pieces.
func (s *storage) access(index int, buf []byte, write bool) error {
	start := int64(index) * s.meta.PieceLength
	end := start + int64(len(buf))
	for i, file := range s.meta.Files {
		a, b := max(start, file.Offset), min(end, file.Offset+file.Length)
		if a >= b {
			continue
		}
		path := filepath.Join(s.root, file.Path)
		off := a - file.Offset
		if !s.selected[i] {
			path = filepath.Join(s.dir, "boundary.part")
			off = a
		}
		if e := noSymlink(path); e != nil {
			return e
		}
		flags := os.O_RDONLY
		if write {
			flags = os.O_RDWR | os.O_CREATE
		}
		f, e := os.OpenFile(path, flags, 0644)
		if e != nil {
			return e
		}
		part := buf[a-start : b-start]
		if write {
			var n int
			n, e = f.WriteAt(part, off)
			if e == nil && n != len(part) {
				e = io.ErrShortWrite
			}
			if e == nil {
				e = f.Sync()
			}
		} else {
			_, e = f.ReadAt(part, off)
		}
		ce := f.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
	}
	return nil
}
func (s *storage) commit(i int, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(b) != s.pieceSize(i) || sha1.Sum(b) != s.meta.PieceHashes[i] {
		return errors.New("piece failed verification")
	}
	if e := s.access(i, b, true); e != nil {
		return e
	}
	s.verified[i] = true
	s.bytes += int64(len(b))
	return s.checkpointLocked()
}
func (s *storage) recheck(ctx context.Context, progress func(int64)) error {
	var n int64
	for i := range s.verified {
		if e := ctx.Err(); e != nil {
			return e
		}
		if !s.wanted(i) {
			continue
		}
		b := make([]byte, s.pieceSize(i))
		e := s.access(i, b, false)
		if e == nil && sha1.Sum(b) == s.meta.PieceHashes[i] {
			s.verified[i] = true
			n += s.selectedBytes(i)
		} else if e != nil && !os.IsNotExist(e) && !errors.Is(e, io.EOF) {
			return e
		}
		progress(n)
	}
	return s.checkpoint()
}
func (s *storage) block(index, begin, n int) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index < 0 || index >= len(s.verified) || !s.verified[index] || begin < 0 || n <= 0 || n > MaxBlockSize || begin+n > s.pieceSize(index) {
		return nil, errors.New("invalid upload request")
	}
	b := make([]byte, s.pieceSize(index))
	if e := s.access(index, b, false); e != nil {
		return nil, e
	}
	if sha1.Sum(b) != s.meta.PieceHashes[index] {
		s.verified[index] = false
		return nil, errors.New("stored piece changed")
	}
	return append([]byte(nil), b[begin:begin+n]...), nil
}

func (s *storage) fileProgress() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]int64, len(s.meta.Files))
	for i, ok := range s.verified {
		if !ok {
			continue
		}
		a := int64(i) * s.meta.PieceLength
		b := a + int64(s.pieceSize(i))
		for j := range s.selected {
			f := s.meta.Files[j]
			out[j] += max(int64(0), min(b, f.Offset+f.Length)-max(a, f.Offset))
		}
	}
	return out
}
func (s *storage) finalize() error {
	for i := range s.selected {
		f := s.meta.Files[i]
		p := filepath.Join(s.root, f.Path)
		if e := noSymlink(p); e != nil {
			return e
		}
		file, e := os.OpenFile(p, os.O_RDWR, 0644)
		if e != nil {
			return e
		}
		e = file.Truncate(f.Length)
		if e == nil {
			e = file.Sync()
		}
		ce := file.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
	}
	return nil
}
func (s *storage) bitfield() Bitfield {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := make(Bitfield, (len(s.verified)+7)/8)
	for i, v := range s.verified {
		if v {
			b.SetPiece(i)
		}
	}
	return b
}
