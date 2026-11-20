// Package queue schedules torrent workers and owns their persistent lifecycle.
package queue

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"bitGorrent/internal/p2p"
)

var (
	ErrDuplicate = errors.New("this torrent is already in the transfer list")
	ErrClosed    = errors.New("the download manager is closing")
	ErrNotFound  = errors.New("torrent not found")
)

type Runner interface {
	Run(context.Context, p2p.RunOptions, func(p2p.Progress)) error
}
type Limits struct {
	Downloads int `json:"downloads"`
	Seeds     int `json:"seeds"`
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
	Intent        string       `json:"intent"`
	Private       bool         `json:"private"`
	Progress      p2p.Progress `json:"progress"`
}
type Snapshot struct {
	Revision uint64   `json:"revision"`
	Jobs     []Job    `json:"jobs"`
	Order    []string `json:"order"`
	Limits   Limits   `json:"limits"`
	Error    string   `json:"error"`
}
type entry struct {
	job         Job
	meta        p2p.TorrentFile
	run         *worker
	removing    bool
	removalDone chan struct{}
}
type worker struct {
	generation   uint64
	mode         p2p.RunMode
	ctx          context.Context
	cancel       context.CancelFunc
	exited, done chan struct{}
	stopping     bool
}
type Manager struct {
	// commands serializes durable transitions, never waits for an engine worker.
	commands             sync.Mutex
	mu                   sync.Mutex
	runner               Runner
	emit                 func(Snapshot)
	path                 string
	entries              map[string]*entry
	order                []string
	limits               Limits
	revision, generation uint64
	problem              string
	closing              bool
	closeOnce            sync.Once
	closeErr             error
	writeState           func(string, []byte) error
}

func cloneJob(j Job) Job {
	j.Selected = append([]int{}, j.Selected...)
	j.Files = append([]p2p.File{}, j.Files...)
	j.Trackers = append([]string{}, j.Trackers...)
	j.Progress.FileCompleted = append([]int64{}, j.Progress.FileCompleted...)
	return j
}
func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Snapshot{Revision: m.revision, Jobs: []Job{}, Order: append([]string{}, m.order...), Limits: m.limits, Error: m.problem}
	for _, id := range m.order {
		if e := m.entries[id]; e != nil {
			s.Jobs = append(s.Jobs, cloneJob(e.job))
		}
	}
	return s
}
func (m *Manager) notify() {
	if m.emit != nil {
		m.emit(m.Snapshot())
	}
}
func stoppedProgress(p p2p.Progress) p2p.Progress {
	p.DownloadSpeed = 0
	p.UploadSpeed = 0
	p.Peers = 0
	return p
}
func validLimits(l Limits) bool {
	return l.Downloads >= 0 && l.Downloads <= 16 && l.Seeds >= 0 && l.Seeds <= 16
}

// change serializes a command, commits it before scheduling, and leaves the
// current queue intact if persistence fails. Progress may continue during I/O.
func (m *Manager) change(edit func(*diskState) error) error {
	m.commands.Lock()
	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		m.commands.Unlock()
		return ErrClosed
	}
	state := m.diskLocked()
	m.mu.Unlock()
	err := edit(&state)
	if err == nil {
		err = m.commit(state)
	}
	if err == nil {
		m.schedule()
	}
	m.commands.Unlock()
	m.notify()
	return err
}
func record(state *diskState, id string) (*savedJob, error) {
	for i := range state.Jobs {
		if state.Jobs[i].Job.ID == id {
			return &state.Jobs[i], nil
		}
	}
	return nil, ErrNotFound
}
func moveLast(order []string, id string) []string {
	out := make([]string, 0, len(order))
	for _, v := range order {
		if v != id {
			out = append(out, v)
		}
	}
	return append(out, id)
}

