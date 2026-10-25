package scheduler

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

type fakeCases struct{ plans, dialogs, deliveries atomic.Int64 }

func (f *fakeCases) Plan(context.Context, time.Time) error          { f.plans.Add(1); return nil }
func (f *fakeCases) ExpireDialogs(context.Context, time.Time) error { f.dialogs.Add(1); return nil }
func (f *fakeCases) DispatchOne(context.Context, time.Time) (bool, error) {
	f.deliveries.Add(1)
	return false, nil
}
func TestScheduler_StartAndGracefulShutdown(t *testing.T) {
	f := &fakeCases{}
	s := New(f, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.planInterval = 5 * time.Millisecond
	s.dialogInterval = 5 * time.Millisecond
	s.idleInterval = time.Millisecond
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if e := s.Run(ctx); e != nil {
		t.Fatal(e)
	}
	if f.plans.Load() < 1 || f.dialogs.Load() < 1 || f.deliveries.Load() < 1 {
		t.Fatal("scheduler did not run all jobs at startup")
	}
}
