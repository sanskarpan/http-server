package pool

import (
	"context"
	"log"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

// Job represents a unit of work
type Job struct {
	Task func()
}

// WorkerPool manages a pool of worker goroutines
type WorkerPool struct {
	maxWorkers    int
	jobQueue      chan Job
	wg            sync.WaitGroup
	ctx           context.Context
	cancel        context.CancelFunc
	mu            sync.RWMutex
	shutdownOnce  sync.Once
	activeWorkers atomic.Int32
	totalJobs     atomic.Int64
	completed     atomic.Int64
	closed        atomic.Bool
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(maxWorkers, queueSize int) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())

	pool := &WorkerPool{
		maxWorkers: maxWorkers,
		jobQueue:   make(chan Job, queueSize),
		ctx:        ctx,
		cancel:     cancel,
	}

	pool.activeWorkers.Store(0)
	pool.totalJobs.Store(0)
	pool.completed.Store(0)

	return pool
}

// Start starts the worker pool
func (p *WorkerPool) Start() {
	for i := 0; i < p.maxWorkers; i++ {
		p.wg.Add(1)
		go p.worker()
	}
}

// worker is the worker goroutine
func (p *WorkerPool) worker() {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		case job, ok := <-p.jobQueue:
			if !ok {
				return
			}

			p.activeWorkers.Add(1)
			func() {
				defer p.activeWorkers.Add(-1)
				defer p.completed.Add(1)
				defer func() {
					if recovered := recover(); recovered != nil {
						log.Printf("worker pool task panic: %v\n%s", recovered, debug.Stack())
					}
				}()
				job.Task()
			}()
		}
	}
}

// Submit submits a job to the pool
func (p *WorkerPool) Submit(task func()) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed.Load() {
		return false
	}

	select {
	case p.jobQueue <- Job{Task: task}:
		p.totalJobs.Add(1)
		return true
	default:
		// Queue is full
		return false
	}
}

// Shutdown gracefully shuts down the worker pool
func (p *WorkerPool) Shutdown() {
	p.shutdownOnce.Do(func() {
		p.mu.Lock()
		p.closed.Store(true)
		close(p.jobQueue)
		p.cancel()
		p.mu.Unlock()
	})
	p.wg.Wait()
}

// ShutdownWithContext shuts down with context
func (p *WorkerPool) ShutdownWithContext(ctx context.Context) error {
	done := make(chan struct{})

	go func() {
		p.Shutdown()
		close(done)
	}()

	select {
	case <-ctx.Done():
		p.cancel()
		return ctx.Err()
	case <-done:
		return nil
	}
}

// Stats returns pool statistics
func (p *WorkerPool) Stats() PoolStats {
	return PoolStats{
		MaxWorkers:    p.maxWorkers,
		ActiveWorkers: int(p.activeWorkers.Load()),
		QueueSize:     len(p.jobQueue),
		QueueCapacity: cap(p.jobQueue),
		TotalJobs:     p.totalJobs.Load(),
		Completed:     p.completed.Load(),
	}
}

// PoolStats contains pool statistics
type PoolStats struct {
	MaxWorkers    int
	ActiveWorkers int
	QueueSize     int
	QueueCapacity int
	TotalJobs     int64
	Completed     int64
}
