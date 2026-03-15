package proxy

import (
	"sync"
)

// BufferPool 全局缓冲池
type BufferPool struct {
	pools map[int]*sync.Pool
	mu    sync.RWMutex
}

// 全局缓冲池实例
var globalBufferPool = NewBufferPool()

// NewBufferPool 创建缓冲池
func NewBufferPool() *BufferPool {
	return &BufferPool{
		pools: make(map[int]*sync.Pool),
	}
}

// Get 获取指定大小的缓冲区
func (p *BufferPool) Get(size int) []byte {
	// 向上取整到最近的 1KB
	poolSize := ((size + 1023) / 1024) * 1024
	if poolSize < 1024 {
		poolSize = 1024
	}

	p.mu.RLock()
	pool, ok := p.pools[poolSize]
	p.mu.RUnlock()

	if !ok {
		p.mu.Lock()
		// 双重检查
		if pool, ok = p.pools[poolSize]; !ok {
			pool = &sync.Pool{
				New: func() interface{} {
					return make([]byte, poolSize)
				},
			}
			p.pools[poolSize] = pool
		}
		p.mu.Unlock()
	}

	return pool.Get().([]byte)[:size]
}

// Put 归还缓冲区
func (p *BufferPool) Put(buf []byte) {
	if cap(buf) == 0 {
		return
	}

	poolSize := ((cap(buf) + 1023) / 1024) * 1024

	p.mu.RLock()
	pool, ok := p.pools[poolSize]
	p.mu.RUnlock()

	if ok {
		pool.Put(buf[:cap(buf)])
	}
}

// GetBuffer 从全局池获取缓冲区
func GetBuffer(size int) []byte {
	return globalBufferPool.Get(size)
}

// PutBuffer 向全局池归还缓冲区
func PutBuffer(buf []byte) {
	globalBufferPool.Put(buf)
}

// BufferPoolStats 缓冲池统计
type BufferPoolStats struct {
	PoolCount int
	TotalSize int64
}

// Stats 获取缓冲池统计
func (p *BufferPool) Stats() BufferPoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := BufferPoolStats{
		PoolCount: len(p.pools),
	}

	for size := range p.pools {
		stats.TotalSize += int64(size)
	}

	return stats
}

// SizedPool 固定大小的缓冲池
type SizedPool struct {
	pool   *sync.Pool
	size   int
	gets   int64
	puts   int64
}

// NewSizedPool 创建固定大小的缓冲池
func NewSizedPool(size int) *SizedPool {
	return &SizedPool{
		pool: &sync.Pool{
			New: func() interface{} {
				return make([]byte, size)
			},
		},
		size: size,
	}
}

// Get 获取缓冲区
func (p *SizedPool) Get() []byte {
	p.gets++
	return p.pool.Get().([]byte)
}

// Put 归还缓冲区
func (p *SizedPool) Put(buf []byte) {
	p.puts++
	p.pool.Put(buf)
}

// Stats 获取统计
func (p *SizedPool) Stats() (gets, puts int64) {
	return p.gets, p.puts
}