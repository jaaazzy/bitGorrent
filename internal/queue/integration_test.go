package queue_test

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"bitGorrent/internal/p2p"
	"bitGorrent/internal/queue"
	"github.com/jackpal/bencode-go"
)

func TestConcurrentLocalSwarmAndRestartRecovery(t *testing.T) {
	const pieceLength = 16 << 10
	type fixture struct {
		meta               p2p.TorrentFile
		data               []byte
		gate               chan struct{}
		mu                 sync.Mutex
		firstPieceRequests int
	}
	fixtures := map[[20]byte]*fixture{}
	ordered := []*fixture{}
	for _, name := range []string{"a.bin", "b.bin", "c.bin"} {
		data := bytes.Repeat([]byte(name+" verified payload\n"), 8192)
		var hashes []byte
		for start := 0; start < len(data); start += pieceLength {
			h := sha1.Sum(data[start:min(start+pieceLength, len(data))])
			hashes = append(hashes, h[:]...)
		}
		var raw bytes.Buffer
		if err := bencode.Marshal(&raw, map[string]any{"name": name, "length": len(data), "piece length": pieceLength, "pieces": string(hashes), "private": 1}); err != nil {
			t.Fatal(err)
		}
		meta, err := p2p.Parse(p2p.MetadataFile(raw.Bytes(), nil))
		if err != nil {
			t.Fatal(err)
		}
		f := &fixture{meta: meta, data: data, gate: make(chan struct{})}
		fixtures[meta.InfoHash] = f
		ordered = append(ordered, f)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().(*net.TCPAddr)
	compact := []byte{127, 0, 0, 1, 0, 0}
	binary.BigEndian.PutUint16(compact[4:], uint16(addr.Port))
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bencode.Marshal(w, map[string]any{"interval": 60, "peers": string(compact)})
	}))
	defer tracker.Close()
	for _, f := range ordered {
		f.meta.Trackers = []string{tracker.URL}
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				stop := context.AfterFunc(ctx, func() { conn.Close() })
				defer stop()
				conn.SetDeadline(time.Now().Add(20 * time.Second))
				h, err := p2p.ReadHandshake(conn)
				if err != nil {
					return
				}
				f := fixtures[h.InfoHash]
				if f == nil {
					return
				}
				write := func(message []byte) error { _, err := io.Copy(conn, bytes.NewReader(message)); return err }
				if write(p2p.NewHandshake(f.meta.InfoHash, [20]byte{}).Serialize()) != nil {
					return
				}
				bf := make(p2p.Bitfield, (len(f.meta.PieceHashes)+7)/8)
				for i := range f.meta.PieceHashes {
					bf.SetPiece(i)
				}
				if write((&p2p.Message{ID: p2p.MsgBitfield, Payload: bf}).Serialize()) != nil {
					return
				}
				if write((&p2p.Message{ID: p2p.MsgUnchoke}).Serialize()) != nil {
					return
				}
				for {
					message, err := p2p.ReadMessage(conn)
					if err != nil {
						return
					}
					if message == nil || message.ID != p2p.MsgRequest {
						continue
					}
					if len(message.Payload) != 12 {
						return
					}
					index := int(binary.BigEndian.Uint32(message.Payload[:4]))
					begin := int(binary.BigEndian.Uint32(message.Payload[4:8]))
					n := int(binary.BigEndian.Uint32(message.Payload[8:]))
					if index == 0 {
						f.mu.Lock()
						f.firstPieceRequests++
						f.mu.Unlock()
					} else {
						select {
						case <-f.gate:
						case <-ctx.Done():
							return
						}
					}
					offset := index*pieceLength + begin
					if offset < 0 || n <= 0 || n > p2p.MaxBlockSize || offset+n > len(f.data) {
						return
					}
					payload := make([]byte, 8+n)
					copy(payload[:8], message.Payload[:8])
					copy(payload[8:], f.data[offset:offset+n])
					if write((&p2p.Message{ID: p2p.MsgPiece, Payload: payload}).Serialize()) != nil {
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { cancel(); listener.Close(); wg.Wait() })
	path := filepath.Join(t.TempDir(), "queue.json")
	m, err := queue.New(p2p.Engine{}, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	destinations := []string{t.TempDir(), t.TempDir(), t.TempDir()}
	for i, f := range ordered {
		if err = m.Add(f.meta, destinations[i], []int{0}, 0, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	a, b, c := ordered[0], ordered[1], ordered[2]
	await(t, func() bool {
		return job(t, m, a.meta.ID()).Progress.Completed >= pieceLength && job(t, m, b.meta.ID()).Progress.Completed >= pieceLength
	})
	if state(m, c.meta.ID()) != "queued" {
		t.Fatal("third real transfer did not wait")
	}
	if err = m.Pause(a.meta.ID()); err != nil {
		t.Fatal(err)
	}
	await(t, func() bool { return state(m, a.meta.ID()) == "paused" })
	await(t, func() bool { return job(t, m, c.meta.ID()).Progress.Completed >= pieceLength })
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := queue.New(p2p.Engine{}, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { restored.Close() })
	for _, j := range restored.Snapshot().Jobs {
		if j.Progress.State != "paused" {
			t.Fatal("restored transfer started automatically")
		}
	}
	a.mu.Lock()
	before := a.firstPieceRequests
	a.mu.Unlock()
	close(a.gate)
	if err = restored.Resume(a.meta.ID()); err != nil {
		t.Fatal(err)
	}
	await(t, func() bool { return state(restored, a.meta.ID()) == "completed" })
	result, err := os.ReadFile(filepath.Join(destinations[0], a.meta.Name))
	if err != nil || !bytes.Equal(result, a.data) {
		t.Fatal("resumed payload differs", err)
	}
	a.mu.Lock()
	after := a.firstPieceRequests
	a.mu.Unlock()
	if after != before {
		t.Fatal("re-downloaded verified first piece after restart")
	}
}
