package pool

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPoolRecoversFromTaskPanic(t *testing.T) {
	pool := NewWorkerPool(1, 2)
	pool.Start()
	defer pool.Shutdown()

	var executed atomic.Bool
	done := make(chan struct{})

	if !pool.Submit(func() {
		panic("boom")
	}) {
		t.Fatal("failed to submit panic task")
	}

	if !pool.Submit(func() {
		executed.Store(true)
		close(done)
	}) {
		t.Fatal("failed to submit follow-up task")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker pool did not continue after panic")
	}

	if !executed.Load() {
		t.Fatal("expected follow-up task to execute")
	}
}

func TestWorkerPoolSubmitAfterShutdownReturnsFalse(t *testing.T) {
	pool := NewWorkerPool(1, 1)
	pool.Start()
	pool.Shutdown()

	if pool.Submit(func() {}) {
		t.Fatal("submit after shutdown should return false")
	}
}
