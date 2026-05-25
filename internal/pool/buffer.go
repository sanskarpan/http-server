package pool

import (
	"sync"
)

// BufferPool is a pool of reusable byte slices
type BufferPool struct {
	pool sync.Pool
	size int
}

// NewBufferPool creates a new buffer pool
func NewBufferPool(size int) *BufferPool {
	return &BufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				buf := make([]byte, size)
				return &buf
			},
		},
		size: size,
	}
}

// Get retrieves a buffer from the pool
func (p *BufferPool) Get() []byte {
	bufPtr := p.pool.Get().(*[]byte)
	return (*bufPtr)[:0] // Reset length to 0, keep capacity
}

// Put returns a buffer to the pool
func (p *BufferPool) Put(buf []byte) {
	// Only pool buffers of the expected size
	if cap(buf) == p.size {
		buf = buf[:0]
		p.pool.Put(&buf)
	}
}

// Default buffer pool sizes
var (
	// Small buffers for headers
	SmallBufferPool = NewBufferPool(4 * 1024) // 4 KB

	// Medium buffers for typical requests/responses
	MediumBufferPool = NewBufferPool(32 * 1024) // 32 KB

	// Large buffers for file serving
	LargeBufferPool = NewBufferPool(128 * 1024) // 128 KB
)
