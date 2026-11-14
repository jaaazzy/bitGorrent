// Package queue manages a single torrent's lifecycle and persistent job state.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"bitGorrent/internal/p2p"
)

var ErrBusy = errors.New("only one torrent can be managed at a time; remove the current torrent before adding another")

type Runner interface {
	Run(context.Context, p2p.RunOptions, func(p2p.Progress)) error
}
type Job struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Destination   string       `json:"destination"`
	Selected      []int        `json:"selected"`
	Files         []p2p.File   `json:"files"`
	Trackers      []string     `json:"trackers"`
	SeedRatio     float64      `json:"seedRatio"`
	DownloadLimit int64        `json:"downloadLimit"`
	UploadLimit   int64        `json:"uploadLimit"`
	Progress      p2p.Progress `json:"progress"`
}
type Snapshot struct {
	Revision uint64 `json:"revision"`
	Job      *Job   `json:"job"`
}
type saved struct {
	Job  Job    `json:"job"`
	Info []byte `json:"info"`
}
type Manager struct {
	mu        sync.Mutex
	persistMu sync.Mutex
	runner    Runner
	emit      func(Snapshot)
	path      string
	job       *Job
	meta      p2p.TorrentFile
	revision  uint64
	cancel    context.CancelFunc
	done      chan struct{}
	closed    bool
}

func unfinished(done <-chan struct{}) bool {
	if done == nil {
		return false
	}
	select {
	case <-done:
		return false
	default:
		return true
	}
}

