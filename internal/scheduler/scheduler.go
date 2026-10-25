package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/service"
)

type Scheduler struct {
	cases                                      service.NotificationCases
	now                                        func() time.Time
	logger                                     *slog.Logger
	planInterval, dialogInterval, idleInterval time.Duration
}

func New(cases service.NotificationCases, now func() time.Time, logger *slog.Logger) *Scheduler {
	return &Scheduler{cases, now, logger, 15 * time.Second, time.Minute, time.Second}
}
func (s *Scheduler) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); s.periodic(ctx, s.planInterval, s.cases.Plan) }()
	go func() { defer wg.Done(); s.periodic(ctx, s.dialogInterval, s.cases.ExpireDialogs) }()
	go func() { defer wg.Done(); s.deliver(ctx) }()
	wg.Wait()
	return nil
}
func (s *Scheduler) periodic(ctx context.Context, interval time.Duration, fn func(context.Context, time.Time) error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		s.report(ctx, fn(ctx, s.now()))
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Scheduler) deliver(ctx context.Context) {
	for ctx.Err() == nil {
		worked, e := s.cases.DispatchOne(ctx, s.now())
		s.report(ctx, e)
		if e == nil && worked {
			continue
		}
		timer := time.NewTimer(s.idleInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (s *Scheduler) report(ctx context.Context, e error) {
	if e != nil && !errors.Is(e, context.Canceled) {
		s.logger.ErrorContext(ctx, "Scheduled operation failed", "event", "scheduler_failed", "err", e)
	}
}
