package queue

import (
	"context"
	"errors"

	"bitGorrent/internal/p2p"
)

// schedule is called under commands. Slots remain occupied until Run returns,
// including when a worker has received a cancellation request.
func (m *Manager) schedule() {
	m.mu.Lock()
	if m.closing || m.problem != "" {
		m.mu.Unlock()
		return
	}
	downloads, seeds := 0, 0
	for _, e := range m.entries {
		if e.run != nil {
			if e.run.mode == p2p.ModeSeed {
				seeds++
			} else {
				downloads++
			}
		}
	}
	type launch struct {
		id string
		e  *entry
		r  *worker
		o  p2p.RunOptions
	}
	starts := []launch{}
	for _, id := range m.order {
		e := m.entries[id]
		if e.run != nil || e.removing {
			continue
		}
		mode := p2p.RunMode(e.job.Intent)
		if mode == p2p.ModeSeed {
			if e.job.Progress.State != "completed" || seeds >= m.limits.Seeds {
				continue
			}
			seeds++
		} else {
			if (mode != p2p.ModeDownload && mode != p2p.ModeCheck) || e.job.Progress.State != "queued" || downloads >= m.limits.Downloads {
				continue
			}
			downloads++
		}
		ctx, cancel := context.WithCancel(context.Background())
		m.generation++
		r := &worker{generation: m.generation, mode: mode, ctx: ctx, cancel: cancel, exited: make(chan struct{}), done: make(chan struct{})}
		e.run = r
		e.job.Progress.State = "checking"
		e.job.Progress.Message = "Verifying existing pieces"
		m.revision++
		t := e.meta
		t.RawInfo = append([]byte(nil), t.RawInfo...)
		t.Files = append([]p2p.File(nil), t.Files...)
		t.Trackers = append([]string(nil), t.Trackers...)
		t.PieceHashes = append([][20]byte(nil), t.PieceHashes...)
		o := p2p.RunOptions{Metadata: t, Destination: e.job.Destination, Selected: append([]int(nil), e.job.Selected...), SeedRatio: e.job.SeedRatio, DownloadLimit: e.job.DownloadLimit, UploadLimit: e.job.UploadLimit, InitialUploaded: e.job.Progress.Uploaded, Mode: mode, CheckOnly: mode == p2p.ModeCheck}
		starts = append(starts, launch{id, e, r, o})
	}
	m.mu.Unlock()
	for _, start := range starts {
		go func(start launch) {
			err := m.runner.Run(start.r.ctx, start.o, func(p p2p.Progress) {
				m.mu.Lock()
				current := m.entries[start.id]
				if current != start.e || current.run != start.r || current.run.generation != start.r.generation {
					m.mu.Unlock()
					return
				}
				p.FileCompleted = append([]int64(nil), p.FileCompleted...)
				p.Total = current.job.Progress.Total
				if p.Completed < 0 {
					p.Completed = 0
				}
				if p.Completed > p.Total {
					p.Completed = p.Total
				}
				if p.Uploaded < current.job.Progress.Uploaded {
					p.Uploaded = current.job.Progress.Uploaded
				}
				if start.r.stopping || current.removing || current.job.Progress.State == "pausing" || current.job.Progress.State == "removing" {
					p.State = current.job.Progress.State
					p.Message = current.job.Progress.Message
					p = stoppedProgress(p)
				}
				current.job.Progress = p
				m.revision++
				m.mu.Unlock()
				m.notify()
			})
			start.r.cancel()
			close(start.r.exited)
			m.finish(start.id, start.e, start.r, err)
			close(start.r.done)
			m.notify()
		}(start)
	}
}
func (m *Manager) finish(id string, e *entry, r *worker, runErr error) {
	m.commands.Lock()
	defer m.commands.Unlock()
	m.mu.Lock()
	if m.entries[id] != e || e.run != r {
		m.mu.Unlock()
		return
	}
	e.run = nil
	if e.removing {
		m.revision++
		m.mu.Unlock()
		m.schedule()
		return
	}
	if runErr == nil && !r.stopping && !m.closing && r.mode == p2p.ModeDownload {
		e.job.Progress.Completed = e.job.Progress.Total
		e.job.Progress.FileCompleted = make([]int64, len(e.job.Files))
		for _, i := range e.job.Selected {
			e.job.Progress.FileCompleted[i] = e.job.Files[i].Length
		}
	}
	s := m.diskLocked()
	closing := m.closing
	stopping := r.stopping
	m.mu.Unlock()
	saved, _ := record(&s, id)
	j := &saved.Job
	j.Progress = stoppedProgress(j.Progress)
	switch {
	case runErr != nil && !errors.Is(runErr, context.Canceled):
		j.Intent = "paused"
		j.Progress.State = "error"
		j.Progress.Message = runErr.Error()
	case closing:
		j.Progress.State = "paused"
		j.Progress.Message = "Stopped safely. Resume to continue."
	case stopping:
		if j.Intent == string(p2p.ModeDownload) || j.Intent == string(p2p.ModeCheck) {
			j.Progress.State = "queued"
			j.Progress.Message = "Waiting for a download slot"
		} else {
			j.Progress.State = "paused"
			j.Progress.Message = "Ready to resume"
		}
	case r.mode == p2p.ModeCheck:
		j.Intent = "paused"
		j.Progress.State = "paused"
		j.Progress.Message = "Verification finished. Resume to continue."
	case r.mode == p2p.ModeDownload && j.SeedRatio > 0 && float64(j.Progress.Uploaded) < float64(j.Progress.Total)*j.SeedRatio:
		j.Intent = string(p2p.ModeSeed)
		j.Progress.State = "completed"
		j.Progress.Message = "Download verified. Waiting for a seed slot."
	default:
		j.Intent = "done"
		j.Progress.State = "completed"
		j.Progress.Message = "Download verified"
		if r.mode == p2p.ModeSeed {
			j.Progress.Message = "Seed target reached"
		}
	}
	if err := m.commit(s); err != nil {
		m.mu.Lock()
		e.job.Intent = "paused"
		e.job.Progress = stoppedProgress(e.job.Progress)
		e.job.Progress.State = "error"
		e.job.Progress.Message = "Cannot save queue: " + err.Error()
		m.revision++
		m.mu.Unlock()
		return
	}
	m.schedule()
}