func (m *Manager) Add(t p2p.TorrentFile, destination string, selected []int, ratio float64, dl, ul int64) error {
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 || ratio > 100 || dl < 0 || ul < 0 || dl > 1<<40 || ul > 1<<40 {
		return errors.New("invalid ratio or speed limit")
	}
	parsed, err := p2p.Parse(p2p.MetadataFile(t.RawInfo, t.Trackers))
	if err != nil {
		return err
	}
	parsed.Trackers = append([]string(nil), t.Trackers...)
	t = parsed
	destination, err = filepath.Abs(destination)
	if err != nil {
		return err
	}
	info, err := os.Stat(destination)
	if err != nil || !info.IsDir() {
		return errors.New("choose an existing destination folder")
	}
	if len(selected) == 0 {
		return errors.New("select at least one file")
	}
	indices := []int{}
	seen := map[int]bool{}
	var total int64
	for _, i := range selected {
		if i < 0 || i >= len(t.Files) {
			return errors.New("invalid file selection")
		}
		if !seen[i] {
			seen[i] = true
			indices = append(indices, i)
			total += t.Files[i].Length
		}
	}
	j := Job{ID: t.ID(), Name: t.Name, Destination: destination, Selected: indices, Files: t.Files, Trackers: t.Trackers, SeedRatio: ratio, DownloadLimit: dl, UploadLimit: ul, Intent: string(p2p.ModeDownload), Private: t.Private, Progress: p2p.Progress{State: "queued", Total: total, Message: "Waiting for a download slot"}}
	return m.change(func(s *diskState) error {
		if _, err := record(s, j.ID); err == nil {
			return ErrDuplicate
		}
		for _, saved := range s.Jobs {
			for _, a := range saved.Job.Selected {
				old := strings.ToLower(filepath.Join(saved.Job.Destination, saved.Job.Files[a].Path))
				for _, b := range j.Selected {
					next := strings.ToLower(filepath.Join(j.Destination, j.Files[b].Path))
					sep := string(os.PathSeparator)
					if old == next || strings.HasPrefix(old, next+sep) || strings.HasPrefix(next, old+sep) {
						return fmt.Errorf("destination overlaps another transfer: %s", j.Files[b].Path)
					}
				}
			}
		}
		s.Jobs = append(s.Jobs, savedJob{Job: j, Info: append([]byte(nil), t.RawInfo...)})
		s.Order = append(s.Order, j.ID)
		return nil
	})
}

func (m *Manager) Resume(id string) error  { return m.enqueue(id, p2p.ModeDownload) }
func (m *Manager) Recheck(id string) error { return m.enqueue(id, p2p.ModeCheck) }
func (m *Manager) enqueue(id string, mode p2p.RunMode) error {
	return m.change(func(s *diskState) error {
		r, err := record(s, id)
		if err != nil {
			return err
		}
		m.mu.Lock()
		e := m.entries[id]
		removing := e.removing
		active := e.run != nil
		stopping := active && e.run.stopping
		m.mu.Unlock()
		if removing {
			return errors.New("torrent is being removed")
		}
		if active && !stopping {
			if mode == p2p.ModeCheck {
				return errors.New("pause the torrent before verifying files")
			}
			return nil
		}
		r.Job.Intent = string(mode)
		r.Job.Progress = stoppedProgress(r.Job.Progress)
		if active {
			r.Job.Progress.State = "pausing"
			r.Job.Progress.Message = "Waiting for the previous worker to stop"
		} else {
			r.Job.Progress.State = "queued"
			r.Job.Progress.Message = "Waiting for a download slot"
		}
		s.Order = moveLast(s.Order, id)
		return nil
	})
}
func (m *Manager) Pause(id string) error {
	m.commands.Lock()
	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		m.commands.Unlock()
		return ErrClosed
	}
	e := m.entries[id]
	if e == nil {
		m.mu.Unlock()
		m.commands.Unlock()
		return ErrNotFound
	}
	if e.removing {
		m.mu.Unlock()
		m.commands.Unlock()
		return errors.New("torrent is being removed")
	}
	state := m.diskLocked()
	run := e.run
	m.mu.Unlock()
	r, _ := record(&state, id)
	r.Job.Intent = "paused"
	r.Job.Progress = stoppedProgress(r.Job.Progress)
	r.Job.Progress.State = "paused"
	r.Job.Progress.Message = "Ready to resume"
	if run != nil {
		r.Job.Progress.State = "pausing"
		r.Job.Progress.Message = "Stopping peers and saving verified pieces"
	}
	err := m.commit(state)
	if err == nil {
		if run != nil {
			m.mu.Lock()
			run.stopping = true
			m.mu.Unlock()
			run.cancel()
		}
		m.schedule()
	}
	m.commands.Unlock()
	m.notify()
	return err
}

