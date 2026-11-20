package queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"bitGorrent/internal/p2p"
)

const stateVersion = 1
const maxStateSize = 128 << 20

type savedJob struct {
	Job  Job    `json:"job"`
	Info []byte `json:"info"`
}
type diskState struct {
	Version int        `json:"version"`
	Jobs    []savedJob `json:"jobs"`
	Order   []string   `json:"order"`
	Limits  Limits     `json:"limits"`
}

func New(r Runner, path string, emit func(Snapshot)) (*Manager, error) {
	m := &Manager{runner: r, path: path, emit: emit, entries: map[string]*entry{}, order: []string{}, limits: Limits{2, 1}, writeState: writeAtomic}
	if r == nil {
		return nil, errors.New("a torrent runner is required")
	}
	if path == "" {
		return m, nil
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxStateSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxStateSize {
		return nil, errors.New("saved queue is too large")
	}
	var header struct {
		Version *int   `json:"version"`
		Job     *Job   `json:"job"`
		Info    []byte `json:"info"`
	}
	if err = json.Unmarshal(data, &header); err != nil {
		return nil, fmt.Errorf("cannot read saved queue; original file preserved: %w", err)
	}
	var state diskState
	if header.Version == nil {
		if header.Job == nil || len(header.Info) == 0 {
			return nil, errors.New("unrecognized saved queue; original file preserved")
		}
		state = diskState{Version: stateVersion, Jobs: []savedJob{{Job: *header.Job, Info: header.Info}}, Order: []string{header.Job.ID}, Limits: m.limits}
	} else {
		if *header.Version != stateVersion {
			return nil, errors.New("unsupported queue version; original file preserved")
		}
		if err = json.Unmarshal(data, &state); err != nil {
			return nil, err
		}
	}
	metas, err := validateState(state)
	if err != nil {
		return nil, fmt.Errorf("invalid saved queue; original file preserved: %w", err)
	}
	m.limits = state.Limits
	m.order = append([]string{}, state.Order...)
	for _, saved := range state.Jobs {
		j := cloneJob(saved.Job)
		meta := metas[j.ID]
		j.Name = meta.Name
		j.Files = append([]p2p.File{}, meta.Files...)
		j.Private = meta.Private
		j.Progress = stoppedProgress(j.Progress)
		j.Progress.State = "paused"
		j.Progress.Message = "Restored safely. Resume to verify existing pieces."
		m.entries[j.ID] = &entry{job: j, meta: meta}
	}
	// Loading is read-only, including migration. The next successful command
	// writes the versioned document; corrupt/unknown state is never replaced.
	return m, nil
}
func validateState(s diskState) (map[string]p2p.TorrentFile, error) {
	if !validLimits(s.Limits) || s.Version != stateVersion || len(s.Jobs) > 1000 || len(s.Jobs) != len(s.Order) {
		return nil, errors.New("invalid queue settings or order")
	}
	metas := map[string]p2p.TorrentFile{}
	for _, saved := range s.Jobs {
		j := saved.Job
		if _, exists := metas[j.ID]; exists {
			return nil, ErrDuplicate
		}
		meta, err := p2p.Parse(p2p.MetadataFile(saved.Info, j.Trackers))
		if err != nil {
			return nil, err
		}
		meta.Trackers = append([]string{}, j.Trackers...)
		if meta.ID() != j.ID {
			return nil, errors.New("saved job hash does not match metadata")
		}
		if !filepath.IsAbs(j.Destination) || len(j.Selected) == 0 {
			return nil, errors.New("invalid saved destination or selection")
		}
		var total int64
		seen := map[int]bool{}
		for _, i := range j.Selected {
			if i < 0 || i >= len(meta.Files) || seen[i] {
				return nil, errors.New("invalid saved file selection")
			}
			seen[i] = true
			total += meta.Files[i].Length
		}
		if j.Progress.Total != total || j.Progress.Completed < 0 || j.Progress.Completed > total || j.Progress.Uploaded < 0 {
			return nil, errors.New("invalid saved progress")
		}
		if j.SeedRatio < 0 || j.SeedRatio > 100 || j.DownloadLimit < 0 || j.UploadLimit < 0 {
			return nil, errors.New("invalid saved transfer limits")
		}
		switch j.Intent {
		case "", "paused", "done", string(p2p.ModeDownload), string(p2p.ModeSeed), string(p2p.ModeCheck):
		default:
			return nil, errors.New("invalid saved intent")
		}
		metas[j.ID] = meta
	}
	seen := map[string]bool{}
	for _, id := range s.Order {
		if _, ok := metas[id]; !ok || seen[id] {
			return nil, errors.New("invalid queue order")
		}
		seen[id] = true
	}
	return metas, nil
}
func (m *Manager) diskLocked() diskState {
	s := diskState{Version: stateVersion, Jobs: []savedJob{}, Order: append([]string{}, m.order...), Limits: m.limits}
	for _, id := range m.order {
		e := m.entries[id]
		s.Jobs = append(s.Jobs, savedJob{cloneJob(e.job), append([]byte(nil), e.meta.RawInfo...)})
	}
	return s
}
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "queue-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	return os.Rename(tmp, path)
}

// commit is called under commands, with no state lock held during filesystem I/O.
func (m *Manager) commit(s diskState) error {
	metas, err := validateState(s)
	if err == nil && m.path != "" {
		var data []byte
		data, err = json.Marshal(s)
		if err == nil {
			if len(data) > maxStateSize {
				err = errors.New("saved queue is too large")
			} else {
				err = m.writeState(m.path, data)
			}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.problem = "Cannot save queue: " + err.Error()
		m.revision++
		return err
	}
	next := make(map[string]*entry, len(s.Jobs))
	for _, saved := range s.Jobs {
		j := cloneJob(saved.Job)
		e := m.entries[j.ID]
		if e == nil {
			e = &entry{meta: metas[j.ID]}
		} else {
			// Worker reports may arrive while saving. Preserve their counters while
			// applying the command's state and intent to the existing entry identity.
			if e.run != nil {
				state, message := j.Progress.State, j.Progress.Message
				if j.Intent == e.job.Intent && state != "pausing" && state != "removing" {
					state = e.job.Progress.State
					message = e.job.Progress.Message
				}
				j.Progress = e.job.Progress
				j.Progress.State = state
				j.Progress.Message = message
				j.Progress.FileCompleted = append([]int64{}, j.Progress.FileCompleted...)
			}
			if j.Progress.State != "checking" && j.Progress.State != "downloading" && j.Progress.State != "waiting" && j.Progress.State != "seeding" {
				j.Progress = stoppedProgress(j.Progress)
			}
		}
		e.job = j
		next[j.ID] = e
	}
	m.entries = next
	m.order = append([]string{}, s.Order...)
	m.limits = s.Limits
	m.problem = ""
	m.revision++
	return nil
}
