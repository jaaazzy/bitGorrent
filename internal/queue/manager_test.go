package queue_test

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"bitGorrent/internal/p2p"
	"bitGorrent/internal/queue"
	"bitGorrent/internal/queue/queuetest"
	"github.com/jackpal/bencode-go"
)

func metadata(t *testing.T, name string) p2p.TorrentFile {
	t.Helper()
	h := sha1.Sum([]byte("a"))
	var b bytes.Buffer
	if err := bencode.Marshal(&b, map[string]any{"name": name, "length": 1, "pieces": string(h[:]), "piece length": 4}); err != nil {
		t.Fatal(err)
	}
	m, err := p2p.Parse(p2p.MetadataFile(b.Bytes(), nil))
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func await(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.After(3 * time.Second)
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
	case <-time.After(3 * time.Second):
		t.Fatal("engine did not start")
		return nil
	}
}
func noStart(t *testing.T, f *queuetest.Fake) {
	t.Helper()
	select {
	case r := <-f.Started:
		t.Fatalf("unexpected worker: %s %s", r.Options.Metadata.Name, r.Options.Mode)
	case <-time.After(40 * time.Millisecond):
	}
}
func newManager(t *testing.T, f *queuetest.Fake, path string) *queue.Manager {
	t.Helper()
	m, err := queue.New(f, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	return m
}
func add(t *testing.T, m *queue.Manager, name string, ratio float64) string {
	t.Helper()
	meta := metadata(t, name)
	if err := m.Add(meta, t.TempDir(), []int{0}, ratio, 0, 0); err != nil {
		t.Fatal(err)
	}
	return meta.ID()
}
func job(t *testing.T, m *queue.Manager, id string) queue.Job {
	t.Helper()
	for _, j := range m.Snapshot().Jobs {
		if j.ID == id {
			return j
		}
	}
	t.Fatalf("missing job %s", id)
	return queue.Job{}
}
func state(m *queue.Manager, id string) string {
	for _, j := range m.Snapshot().Jobs {
		if j.ID == id {
			return j.Progress.State
		}
	}
	return ""
}

func TestSingleJobLifecycle(t *testing.T) {
	f := queuetest.New()
	m := newManager(t, f, "")
	id := add(t, m, "example.txt", 0)
	first := start(t, f)
	if err := m.Add(metadata(t, "example.txt"), t.TempDir(), []int{0}, 0, 0, 0); !errors.Is(err, queue.ErrDuplicate) {
		t.Fatalf("duplicate: %v", err)
	}
	if err := m.Resume(id); err != nil {
		t.Fatal(err)
	}
	noStart(t, f)
	first.Report(p2p.Progress{State: "downloading", Total: 1, FileCompleted: []int64{0}})
	snap := m.Snapshot()
	snap.Jobs[0].Selected[0] = 99
	snap.Jobs[0].Files[0].Path = "changed"
	snap.Jobs[0].Progress.FileCompleted[0] = 99
	snap.Order[0] = "changed"
	j := job(t, m, id)
	if j.Selected[0] != 0 || j.Files[0].Path != "example.txt" || j.Progress.FileCompleted[0] != 0 || m.Snapshot().Order[0] != id {
		t.Fatal("snapshot aliases state")
	}
	if err := m.Pause(id); err != nil {
		t.Fatal(err)
	}
	await(t, func() bool { return state(m, id) == "paused" })
	if err := m.Resume(id); err != nil {
		t.Fatal(err)
	}
	second := start(t, f)
	second.Finish <- nil
	await(t, func() bool { return state(m, id) == "completed" })
	if job(t, m, id).Progress.Completed != 1 {
		t.Fatal("completion lost")
	}
	if err := m.Remove(id); err != nil {
		t.Fatal(err)
	}
	if len(m.Snapshot().Jobs) != 0 {
		t.Fatal("job remained")
	}
}
func TestFIFOReorderAndLimitDecrease(t *testing.T) {
	f := queuetest.New()
	m := newManager(t, f, "")
	a := add(t, m, "a", 0)
	ra := start(t, f)
	b := add(t, m, "b", 0)
	rb := start(t, f)
	c := add(t, m, "c", 0)
	d := add(t, m, "d", 0)
	if state(m, c) != "queued" || state(m, d) != "queued" {
		t.Fatal("queue did not enforce limit")
	}
	noStart(t, f)
	if err := m.MoveQueued(d, -1); err != nil {
		t.Fatal(err)
	}
	if err := m.MoveQueued(a, 1); err == nil {
		t.Fatal("reordered active job")
	}
	if err := m.SetLimits(1, 1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ra.Cancelled:
		t.Fatal("lower limit cancelled running worker")
	default:
	}
	ra.Finish <- nil
	await(t, func() bool { return state(m, a) == "completed" })
	noStart(t, f)
	rb.Finish <- nil
	next := start(t, f)
	if next.Options.Metadata.ID() != d {
		t.Fatal("FIFO reorder ignored")
	}
	next.Finish <- nil
	next = start(t, f)
	if next.Options.Metadata.ID() != c {
		t.Fatal("FIFO order ignored")
	}
	if state(m, b) != "completed" {
		t.Fatal("other completion changed")
	}
}
func TestQueuedPauseAndResumeAtBack(t *testing.T) {
	f := queuetest.New()
	m := newManager(t, f, "")
	if err := m.SetLimits(0, 0); err != nil {
		t.Fatal(err)
	}
	a := add(t, m, "a", 0)
	b := add(t, m, "b", 0)
	if err := m.Pause(a); err != nil {
		t.Fatal(err)
	}
	if state(m, a) != "paused" {
		t.Fatal("queued pause failed")
	}
	if err := m.Resume(a); err != nil {
		t.Fatal(err)
	}
	if m.Snapshot().Order[1] != a {
		t.Fatal("resume did not append")
	}
	noStart(t, f)
	if err := m.SetLimits(1, 0); err != nil {
		t.Fatal(err)
	}
	r := start(t, f)
	if r.Options.Metadata.ID() != b {
		t.Fatal("wrong queued job started")
	}
}
func TestSeparateSeedingSlots(t *testing.T) {
	f := queuetest.New()
	m := newManager(t, f, "")
	a := add(t, m, "a", 1)
	ra := start(t, f)
	b := add(t, m, "b", 1)
	rb := start(t, f)
	c := add(t, m, "c", 0)
	ra.Finish <- nil
	first, second := start(t, f), start(t, f)
	runs := map[p2p.RunMode]*queuetest.Run{first.Options.Mode: first, second.Options.Mode: second}
	if runs[p2p.ModeSeed] == nil || runs[p2p.ModeDownload] == nil || runs[p2p.ModeSeed].Options.Metadata.ID() != a || runs[p2p.ModeDownload].Options.Metadata.ID() != c {
		t.Fatal("download and seed slots were not independent")
	}
	rb.Finish <- nil
	await(t, func() bool { return state(m, b) == "completed" && job(t, m, b).Intent == "seed" })
	noStart(t, f)
	runs[p2p.ModeSeed].Finish <- nil
	seedB := start(t, f)
	if seedB.Options.Mode != p2p.ModeSeed || seedB.Options.Metadata.ID() != b {
		t.Fatal("waiting seed did not start")
	}
	seedB.Report(p2p.Progress{State: "seeding", Total: 1, Completed: 1, Uploaded: 1})
	seedB.Finish <- nil
	await(t, func() bool { return state(m, b) == "completed" && job(t, m, b).Intent == "done" })
}
func TestImmediatePauseResumeAndStaleReports(t *testing.T) {
	f := queuetest.New()
	gate := make(chan struct{})
	f.AfterCancel = gate
	m := newManager(t, f, "")
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(gate) }) })
	id := add(t, m, "a", 0)
	first := start(t, f)
	if err := m.Pause(id); err != nil {
		t.Fatal(err)
	}
	if err := m.Resume(id); err != nil {
		t.Fatal(err)
	}
	if state(m, id) != "pausing" {
		t.Fatal("released worker slot before exit")
	}
	noStart(t, f)
	once.Do(func() { close(gate) })
	second := start(t, f)
	first.Report(p2p.Progress{State: "error", Message: "stale", Uploaded: 999})
	if job(t, m, id).Progress.Message == "stale" || job(t, m, id).Progress.Uploaded == 999 {
		t.Fatal("accepted stale generation")
	}
	second.Finish <- nil
	await(t, func() bool { return state(m, id) == "completed" })
}
func TestRemoveActiveWaitsAndCannotResurrect(t *testing.T) {
	f := queuetest.New()
	gate := make(chan struct{})
	f.AfterCancel = gate
	m := newManager(t, f, "")
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(gate) }) })
	id := add(t, m, "a", 0)
	first := start(t, f)
	removed := make(chan error, 1)
	go func() { removed <- m.Remove(id) }()
	await(t, func() bool { return state(m, id) == "removing" })
	select {
	case <-removed:
		t.Fatal("removal returned before worker exit")
	default:
	}
	if err := m.Add(metadata(t, "a"), t.TempDir(), []int{0}, 0, 0, 0); !errors.Is(err, queue.ErrDuplicate) {
		t.Fatal("re-add raced old worker")
	}
	once.Do(func() { close(gate) })
	select {
	case err := <-removed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("remove deadlocked")
	}
	first.Report(p2p.Progress{State: "downloading"})
	if len(m.Snapshot().Jobs) != 0 {
		t.Fatal("removed job resurrected")
	}
	add(t, m, "a", 0)
	second := start(t, f)
	first.Report(p2p.Progress{State: "error", Message: "old entry"})
	if job(t, m, id).Progress.Message == "old entry" {
		t.Fatal("old entry changed re-added job")
	}
	second.Finish <- nil
}
func TestEngineFailureRetryAndRecheck(t *testing.T) {
	f := queuetest.New()
	m := newManager(t, f, "")
	id := add(t, m, "a", 0)
	r := start(t, f)
	r.Finish <- errors.New("disk full")
	await(t, func() bool { return state(m, id) == "error" })
	if job(t, m, id).Progress.Message != "disk full" {
		t.Fatal("lost engine error")
	}
	if err := m.Resume(id); err != nil {
		t.Fatal(err)
	}
	r = start(t, f)
	if err := m.Recheck(id); err == nil {
		t.Fatal("rechecked active job")
	}
	if err := m.Pause(id); err != nil {
		t.Fatal(err)
	}
	await(t, func() bool { return state(m, id) == "paused" })
	if err := m.Recheck(id); err != nil {
		t.Fatal(err)
	}
	r = start(t, f)
	if r.Options.Mode != p2p.ModeCheck || !r.Options.CheckOnly {
		t.Fatal("recheck would download")
	}
	r.Finish <- nil
	await(t, func() bool { return state(m, id) == "paused" })
	noStart(t, f)
}
func TestShutdownWaitsForWorkers(t *testing.T) {
	f := queuetest.New()
	gate := make(chan struct{})
	f.AfterCancel = gate
	m := newManager(t, f, "")
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(gate) }) })
	a := add(t, m, "a", 0)
	ra := start(t, f)
	add(t, m, "b", 0)
	rb := start(t, f)
	closed := make(chan error, 1)
	go func() { closed <- m.Close() }()
	<-ra.Cancelled
	<-rb.Cancelled
	select {
	case <-closed:
		t.Fatal("shutdown returned before workers stopped")
	default:
	}
	if err := m.Resume(a); !errors.Is(err, queue.ErrClosed) {
		t.Fatalf("accepted command during shutdown: %v", err)
	}
	once.Do(func() { close(gate) })
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown blocked")
	}
	if state(m, a) != "paused" {
		t.Fatal("shutdown did not pause job")
	}
}
func TestPersistentOrderLimitsAndPausedRecovery(t *testing.T) {
	f := queuetest.New()
	path := filepath.Join(t.TempDir(), "queue.json")
	m := newManager(t, f, path)
	if err := m.SetLimits(0, 2); err != nil {
		t.Fatal(err)
	}
	a := add(t, m, "a", 1)
	b := add(t, m, "b", 0)
	if err := m.MoveQueued(b, -1); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	restoredFake := queuetest.New()
	restored := newManager(t, restoredFake, path)
	s := restored.Snapshot()
	if len(s.Jobs) != 2 || s.Order[0] != b || s.Order[1] != a || s.Limits.Downloads != 0 || s.Limits.Seeds != 2 {
		t.Fatalf("wrong recovery: %+v", s)
	}
	for _, j := range s.Jobs {
		if j.Progress.State != "paused" {
			t.Fatal("restored active job")
		}
	}
	noStart(t, restoredFake)
}
func TestLegacyMigrationIsReadOnlyUntilSuccessfulWrite(t *testing.T) {
	meta := metadata(t, "legacy")
	j := queue.Job{ID: meta.ID(), Name: meta.Name, Destination: t.TempDir(), Selected: []int{0}, Files: meta.Files, SeedRatio: 1, Progress: p2p.Progress{State: "seeding", Total: 1, Completed: 1, Uploaded: 1}}
	data, err := json.Marshal(struct {
		Job  queue.Job `json:"job"`
		Info []byte    `json:"info"`
	}{j, meta.RawInfo})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "job.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	f := queuetest.New()
	m := newManager(t, f, path)
	read, _ := os.ReadFile(path)
	if !bytes.Equal(read, data) {
		t.Fatal("load overwrote legacy state")
	}
	if restored := job(t, m, j.ID); restored.Progress.State != "paused" || restored.Progress.Uploaded != 1 || restored.Selected[0] != 0 {
		t.Fatal("legacy data lost")
	}
	noStart(t, f)
	if err = m.SetLimits(3, 2); err != nil {
		t.Fatal(err)
	}
	read, _ = os.ReadFile(path)
	var state struct {
		Version int               `json:"version"`
		Jobs    []json.RawMessage `json:"jobs"`
	}
	if json.Unmarshal(read, &state) != nil || state.Version != 1 || len(state.Jobs) != 1 {
		t.Fatal("legacy state was not migrated")
	}
}
func TestInvalidStateIsPreserved(t *testing.T) {
	for _, data := range []string{"{truncated", `{"version":99}`, `{"version":1,"jobs":[],"order":["missing"],"limits":{"downloads":2,"seeds":1}}`} {
		path := filepath.Join(t.TempDir(), "queue.json")
		os.WriteFile(path, []byte(data), 0600)
		if m, err := queue.New(queuetest.New(), path, nil); err == nil || m != nil {
			t.Fatal("accepted corrupt state")
		}
		read, _ := os.ReadFile(path)
		if string(read) != data {
			t.Fatal("destroyed original state")
		}
	}
}
func TestPersistenceFailureRollsBackCommand(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "state")
	backup := filepath.Join(root, "original")
	path := filepath.Join(dir, "queue.json")
	f := queuetest.New()
	m := newManager(t, f, path)
	if err := m.SetLimits(0, 1); err != nil {
		t.Fatal(err)
	}
	a := add(t, m, "a", 0)
	original, _ := os.ReadFile(path)
	if err := os.Rename(dir, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("block parent directory"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(dir); os.Rename(backup, dir) })
	meta := metadata(t, "b")
	destination := t.TempDir()
	if err := m.Add(meta, destination, []int{0}, 0, 0, 0); err == nil {
		t.Fatal("ignored save error")
	}
	if len(m.Snapshot().Jobs) != 1 || job(t, m, a).Progress.State != "queued" || m.Snapshot().Error == "" {
		t.Fatal("failed command changed queue")
	}
	read, _ := os.ReadFile(filepath.Join(backup, "queue.json"))
	if !bytes.Equal(read, original) {
		t.Fatal("failed save changed valid state")
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, dir); err != nil {
		t.Fatal(err)
	}
	if err := m.Add(meta, destination, []int{0}, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	if m.Snapshot().Error != "" {
		t.Fatal("save recovery did not clear error")
	}
}
func TestConcurrentCommandsAndReports(t *testing.T) {
	f := queuetest.New()
	m := newManager(t, f, "")
	id := add(t, m, "a", 0)
	first := start(t, f)
	var wg sync.WaitGroup
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 8; i++ {
				m.Snapshot()
				m.Pause(id)
				m.Resume(id)
				first.Report(p2p.Progress{State: "downloading", Completed: 0, Uploaded: 0, FileCompleted: []int64{0}})
			}
		}()
	}
	wg.Wait()
	if len(m.Snapshot().Jobs) != 1 {
		t.Fatal("lost job during concurrent commands")
	}
}

