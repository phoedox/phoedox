// Package jobs runs background work stored in the jobs table: file
// previews, text extraction and later webhooks, backups and imports.
// Workers run in the server process (several instances may share the
// database: Postgres hands each job to one worker via SKIP LOCKED).
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/store"
)

// Handler does one job. Returning Permanent(err) stops retries.
type Handler func(ctx context.Context, payload json.RawMessage) error

type permanent struct{ error }

// Permanent marks an error that retrying will not fix.
func Permanent(err error) error { return permanent{err} }

// Queue stores and runs jobs.
type Queue struct {
	DB          *store.DB
	Workers     int           // parallel jobs per process (default 2)
	Lease       time.Duration // how long a job may run before another worker takes it (default 10 min)
	MaxAttempts int           // default 5
	Poll        time.Duration // idle polling interval (default 5 s)

	mu       sync.Mutex
	handlers map[string]Handler
	kick     chan struct{}
}

// Handle registers the handler for a kind of job.
func (q *Queue) Handle(kind string, h Handler) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.handlers == nil {
		q.handlers = map[string]Handler{}
	}
	q.handlers[kind] = h
}

func (q *Queue) wake() chan struct{} {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.kick == nil {
		q.kick = make(chan struct{}, 1)
	}
	return q.kick
}

// Add enqueues a job and wakes a worker.
func (q *Queue) Add(ctx context.Context, kind string, payload any) error {
	return q.AddAt(ctx, kind, payload, time.Time{})
}

// AddAt enqueues a job for a later time.
func (q *Queue) AddAt(ctx context.Context, kind string, payload any, at time.Time) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var runAt store.Time
	if !at.IsZero() {
		runAt = store.Time{Time: at.UTC()}
	}
	if err := q.DB.Enqueue(ctx, string(graph.NewID()), kind, b, runAt); err != nil {
		return err
	}
	select {
	case q.wake() <- struct{}{}:
	default:
	}
	return nil
}

func (q *Queue) defaults() {
	if q.Workers <= 0 {
		q.Workers = 2
	}
	if q.Lease <= 0 {
		q.Lease = 10 * time.Minute
	}
	if q.MaxAttempts <= 0 {
		q.MaxAttempts = 5
	}
	if q.Poll <= 0 {
		q.Poll = 5 * time.Second
	}
}

// Run processes jobs until ctx ends.
func (q *Queue) Run(ctx context.Context) {
	q.defaults()
	var wg sync.WaitGroup
	for range q.Workers {
		wg.Go(func() { q.loop(ctx) })
	}
	wg.Wait()
}

func (q *Queue) loop(ctx context.Context) {
	kick := q.wake()
	for ctx.Err() == nil {
		if q.RunOne(ctx) {
			continue
		}
		select {
		case <-ctx.Done():
		case <-kick:
		case <-time.After(q.Poll):
		}
	}
}

// RunOne claims and runs one due job; false if there was none.
func (q *Queue) RunOne(ctx context.Context) bool {
	q.defaults()
	j, err := q.DB.ClaimJob(ctx, q.Lease)
	if errors.Is(err, store.ErrNotFound) {
		return false
	}
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("claiming a job failed", "err", err)
		}
		return false
	}
	q.mu.Lock()
	h := q.handlers[j.Kind]
	q.mu.Unlock()
	var jobErr error
	if h == nil {
		jobErr = Permanent(fmt.Errorf("no handler for job kind %q", j.Kind))
	} else {
		jctx, cancel := context.WithTimeout(ctx, q.Lease)
		jobErr = safeRun(jctx, h, json.RawMessage(j.Payload))
		cancel()
	}
	var retry *store.Time
	var perm permanent
	if jobErr != nil && !errors.As(jobErr, &perm) && j.Attempts < q.MaxAttempts {
		t := store.Now()
		t.Time = t.Add(time.Duration(1<<j.Attempts) * 10 * time.Second)
		retry = &t
	}
	if jobErr != nil {
		slog.Warn("job failed", "kind", j.Kind, "id", j.ID, "attempt", j.Attempts, "retry", retry != nil, "err", jobErr)
	}
	if err := q.DB.FinishJob(context.WithoutCancel(ctx), j.ID, jobErr, retry); err != nil {
		slog.Warn("finishing a job failed", "err", err)
	}
	return true
}

func safeRun(ctx context.Context, h Handler, payload json.RawMessage) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = Permanent(fmt.Errorf("panic: %v", r))
		}
	}()
	return h(ctx, payload)
}
