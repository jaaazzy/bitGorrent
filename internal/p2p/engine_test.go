package p2p

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackpal/bencode-go"
)

func fixture(t testing.TB, data []byte, piece int, multi bool) TorrentFile {
	t.Helper()
	var hashes []byte
	for i := 0; i < len(data); i += piece {
		h := sha1.Sum(data[i:min(i+piece, len(data))])
		hashes = append(hashes, h[:]...)
	}
	info := map[string]any{"name": "fixture.bin", "piece length": piece, "pieces": string(hashes), "private": 1, "extra-key": "preserve me"}
	if multi {
		info["name"] = "bundle"
		info["files"] = []any{map[string]any{"length": 3, "path": []string{"a.bin"}}, map[string]any{"length": 3, "path": []string{"b.bin"}}, map[string]any{"length": len(data) - 6, "path": []string{"c.bin"}}}
	} else {
		info["length"] = len(data)
	}
	var b bytes.Buffer
	if e := bencode.Marshal(&b, info); e != nil {
		t.Fatal(e)
	}
	meta, e := Parse(MetadataFile(b.Bytes(), nil))
	if e != nil {
		t.Fatal(e)
	}
	return meta
}
func TestOriginalInfoHash(t *testing.T) {
	m := fixture(t, []byte("abcdefgh"), 4, false)
	if m.InfoHash != sha1.Sum(m.RawInfo) {
		t.Fatal("hash lost original bytes")
	}
	if !bytes.Contains(m.RawInfo, []byte("extra-key")) {
		t.Fatal("lost unknown info key")
	}
}
func TestInvalidMetadata(t *testing.T) {
	for _, name := range []string{"..", "a/b", "a\\b", "CON", "x.", "a:b"} {
		t.Run(name, func(t *testing.T) {
			var b bytes.Buffer
			bencode.Marshal(&b, map[string]any{"name": name, "piece length": 4, "length": 0, "pieces": ""})
			if _, e := Parse(MetadataFile(b.Bytes(), nil)); e == nil {
				t.Fatal("accepted unsafe name")
			}
		})
	}
	for _, raw := range []string{"d4:infoi1ee", "d4:infod4:name1:x6:lengthi1e12:piece lengthi0e6:pieces0:ee", "d4:infod4:name1:x4:name1:yee"} {
		if _, e := Parse([]byte(raw)); e == nil {
			t.Fatalf("accepted invalid metadata %q", raw)
		}
	}
}
func TestSelectedFilesBoundaryAndRecovery(t *testing.T) {
	data := []byte("abcdefgh")
	m := fixture(t, data, 4, true)
	root := t.TempDir()
	s, e := openStorage(m, root, []int{1})
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if e = s.commit(i, data[i*4:(i+1)*4]); e != nil {
			t.Fatal(e)
		}
	}
	if !s.complete() || s.completed() != 3 {
		t.Fatal("incorrect selected progress")
	}
	b, e := os.ReadFile(filepath.Join(root, "bundle", "b.bin"))
	if e != nil || string(b) != "def" {
		t.Fatalf("selected file: %q %v", b, e)
	}
	if _, e = os.Stat(filepath.Join(root, "bundle", "a.bin")); !os.IsNotExist(e) {
		t.Fatal("created skipped file")
	}
	resumed, e := openStorage(m, root, []int{1})
	if e != nil {
		t.Fatal(e)
	}
	if e = resumed.recheck(context.Background(), func(int64) {}); e != nil {
		t.Fatal(e)
	}
	if !resumed.complete() {
		t.Fatal("lost durable pieces")
	}
	if e = os.WriteFile(filepath.Join(root, "bundle", "b.bin"), []byte("Xef"), 0644); e != nil {
		t.Fatal(e)
	}
	recheck, e := openStorage(m, root, []int{1})
	if e != nil {
		t.Fatal(e)
	}
	if e = recheck.recheck(context.Background(), func(int64) {}); e != nil {
		t.Fatal(e)
	}
	if recheck.complete() || recheck.completed() != 2 {
		t.Fatalf("trusted corrupt piece: %d", recheck.completed())
	}
}
func TestDestinationCollision(t *testing.T) {
	m := fixture(t, []byte("abcd"), 4, false)
	root := t.TempDir()
	path := filepath.Join(root, m.Name)
	os.WriteFile(path, []byte("mine"), 0644)
	if _, e := openStorage(m, root, []int{0}); e == nil {
		t.Fatal("overwrote existing file")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "mine" {
		t.Fatal("changed destination")
	}
}
func TestCommitDiskFailure(t *testing.T) {
	m := fixture(t, []byte("abcd"), 4, false)
	root := t.TempDir()
	s, e := openStorage(m, root, []int{0})
	if e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(root, m.Name)
	os.Remove(p)
	os.Mkdir(p, 0755)
	if e = s.commit(0, []byte("abcd")); e == nil {
		t.Fatal("ignored disk error")
	}
	if s.complete() {
		t.Fatal("marked failed write verified")
	}
}
func TestOversizedPeerMessage(t *testing.T) {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, 2<<20)
	if _, e := ReadMessage(bytes.NewReader(b)); e == nil {
		t.Fatal("accepted oversized message")
	}
}