func TestShutdownDuringActiveRemoval(t *testing.T) {
	f := queuetest.New()
	gate := make(chan struct{})
	f.AfterCancel = gate
	m := newManager(t, f, filepath.Join(t.TempDir(), "queue.json"))
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(gate) }) })
	id := add(t, m, "a", 0)
	start(t, f)
	removed := make(chan error, 1)
	go func() { removed <- m.Remove(id) }()
	await(t, func() bool { return state(m, id) == "removing" })
	closed := make(chan error, 1)
	go func() { closed <- m.Close() }()
	once.Do(func() { close(gate) })
	for _, done := range []chan error{removed, closed} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("shutdown/removal deadlocked")
		}
	}
	if len(m.Snapshot().Jobs) != 0 {
		t.Fatal("removed job persisted during shutdown")
	}
}
func TestWaitingSeedCanBePausedAndUploadedTotalsSurvive(t *testing.T) {
	f := queuetest.New()
	path := filepath.Join(t.TempDir(), "queue.json")
	m := newManager(t, f, path)
	if err := m.SetLimits(1, 0); err != nil {
		t.Fatal(err)
	}
	id := add(t, m, "a", 2)
	r := start(t, f)
	r.Report(p2p.Progress{State: "downloading", Total: 1, Completed: 1, Uploaded: 1})
	r.Finish <- nil
	await(t, func() bool { return state(m, id) == "completed" && job(t, m, id).Intent == "seed" })
	noStart(t, f)
	if err := m.Pause(id); err != nil {
		t.Fatal(err)
	}
	if err := m.SetLimits(1, 1); err != nil {
		t.Fatal(err)
	}
	noStart(t, f)
	if err := m.Resume(id); err != nil {
		t.Fatal(err)
	}
	r = start(t, f)
	if r.Options.InitialUploaded != 1 {
		t.Fatal("lost uploaded total on resume")
	}
	r.Finish <- nil
	r = start(t, f)
	if r.Options.Mode != p2p.ModeSeed || r.Options.InitialUploaded != 1 {
		t.Fatal("lost uploaded total on seed transition")
	}
	r.Report(p2p.Progress{State: "seeding", Total: 1, Completed: 1, Uploaded: 2})
	r.Finish <- nil
	await(t, func() bool { return state(m, id) == "completed" && job(t, m, id).Intent == "done" })
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	restored := newManager(t, queuetest.New(), path)
	if job(t, restored, id).Progress.Uploaded != 2 {
		t.Fatal("uploaded total did not survive restart")
	}
}
func TestConcurrentDuplicateAdds(t *testing.T) {
	f := queuetest.New()
	m := newManager(t, f, "")
	meta := metadata(t, "same")
	destination := t.TempDir()
	results := make(chan error, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- m.Add(meta, destination, []int{0}, 0, 0, 0) }()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, queue.ErrDuplicate) {
			t.Fatal(err)
		}
	}
	if success != 1 || len(m.Snapshot().Jobs) != 1 {
		t.Fatal("duplicate add was not atomic")
	}
	start(t, f)
	noStart(t, f)
}
func TestDestinationOverlapRejectedBeforeScheduling(t *testing.T) {
	f := queuetest.New()
	m := newManager(t, f, "")
	if err := m.SetLimits(0, 0); err != nil {
		t.Fatal(err)
	}
	first := metadata(t, "same.bin")
	v := map[string]any{"name": "same.bin", "length": 2, "pieces": string(make([]byte, 20)), "piece length": 4}
	var raw bytes.Buffer
	bencode.Marshal(&raw, v)
	second, err := p2p.Parse(p2p.MetadataFile(raw.Bytes(), nil))
	if err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	if err = m.Add(first, destination, []int{0}, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err = m.Add(second, destination, []int{0}, 0, 0, 0); err == nil {
		t.Fatal("accepted conflicting output paths")
	}
	if len(m.Snapshot().Jobs) != 1 {
		t.Fatal("failed add changed queue")
	}
}
