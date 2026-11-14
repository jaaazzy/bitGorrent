package main

import (
	"bitGorrent/internal/p2p"
	"bitGorrent/internal/queue"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx                context.Context
	mu                 sync.Mutex
	manager            *queue.Manager
	startupError       error
	preview            *p2p.TorrentFile
	metadataCancel     context.CancelFunc
	metadataGeneration uint64
}

func NewApp() *App { return &App{} }
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	dir := os.Getenv("BITGORRENT_CONFIG_DIR")
	if dir == "" {
		base, e := os.UserConfigDir()
		if e != nil {
			a.startupError = e
			return
		}
		dir = filepath.Join(base, "bitGorrent")
	}
	var e error
	a.manager, e = queue.New(p2p.Engine{}, filepath.Join(dir, "job.json"), func(s queue.Snapshot) { runtime.EventsEmit(ctx, "transfers:changed", s) })
	a.startupError = e
}
func (a *App) shutdown(ctx context.Context) {
	a.CancelMetadata()
	if a.manager != nil {
		a.manager.Close()
	}
}
func (a *App) ready() error {
	if a.startupError != nil {
		return a.startupError
	}
	if a.manager == nil {
		return errors.New("download manager is not ready")
	}
	return nil
}
func (a *App) GetSnapshot() (queue.Snapshot, error) {
	if e := a.ready(); e != nil {
		return queue.Snapshot{}, e
	}
	return a.manager.Snapshot(), nil
}

type Preview struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Length   int64      `json:"length"`
	Files    []p2p.File `json:"files"`
	Trackers []string   `json:"trackers"`
	Private  bool       `json:"private"`
}

func (a *App) InspectTorrent(source string) (Preview, error) {
	a.mu.Lock()
	if a.metadataCancel != nil {
		a.metadataCancel()
	}
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
	a.metadataCancel = cancel
	a.metadataGeneration++
	generation := a.metadataGeneration
	a.preview = nil
	a.mu.Unlock()
	defer cancel()
	var t p2p.TorrentFile
	var e error
	if strings.HasPrefix(strings.TrimSpace(source), "magnet:") {
		t, e = p2p.ResolveMagnet(ctx, strings.TrimSpace(source), func(s string) { runtime.EventsEmit(a.ctx, "metadata:status", s) })
	} else {
		t, e = p2p.Open(source)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if generation != a.metadataGeneration {
		return Preview{}, context.Canceled
	}
	a.metadataCancel = nil
	if e != nil {
		return Preview{}, e
	}
	a.preview = &t
	return Preview{t.ID(), t.Name, t.Length, t.Files, t.Trackers, t.Private}, nil
}
func (a *App) CancelMetadata() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.metadataCancel != nil {
		a.metadataCancel()
		a.metadataCancel = nil
	}
	a.metadataGeneration++
	a.preview = nil
}
func (a *App) AddTorrent(id, destination string, selected []int, seedRatio float64, downloadLimit, uploadLimit int64) error {
	if e := a.ready(); e != nil {
		return e
	}
	a.mu.Lock()
	if a.preview == nil || a.preview.ID() != id {
		a.mu.Unlock()
		return errors.New("inspect the torrent again before adding it")
	}
	t := *a.preview
	a.mu.Unlock()
	return a.manager.Add(t, destination, selected, seedRatio, downloadLimit, uploadLimit)
}
func (a *App) PauseTorrent() error {
	if e := a.ready(); e != nil {
		return e
	}
	return a.manager.Pause()
}
func (a *App) ResumeTorrent() error {
	if e := a.ready(); e != nil {
		return e
	}
	return a.manager.Start(false)
}
func (a *App) RecheckTorrent() error {
	if e := a.ready(); e != nil {
		return e
	}
	return a.manager.Start(true)
}
func (a *App) RemoveTorrent() error {
	if e := a.ready(); e != nil {
		return e
	}
	return a.manager.Remove()
}
func (a *App) RevealDestination() error {
	if e := a.ready(); e != nil {
		return e
	}
	s := a.manager.Snapshot()
	if s.Job == nil {
		return errors.New("no torrent selected")
	}
	return exec.Command("explorer.exe", s.Job.Destination).Start()
}
func (a *App) SelectTorrentFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Add a torrent", Filters: []runtime.FileFilter{{DisplayName: "Torrent files (*.torrent)", Pattern: "*.torrent"}}})
}
func (a *App) SelectFolder() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Download to"})
}
