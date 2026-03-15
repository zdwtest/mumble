package proxy

import (
	"testing"
)

func TestBufferPool_GetPut(t *testing.T) {
	pool := NewBufferPool()

	// Get a buffer
	buf := pool.Get(1024)
	if len(buf) != 1024 {
		t.Errorf("expected buffer size 1024, got %d", len(buf))
	}

	// Put it back
	pool.Put(buf)
}

func TestBufferPool_MultipleSizes(t *testing.T) {
	pool := NewBufferPool()

	sizes := []int{512, 1024, 2048, 4096, 8192}
	for _, size := range sizes {
		buf := pool.Get(size)
		if len(buf) != size {
			t.Errorf("expected buffer size %d, got %d", size, len(buf))
		}
		pool.Put(buf)
	}
}

func TestBufferPool_Stats(t *testing.T) {
	pool := NewBufferPool()

	// Get buffers of different sizes
	pool.Get(1024)
	pool.Get(2048)
	pool.Get(4096)

	stats := pool.Stats()
	if stats.PoolCount == 0 {
		t.Error("expected at least one pool")
	}
}

func TestBufferPool_ZeroSize(t *testing.T) {
	pool := NewBufferPool()

	// Get with size 0
	buf := pool.Get(0)
	if len(buf) != 0 {
		t.Errorf("expected buffer size 0, got %d", len(buf))
	}

	// Put empty buffer should not panic
	pool.Put(buf)
}

func TestGetPutBuffer(t *testing.T) {
	buf := GetBuffer(1024)
	if len(buf) != 1024 {
		t.Errorf("expected buffer size 1024, got %d", len(buf))
	}
	PutBuffer(buf)
}

func TestSizedPool_GetPut(t *testing.T) {
	pool := NewSizedPool(4096)

	buf := pool.Get()
	if len(buf) != 4096 {
		t.Errorf("expected buffer size 4096, got %d", len(buf))
	}

	pool.Put(buf)

	gets, puts := pool.Stats()
	if gets != 1 {
		t.Errorf("expected 1 get, got %d", gets)
	}
	if puts != 1 {
		t.Errorf("expected 1 put, got %d", puts)
	}
}

func TestSizedPool_MultipleOperations(t *testing.T) {
	pool := NewSizedPool(1024)

	for i := 0; i < 10; i++ {
		buf := pool.Get()
		pool.Put(buf)
	}

	gets, puts := pool.Stats()
	if gets != 10 {
		t.Errorf("expected 10 gets, got %d", gets)
	}
	if puts != 10 {
		t.Errorf("expected 10 puts, got %d", puts)
	}
}

func TestBufferPool_Concurrent(t *testing.T) {
	pool := NewBufferPool()

	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				buf := pool.Get(1024)
				pool.Put(buf)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestSizedPool_Concurrent(t *testing.T) {
	pool := NewSizedPool(1024)

	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				buf := pool.Get()
				pool.Put(buf)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}