type shortWriter struct{ bytes.Buffer }

func (w *shortWriter) Write(b []byte) (int, error) { return w.Buffer.Write(b[:min(2, len(b))]) }
func TestShortWrites(t *testing.T) {
	w := &shortWriter{}
	if e := writeFull(w, []byte("abcdefgh")); e != nil || w.String() != "abcdefgh" {
		t.Fatal("lost bytes")
	}
}
func TestDuplicateBlockRejected(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c := &Client{Conn: left, Choked: false}
	go func() {
		defer right.Close()
		ReadMessage(right)
		ReadMessage(right)
		payload := make([]byte, 8+MaxBlockSize)
		msg := (&Message{ID: MsgPiece, Payload: payload}).Serialize()
		writeFull(right, msg)
		writeFull(right, msg)
	}()
	if _, e := fetchPiece(ctx, c, 0, 2*MaxBlockSize, newLimiter(0)); e == nil {
		t.Fatal("accepted duplicate block")
	}
}
func TestEngineLocalSwarm(t *testing.T) {
	data := bytes.Repeat([]byte("bitGorrent verified bytes\n"), 4096)
	m := fixture(t, data, MaxBlockSize, false)
	seed, e := openStorage(m, t.TempDir(), []int{0})
	if e != nil {
		t.Fatal(e)
	}
	for i := range m.PieceHashes {
		a := i * MaxBlockSize
		if e = seed.commit(i, data[a:min(a+MaxBlockSize, len(data))]); e != nil {
			t.Fatal(e)
		}
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	seedDone := make(chan struct{})
	go func(meta TorrentFile) {
		defer close(seedDone)
		serveSeeds(ctx, listener, meta, [20]byte{}, seed, newLimiter(0), func(int) {})
	}(m)
	defer func() { cancel(); listener.Close(); <-seedDone }()
	addr := listener.Addr().(*net.TCPAddr)
	compact := make([]byte, 6)
	copy(compact, net.IPv4(127, 0, 0, 1).To4())
	binary.BigEndian.PutUint16(compact[4:], uint16(addr.Port))
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("info_hash") != string(m.InfoHash[:]) {
			t.Error("tracker info hash mismatch")
		}
		bencode.Marshal(w, map[string]any{"interval": 60, "peers": string(compact)})
	}))
	defer tracker.Close()
	m.Trackers = []string{tracker.URL}
	root := t.TempDir()
	var last Progress
	e = (Engine{}).Run(ctx, RunOptions{Metadata: m, Destination: root, Selected: []int{0}}, func(p Progress) { last = p })
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(root, m.Name))
	if e != nil || !bytes.Equal(b, data) {
		t.Fatal("download differs", e)
	}
	if last.Completed != int64(len(data)) || last.State != "completed" {
		t.Fatalf("wrong final progress: %+v", last)
	}
	// A fresh run verifies existing files and needs no tracker or peer.
	m.Trackers = nil
	if e = (Engine{}).Run(ctx, RunOptions{Metadata: m, Destination: root, Selected: []int{0}, CheckOnly: true}, func(Progress) {}); e != nil {
		t.Fatal(e)
	}
}
func TestNoPeersCancellation(t *testing.T) {
	m := fixture(t, []byte("abcd"), 4, false)
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bencode.Marshal(w, map[string]any{"interval": 60, "peers": ""})
	}))
	defer tracker.Close()
	m.Trackers = []string{tracker.URL}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	e := (Engine{}).Run(ctx, RunOptions{Metadata: m, Destination: t.TempDir(), Selected: []int{0}}, func(Progress) {})
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("did not cancel: %v", e)
	}
}
func TestHandshakeCancellation(t *testing.T) {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, e := l.Accept()
		if e == nil {
			defer c.Close()
			io.Copy(io.Discard, c)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	addr := l.Addr().(*net.TCPAddr)
	_, e = connectContext(ctx, Peer{addr.IP, uint16(addr.Port)}, [20]byte{}, [20]byte{})
	if e == nil {
		t.Fatal("expected cancellation")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("connection leaked")
	}
}
func TestMagnet(t *testing.T) {
	valid := "magnet:?xt=urn:btih:0123456789012345678901234567890123456789&dn=Example&tr=https%3A%2F%2Ftracker.example%2Fannounce"
	m, e := ParseMagnet(valid)
	if e != nil || m.Name != "Example" || len(m.Trackers) != 1 {
		t.Fatalf("magnet: %+v %v", m, e)
	}
	if _, e = ParseMagnet("magnet:?xt=urn:btmh:1234"); e == nil {
		t.Fatal("accepted unsupported hash")
	}
}
