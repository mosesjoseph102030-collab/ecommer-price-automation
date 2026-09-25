package billing

import (
	"context"
	"log/slog"
	"time"
)

// RunDunningWorker drives the subscription lifecycle on a timer.
//
// It is the safety net behind read-only mode. Without it, a tenant whose
// renewal failed would keep publishing forever, because nothing in the request
// path flips the flag: the request path only *reads* the state.
//
// The loop is intentionally idempotent. Running it twice changes nothing the
// second time, so a restart mid-cycle cannot double-charge or double-expire.
func (s *Service) RunDunningWorker(ctx context.Context, logger *slog.Logger, every time.Duration) {
	if every <= 0 {
		every = 15 * time.Minute
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	logger.Info("billing_dunning_worker_started", "interval", every.String())
	for {
		select {
		case <-ctx.Done():
			logger.Info("billing_dunning_worker_stopped")
			return
		case <-ticker.C:
			marked, err := s.RunDunning(ctx)
			if err != nil {
				if ctx.Err() != nil {
					logger.Info("billing_dunning_worker_stopped")
					return
				}
				logger.Error("billing_dunning_run_failed", "error", err)
				continue
			}
			if marked > 0 {
				logger.Info("billing_dunning_applied", "subscriptions", marked)
			}
		}
	}
}
