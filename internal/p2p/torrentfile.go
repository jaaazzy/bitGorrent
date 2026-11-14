package p2p

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const Port uint16 = 6881
const maxMetadata = 8 << 20

type File struct {
	Path   string `json:"path"`
	Length int64  `json:"length"`
	Offset int64  `json:"offset"`
}
type TorrentFile struct {
	Announce    string     `json:"announce"`
	Trackers    []string   `json:"trackers"`
	InfoHash    [20]byte   `json:"-"`
	PieceHashes [][20]byte `json:"-"`
	PieceLength int64      `json:"pieceLength"`
	Length      int64      `json:"length"`
	Name        string     `json:"name"`
	Files       []File     `json:"files"`
	Private     bool       `json:"private"`
	RawInfo     []byte     `json:"-"`
}

func (t TorrentFile) ID() string { return hex.EncodeToString(t.InfoHash[:]) }

// Keep original info bytes: re-encoding a partial struct changes the info hash.
type decoder struct {
	data []byte
	pos  int
	info []byte
}

func (d *decoder) value(depth int) (any, error) {
	if depth > 32 || d.pos >= len(d.data) {
		return nil, errors.New("invalid or deeply nested bencode")
	}
	switch d.data[d.pos] {
	case 'i':
		d.pos++
		start := d.pos
		for d.pos < len(d.data) && d.data[d.pos] != 'e' {
			d.pos++
		}
		if d.pos == len(d.data) {
			return nil, io.ErrUnexpectedEOF
		}
		s := string(d.data[start:d.pos])
		d.pos++
		var n int64
		if _, e := fmt.Sscan(s, &n); e != nil || fmt.Sprint(n) != s {
			return nil, errors.New("invalid integer")
		}
		return n, nil
	case 'l', 'd':
		kind := d.data[d.pos]
		d.pos++
		list := []any{}
		dict := map[string]any{}
		for d.pos < len(d.data) && d.data[d.pos] != 'e' {
			if kind == 'l' {
				v, e := d.value(depth + 1)
				if e != nil {
					return nil, e
				}
				list = append(list, v)
			} else {
				k, e := d.value(depth + 1)
				if e != nil {
					return nil, e
				}
				key, ok := k.(string)
				if !ok {
					return nil, errors.New("invalid dictionary key")
				}
				if _, ok := dict[key]; ok {
					return nil, errors.New("duplicate dictionary key")
				}
				start := d.pos
				v, e := d.value(depth + 1)
				if e != nil {
					return nil, e
				}
				dict[key] = v
				if depth == 0 && key == "info" {
					d.info = append([]byte(nil), d.data[start:d.pos]...)
				}
			}
		}
		if d.pos == len(d.data) {
			return nil, io.ErrUnexpectedEOF
		}
		d.pos++
		if kind == 'l' {
			return list, nil
		}
		return dict, nil
	default:
		start := d.pos
		n := 0
		for d.pos < len(d.data) && d.data[d.pos] >= '0' && d.data[d.pos] <= '9' {
			n = n*10 + int(d.data[d.pos]-'0')
			if n > maxMetadata {
				return nil, errors.New("bencode string too large")
			}
			d.pos++
		}
		if start == d.pos || d.pos >= len(d.data) || d.data[d.pos] != ':' || (d.pos-start > 1 && d.data[start] == '0') {
			return nil, errors.New("invalid string length")
		}
		d.pos++
		if n > len(d.data)-d.pos {
			return nil, io.ErrUnexpectedEOF
		}
		s := string(d.data[d.pos : d.pos+n])
		d.pos += n
		return s, nil
	}
}
func decode(data []byte) (any, error) {
	d := decoder{data: data}
	v, e := d.value(0)
	if e == nil && d.pos != len(data) {
		e = errors.New("trailing bencode data")
	}
	return v, e
}
func str(v any) string    { s, _ := v.(string); return s }
func integer(v any) int64 { n, _ := v.(int64); return n }
func safeComponent(s string) bool {
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, "/\\:\x00<>\"|?*") || strings.TrimRight(s, ". ") != s {
		return false
	}
	base := strings.ToUpper(strings.Split(s, ".")[0])
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return false
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return false
	}
	return true
}
func Open(path string) (TorrentFile, error) {
	f, e := os.Open(path)
	if e != nil {
		return TorrentFile{}, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, maxMetadata+1))
	if e != nil {
		return TorrentFile{}, e
	}
	return Parse(b)
}
func Parse(data []byte) (TorrentFile, error) {
	if len(data) > maxMetadata {
		return TorrentFile{}, errors.New("metadata exceeds 8 MiB")
	}
	d := decoder{data: data}
	v, e := d.value(0)
	if e != nil {
		return TorrentFile{}, e
	}
	root, ok := v.(map[string]any)
	if !ok || d.pos != len(data) || len(d.info) == 0 {
		return TorrentFile{}, errors.New("invalid torrent dictionary")
	}
	t, e := parseInfo(d.info)
	if e != nil {
		return t, e
	}
	t.Announce = str(root["announce"])
	if t.Announce != "" {
		t.Trackers = append(t.Trackers, t.Announce)
	}
	if tiers, ok := root["announce-list"].([]any); ok {
		for _, tier := range tiers {
			if list, ok := tier.([]any); ok {
				for _, v := range list {
					s := str(v)
					if s != "" && !contains(t.Trackers, s) {
						t.Trackers = append(t.Trackers, s)
					}
				}
			}
		}
	}
	return t, nil
}
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
func parseInfo(raw []byte) (TorrentFile, error) {
	t := TorrentFile{RawInfo: raw, InfoHash: sha1.Sum(raw)}
	v, e := decode(raw)
	if e != nil {
		return t, e
	}
	m, ok := v.(map[string]any)
	if !ok {
		return t, errors.New("invalid info dictionary")
	}
	if integer(m["meta version"]) == 2 {
		return t, errors.New("BitTorrent v2 is not supported; use a v1 torrent")
	}
	t.Name = str(m["name"])
	t.PieceLength = integer(m["piece length"])
	t.Private = integer(m["private"]) == 1
	if !safeComponent(t.Name) || strings.EqualFold(t.Name, ".bitgorrent") || t.PieceLength <= 0 || t.PieceLength > 16<<20 {
		return t, errors.New("invalid name or piece size")
	}
	if fs, ok := m["files"].([]any); ok {
		if len(fs) > 10000 {
			return t, errors.New("torrent exceeds 10,000 files")
		}
		seen := map[string]bool{}
		for _, v := range fs {
			f, ok := v.(map[string]any)
			if !ok {
				return t, errors.New("invalid file entry")
			}
			parts, ok := f["path"].([]any)
			if !ok || len(parts) == 0 {
				return t, errors.New("invalid path")
			}
			names := []string{t.Name}
			for _, p := range parts {
				s := str(p)
				if !safeComponent(s) {
					return t, errors.New("unsafe torrent path")
				}
				names = append(names, s)
			}
			path := filepath.Join(names...)
			key := strings.ToLower(path)
			if seen[key] {
				return t, errors.New("duplicate path")
			}
			seen[key] = true
			n, ok := f["length"].(int64)
			if !ok || n < 0 || n > 1<<50 || t.Length > (1<<50)-n {
				return t, errors.New("invalid file length")
			}
			t.Files = append(t.Files, File{path, n, t.Length})
			t.Length += n
		}
	} else {
		n, ok := m["length"].(int64)
		if !ok || n < 0 || n > 1<<50 {
			return t, errors.New("invalid length")
		}
		t.Length = n
		t.Files = []File{{t.Name, n, 0}}
	}
	if len(t.Files) == 0 {
		return t, errors.New("torrent has no files")
	}
	pieces := []byte(str(m["pieces"]))
	count := (t.Length + t.PieceLength - 1) / t.PieceLength
	if int64(len(pieces)) != count*20 {
		return t, errors.New("piece hashes do not match length")
	}
	for len(pieces) > 0 {
		var h [20]byte
		copy(h[:], pieces[:20])
		t.PieceHashes = append(t.PieceHashes, h)
		pieces = pieces[20:]
	}
	paths := map[string]bool{}
	for _, f := range t.Files {
		paths[strings.ToLower(f.Path)] = true
	}
	for _, f := range t.Files {
		for parent := filepath.Dir(strings.ToLower(f.Path)); parent != "."; parent = filepath.Dir(parent) {
			if paths[parent] {
				return t, errors.New("conflicting file paths")
			}
		}
	}
	return t, nil
}
func MetadataFile(info []byte, trackers []string) []byte {
	var b bytes.Buffer
	b.WriteString("d8:announce")
	s := ""
	if len(trackers) > 0 {
		s = trackers[0]
	}
	fmt.Fprintf(&b, "%d:%s4:info", len(s), s)
	b.Write(info)
	b.WriteByte('e')
	return b.Bytes()
}