func New(r Runner, path string, emit func(Snapshot)) (*Manager, error) {
	m := &Manager{runner: r, path: path, emit: emit}
	if path == "" {
		return m, nil
	}
	b, e := os.ReadFile(path)
	if os.IsNotExist(e) {
		return m, nil
	}
	if e != nil {
		return nil, e
	}
	if len(b) > 16<<20 {
		return nil, errors.New("saved job is too large")
	}
	var s saved
	if e = json.Unmarshal(b, &s); e != nil {
		return nil, e
	}
	meta, e := p2p.Parse(p2p.MetadataFile(s.Info, s.Job.Trackers))
	if e != nil {
		return nil, e
	}
	meta.Trackers = s.Job.Trackers
	if meta.ID() != s.Job.ID {
		return nil, errors.New("saved job hash does not match metadata")
	}
	m.meta = meta
	m.job = &s.Job
	m.job.Progress.State = "paused"
	m.job.Progress.DownloadSpeed = 0
	m.job.Progress.UploadSpeed = 0
	m.job.Progress.Peers = 0
	m.job.Progress.Message = "Restored safely. Resume to verify existing pieces."
	return m, nil
}
func (m *Manager) snapshotLocked() Snapshot {
	s := Snapshot{Revision: m.revision}
	if m.job != nil {
		j := *m.job
		j.Selected = append([]int{}, j.Selected...)
		j.Files = append([]p2p.File{}, j.Files...)
		j.Trackers = append([]string{}, j.Trackers...)
		j.Progress.FileCompleted = append([]int64{}, j.Progress.FileCompleted...)
		s.Job = &j
	}
	return s
}
func (m *Manager) Snapshot() Snapshot { m.mu.Lock(); defer m.mu.Unlock(); return m.snapshotLocked() }
func (m *Manager) notify() {
	if m.emit != nil {
		m.emit(m.Snapshot())
	}
}
func (m *Manager) save() error {
	if m.path == "" {
		return nil
	}
	m.persistMu.Lock()
	defer m.persistMu.Unlock()
	m.mu.Lock()
	snap := m.snapshotLocked()
	raw := append([]byte(nil), m.meta.RawInfo...)
	m.mu.Unlock()
	if snap.Job == nil {
		e := os.Remove(m.path)
		if os.IsNotExist(e) {
			return nil
		}
		return e
	}
	b, e := json.Marshal(saved{*snap.Job, raw})
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(m.path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(m.path), "job-*")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(tmp, m.path)
}
func (m *Manager) Add(t p2p.TorrentFile, destination string, selected []int, ratio float64, dl, ul int64) error {
	if ratio < 0 || ratio > 100 || dl < 0 || ul < 0 || dl > 1<<40 || ul > 1<<40 {
		return errors.New("invalid ratio or speed limit")
	}
	if len(selected) == 0 {
		return errors.New("select at least one file")
	}
	var total int64
	seen := map[int]bool{}
	for _, i := range selected {
		if i < 0 || i >= len(t.Files) {
			return errors.New("invalid file selection")
		}
		if !seen[i] {
			total += t.Files[i].Length
			seen[i] = true
		}
	}
	s, e := os.Stat(destination)
	if e != nil || !s.IsDir() {
		return errors.New("choose an existing destination folder")
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errors.New("application is closing")
	}
	if m.job != nil {
		m.mu.Unlock()
		return ErrBusy
	}
	m.meta = t
	m.job = &Job{ID: t.ID(), Name: t.Name, Destination: destination, Selected: append([]int(nil), selected...), Files: t.Files, Trackers: t.Trackers, SeedRatio: ratio, DownloadLimit: dl, UploadLimit: ul, Progress: p2p.Progress{State: "paused", Total: total}}
	m.revision++
	m.mu.Unlock()
	if e = m.save(); e != nil {
		m.mu.Lock()
		m.job.Progress.State = "error"
		m.job.Progress.Message = "Cannot save job: " + e.Error()
		m.revision++
		m.mu.Unlock()
		m.notify()
		return e
	}
	m.notify()
	return m.Start(false)
}
func (m *Manager) Start(checkOnly bool) error {
	m.mu.Lock()
	if m.closed || m.job == nil {
		m.mu.Unlock()
		return errors.New("no available job")
	}
	if m.cancel != nil || unfinished(m.done) {
		m.mu.Unlock()
		return errors.New("wait for the current operation to finish")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	m.cancel = cancel
	m.done = done
	j := m.snapshotLocked().Job
	options := p2p.RunOptions{Metadata: m.meta, Destination: j.Destination, Selected: j.Selected, SeedRatio: j.SeedRatio, DownloadLimit: j.DownloadLimit, UploadLimit: j.UploadLimit, CheckOnly: checkOnly, InitialUploaded: j.Progress.Uploaded}
	m.job.Progress.State = "checking"
	m.job.Progress.Message = "Verifying existing pieces"
	m.revision++
	m.mu.Unlock()
	m.notify()
	go func() {
		defer close(done)
		e := m.runner.Run(ctx, options, func(p p2p.Progress) {
			m.mu.Lock()
			if m.job != nil && m.job.Progress.State != "pausing" {
				m.job.Progress = p
				m.revision++
			}
			m.mu.Unlock()
			m.notify()
		})
		m.mu.Lock()
		if m.job != nil {
			state := "completed"
			message := "Download verified"
			if ctx.Err() != nil || checkOnly {
				state = "paused"
				message = "Ready to resume"
			} else if e != nil {
				state = "error"
				message = e.Error()
			}
			m.job.Progress.State = state
			m.job.Progress.Message = message
			m.job.Progress.DownloadSpeed = 0
			m.job.Progress.UploadSpeed = 0
			m.job.Progress.Peers = 0
			m.revision++
		}
		m.cancel = nil
		m.mu.Unlock()
		if err := m.save(); err != nil {
			m.mu.Lock()
			if m.job != nil {
				m.job.Progress.State = "error"
				m.job.Progress.Message = "Cannot save job: " + err.Error()
				m.revision++
			}
			m.mu.Unlock()
		}
		m.notify()
	}()
	return nil
}
func (m *Manager) Pause() error {
	m.mu.Lock()
	if m.job == nil {
		m.mu.Unlock()
		return errors.New("no job")
	}
	if m.cancel != nil {
		m.job.Progress.State = "pausing"
		m.job.Progress.Message = "Stopping peers and saving verified pieces"
		m.cancel()
		m.revision++
	}
	m.mu.Unlock()
	m.notify()
	return nil
}
func (m *Manager) Remove() error {
	m.mu.Lock()
	if m.cancel != nil || unfinished(m.done) {
		m.mu.Unlock()
		return errors.New("pause the torrent and wait before removing it")
	}
	m.job = nil
	m.meta = p2p.TorrentFile{}
	m.revision++
	m.mu.Unlock()
	e := m.save()
	m.notify()
	return e
}
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	cancel, done := m.cancel, m.done
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	m.save()
}