// Remove waits outside all manager locks. The entry stays reserved until its
// worker exits, preventing a re-add from opening the same files concurrently.
func (m *Manager) Remove(id string) error {
	m.commands.Lock()
	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		m.commands.Unlock()
		return ErrClosed
	}
	e := m.entries[id]
	if e == nil {
		m.mu.Unlock()
		m.commands.Unlock()
		return ErrNotFound
	}
	if e.removing {
		m.mu.Unlock()
		m.commands.Unlock()
		return errors.New("torrent is already being removed")
	}
	state := m.diskLocked()
	run := e.run
	m.mu.Unlock()
	r, _ := record(&state, id)
	r.Job.Intent = "paused"
	r.Job.Progress = stoppedProgress(r.Job.Progress)
	r.Job.Progress.State = "removing"
	r.Job.Progress.Message = "Stopping the transfer; files will be kept"
	if err := m.commit(state); err != nil {
		m.commands.Unlock()
		m.notify()
		return err
	}
	m.mu.Lock()
	e.removing = true
	e.removalDone = make(chan struct{})
	removalDone := e.removalDone
	if run != nil {
		run.stopping = true
	}
	m.mu.Unlock()
	m.commands.Unlock()
	defer close(removalDone)
	m.notify()
	if run != nil {
		run.cancel()
		<-run.exited
	}
	m.commands.Lock()
	m.mu.Lock()
	state = m.diskLocked()
	m.mu.Unlock()
	jobs := state.Jobs[:0]
	for _, j := range state.Jobs {
		if j.Job.ID != id {
			jobs = append(jobs, j)
		}
	}
	state.Jobs = jobs
	order := []string{}
	for _, v := range state.Order {
		if v != id {
			order = append(order, v)
		}
	}
	state.Order = order
	err := m.commit(state)
	if err != nil {
		m.mu.Lock()
		e.removing = false
		e.run = nil
		e.job.Intent = "paused"
		e.job.Progress.State = "error"
		e.job.Progress.Message = "Could not remove transfer: " + err.Error()
		m.revision++
		m.mu.Unlock()
	} else {
		m.schedule()
	}
	m.commands.Unlock()
	m.notify()
	return err
}

func (m *Manager) SetLimits(downloads, seeds int) error {
	l := Limits{downloads, seeds}
	if !validLimits(l) {
		return errors.New("slot limits must be between 0 and 16")
	}
	return m.change(func(s *diskState) error { s.Limits = l; return nil })
}
func (m *Manager) MoveQueued(id string, direction int) error {
	if direction != -1 && direction != 1 {
		return errors.New("queue move must be up or down")
	}
	return m.change(func(s *diskState) error {
		r, err := record(s, id)
		if err != nil {
			return err
		}
		if r.Job.Progress.State != "queued" {
			return errors.New("only queued transfers can be reordered")
		}
		positions := []int{}
		current := -1
		for i, key := range s.Order {
			v, _ := record(s, key)
			if v.Job.Progress.State == "queued" {
				if key == id {
					current = len(positions)
				}
				positions = append(positions, i)
			}
		}
		next := current + direction
		if current < 0 || next < 0 || next >= len(positions) {
			return errors.New("transfer is already at the end of the queue")
		}
		a, b := positions[current], positions[next]
		s.Order[a], s.Order[b] = s.Order[b], s.Order[a]
		return nil
	})
}

func (m *Manager) Close() error {
	m.closeOnce.Do(func() {
		m.commands.Lock()
		m.mu.Lock()
		m.closing = true
		runs := []*worker{}
		removals := []chan struct{}{}
		for _, e := range m.entries {
			if e.run != nil {
				e.run.stopping = true
				runs = append(runs, e.run)
			}
			if e.removing {
				removals = append(removals, e.removalDone)
			}
		}
		m.mu.Unlock()
		m.commands.Unlock()
		for _, r := range runs {
			r.cancel()
		}
		for _, r := range runs {
			<-r.done
		}
		for _, done := range removals {
			<-done
		}
		m.commands.Lock()
		m.mu.Lock()
		s := m.diskLocked()
		m.mu.Unlock()
		m.closeErr = m.commit(s)
		m.commands.Unlock()
	})
	return m.closeErr
}
