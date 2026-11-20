// Package queuetest supplies a controllable engine for lifecycle tests.
// Tests can observe starts, send progress, finish, or wait for cancellation.
package queuetest

import (
	"bitGorrent/internal/p2p"
	"context"
)

type Run struct {
	Options   p2p.RunOptions
	Progress  chan p2p.Progress
	Finish    chan error
	Stopped   chan struct{}
	Cancelled chan struct{}
	Report    func(p2p.Progress)
}
type Fake struct {
	Started     chan *Run
	AfterCancel <-chan struct{}
}

func New() *Fake { return &Fake{Started: make(chan *Run, 64)} }
func (f *Fake) Run(ctx context.Context, o p2p.RunOptions, report func(p2p.Progress)) error {
	run := &Run{Options: o, Progress: make(chan p2p.Progress), Finish: make(chan error), Stopped: make(chan struct{}), Cancelled: make(chan struct{}), Report: report}
	defer close(run.Stopped)
	select {
	case f.Started <- run:
	case <-ctx.Done():
		return ctx.Err()
	}
	for {
		select {
		case <-ctx.Done():
			close(run.Cancelled)
			if f.AfterCancel != nil {
				<-f.AfterCancel
			}
			return ctx.Err()
		case p := <-run.Progress:
			report(p)
		case e := <-run.Finish:
			return e
		}
	}
}
