package queue

import (
	"bytes"
	"crypto/sha1"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"bitGorrent/internal/p2p"
	"bitGorrent/internal/queue/queuetest"
	"github.com/jackpal/bencode-go"
)

func TestProgressContinuesDuringDurableCommand(t *testing.T) {
	f := queuetest.New()
	m, err := New(f, filepath.Join(t.TempDir(), "queue.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	h := sha1.Sum([]byte("a"))
	var encoded bytes.Buffer
	bencode.Marshal(&encoded, map[string]any{"name": "a", "length": 1, "piece length": 4, "pieces": string(h[:])})
	meta, err := p2p.Parse(p2p.MetadataFile(encoded.Bytes(), nil))
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Add(meta, t.TempDir(), []int{0}, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	var run *queuetest.Run
	select {
	case run = <-f.Started:
	case <-time.After(time.Second):
		t.Fatal("runner did not start")
	}
	writing, release := make(chan struct{}), make(chan struct{})
	var gate, cleanup sync.Once
	t.Cleanup(func() { cleanup.Do(func() { close(release) }) })
	m.commands.Lock()
	m.writeState = func(path string, data []byte) error {
		gate.Do(func() { close(writing); <-release })
		return writeAtomic(path, data)
	}
	m.commands.Unlock()
	saved := make(chan error, 1)
	go func() { saved <- m.SetLimits(1, 1) }()
	select {
	case <-writing:
	case <-time.After(time.Second):
		t.Fatal("save did not start")
	}
	reported := make(chan struct{})
	go func() {
		run.Report(p2p.Progress{State: "downloading", Total: 1, Uploaded: 1, DownloadSpeed: 10, FileCompleted: []int64{0}})
		close(reported)
	}()
	select {
	case <-reported:
	case <-time.After(time.Second):
		t.Fatal("filesystem I/O held the state mutex")
	}
	cleanup.Do(func() { close(release) })
	select {
	case err = <-saved:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("save did not finish")
	}
	j := m.Snapshot().Jobs[0]
	if j.Progress.State != "downloading" || j.Progress.Uploaded != 1 || j.Progress.DownloadSpeed != 10 {
		t.Fatalf("durable command overwrote concurrent progress: %+v", j.Progress)
	}
}
