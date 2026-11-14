package queue_test

import (
	"bitGorrent/internal/p2p"
	"bitGorrent/internal/queue"
	"bitGorrent/internal/queue/queuetest"
	"bytes"
	"crypto/sha1"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackpal/bencode-go"
)

func metadata(t *testing.T) p2p.TorrentFile {
	t.Helper()
	h := sha1.Sum([]byte("a"))
	var b bytes.Buffer
	bencode.Marshal(&b, map[string]any{"name": "example.txt", "length": 1, "pieces": string(h[:]), "piece length": 4})
	m, e := p2p.Parse(p2p.MetadataFile(b.Bytes(), nil))
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func await(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("operation did not settle")
		case <-tick.C:
		}
	}
}
func start(t *testing.T, f *queuetest.Fake) *queuetest.Run {
	t.Helper()
	select {
	case r := <-f.Started:
		return r
	case <-time.After(time.Second):
		t.Fatal("engine did not start")
		return nil
	}
}
func TestSingleJobLifecycle(t *testing.T) {
	f := queuetest.New()
	m, e := queue.New(f, "", nil)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	meta := metadata(t)
	if e = m.Add(meta, t.TempDir(), []int{0}, 1, 0, 0); e != nil {
		t.Fatal(e)
	}
	first := start(t, f)
	if e = m.Add(meta, t.TempDir(), []int{0}, 1, 0, 0); !errors.Is(e, queue.ErrBusy) {
		t.Fatalf("did not enforce single job: %v", e)
	}
	if e = m.Start(false); e == nil {
		t.Fatal("started duplicate worker")
	}
	if e = m.Remove(); e == nil {
		t.Fatal("removed active worker")
	}
	first.Progress <- p2p.Progress{State: "downloading", Completed: 0, Total: 1}
	snap := m.Snapshot()
	snap.Job.Selected[0] = 99
	if m.Snapshot().Job.Selected[0] != 0 {
		t.Fatal("snapshot aliases mutable state")
	}
	if e = m.Pause(); e != nil {
		t.Fatal(e)
	}
	await(t, func() bool { return m.Snapshot().Job.Progress.State == "paused" })
	if e = m.Start(false); e != nil {
		t.Fatal(e)
	}
	second := start(t, f)
	second.Progress <- p2p.Progress{State: "downloading", Total: 1, Completed: 1}
	second.Finish <- nil
	await(t, func() bool { return m.Snapshot().Job.Progress.State == "completed" })
	if e = m.Remove(); e != nil {
		t.Fatal(e)
	}
	if m.Snapshot().Job != nil {
		t.Fatal("job still present")
	}
}
func TestRestorePausedAndShutdown(t *testing.T) {
	f := queuetest.New()
	path := filepath.Join(t.TempDir(), "job.json")
	m, e := queue.New(f, path, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Add(metadata(t), t.TempDir(), []int{0}, 1, 0, 0); e != nil {
		t.Fatal(e)
	}
	run := start(t, f)
	closed := make(chan struct{})
	go func() { m.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("shutdown blocked")
	}
	select {
	case <-run.Stopped:
	default:
		t.Fatal("shutdown returned before engine stopped")
	}
	restored, e := queue.New(queuetest.New(), path, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	if j := restored.Snapshot().Job; j == nil || j.Progress.State != "paused" || j.Name != "example.txt" {
		t.Fatalf("bad recovery: %+v", j)
	}
}
func TestEngineFailureAndRecheck(t *testing.T) {
	f := queuetest.New()
	m, _ := queue.New(f, "", nil)
	defer m.Close()
	if e := m.Add(metadata(t), t.TempDir(), []int{0}, 0, 0, 0); e != nil {
		t.Fatal(e)
	}
	run := start(t, f)
	run.Finish <- errors.New("disk full")
	await(t, func() bool { return m.Snapshot().Job.Progress.State == "error" })
	if m.Snapshot().Job.Progress.Message != "disk full" {
		t.Fatal("lost engine error")
	}
	if e := m.Start(true); e != nil {
		t.Fatal(e)
	}
	run = start(t, f)
	if !run.Options.CheckOnly {
		t.Fatal("recheck started network transfer")
	}
	run.Finish <- nil
	await(t, func() bool { return m.Snapshot().Job.Progress.State == "paused" })
}
