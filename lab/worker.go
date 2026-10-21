package lab

import (
	"context"
	"errors"
	"time"
)

type Worker struct {
	Store          *Store
	Tool           *ToolClient
	Name           string
	Lease          time.Duration
	HeartbeatEvery time.Duration
	Backoff        time.Duration
	Hook           func(string, Workflow)
}

func (w *Worker) RunOne(ctx context.Context) error {
	if w.Backoff < 0 {
		return errors.New("invalid bounded retry configuration")
	}
	job, err := w.Store.Claim(ctx, w.Name, w.Lease)
	if err != nil || job == nil {
		return err
	}
	callCtx, cancel := context.WithCancel(ctx)
	stopped := make(chan struct{})
	if w.HeartbeatEvery > 0 {
		go func() {
			defer close(stopped)
			ticker := time.NewTicker(w.HeartbeatEvery)
			defer ticker.Stop()
			for {
				select {
				case <-callCtx.Done():
					return
				case <-ticker.C:
					if w.Store.Heartbeat(callCtx, *job, w.Lease) != nil {
						cancel()
						return
					}
				}
			}
		}()
	} else {
		close(stopped)
	}
	defer func() { cancel(); <-stopped }()
	if w.Hook != nil {
		w.Hook("after_claim", *job)
	}
	// Identity has already been authenticated by the submitting application;
	// this lab rechecks the durable approval and local dispatch permission.
	if err = w.Store.BeforeCall(callCtx, *job); err != nil {
		cleanup, done := context.WithTimeout(context.Background(), 2*time.Second)
		defer done()
		current, e := w.Store.Get(cleanup, job.ID)
		if e != nil {
			return e
		}
		if current.CancelRequested {
			return w.Store.transition(cleanup, *job, "cancelled", "cancelled_before_dispatch", nil, 0)
		}
		return w.Store.Hold(cleanup, *job)
	}
	receipt, callErr := w.Tool.Call(callCtx, *job)
	if callErr == nil && w.Hook != nil {
		w.Hook("after_tool", *job)
	}
	cancel()
	<-stopped
	cleanup, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()
	if callErr == nil {
		return w.Store.Complete(cleanup, *job, receipt)
	}
	if errors.Is(callErr, ErrRejected) {
		delay := w.Backoff * time.Duration(1<<min(job.Attempt-1, 8))
		return w.Store.Retry(cleanup, *job, delay)
	}
	return w.Store.Unknown(cleanup, *job)
}
func (w *Worker) Reconcile(ctx context.Context, id string) error {
	job, err := w.Store.Get(ctx, id)
	if err != nil {
		return err
	}
	if job.State != "unknown" {
		return ErrStale
	}
	if !job.Plan.Idempotent {
		return ErrManual
	}
	receipt, err := w.Tool.Query(ctx, job)
	if err != nil {
		return err
	}
	return w.Store.Reconciled(ctx, job, receipt, receipt == nil)
}
