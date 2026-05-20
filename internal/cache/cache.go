package cache

import (
	"container/list"
	"sync"
	"sync/atomic"
	"time"
)

// Options configures the LRU cache.
type Options struct {
	MaxEntries int
	TTL        time.Duration
}

// CacheStats holds hit/miss/eviction counters.
type CacheStats struct {
	Hits      int64
	Misses    int64
	Evictions int64
}

type entry struct {
	key       string
	value     []byte
	expiresAt time.Time
}

// Cache is a thread-safe LRU cache with TTL eviction.
type Cache struct {
	mu         sync.Mutex
	maxEntries int
	ttl        time.Duration
	items      map[string]*list.Element
	order      *list.List // front = most recently used

	hits      atomic.Int64
	misses    atomic.Int64
	evictions atomic.Int64
}

// New returns a Cache with the given options. Zero values use defaults:
// MaxEntries=1000, TTL=5min.
func New(opts Options) *Cache {
	maxEntries := opts.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 1000
	}
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &Cache{
		maxEntries: maxEntries,
		ttl:        ttl,
		items:      make(map[string]*list.Element),
		order:      list.New(),
	}
}

// Get returns the cached value for key. Returns false on miss or expiry.
func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		c.misses.Add(1)
		return nil, false
	}

	e := elem.Value.(*entry)
	if time.Now().After(e.expiresAt) {
		c.removeLocked(elem)
		c.misses.Add(1)
		return nil, false
	}

	c.order.MoveToFront(elem)
	c.hits.Add(1)
	return e.value, true
}

// Set stores value under key, refreshing TTL. Evicts LRU entries when over capacity.
func (c *Cache) Set(key string, value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		e := elem.Value.(*entry)
		e.value = value
		e.expiresAt = time.Now().Add(c.ttl)
		return
	}

	e := &entry{
		key:       key,
		value:     value,
		expiresAt: time.Now().Add(c.ttl),
	}
	elem := c.order.PushFront(e)
	c.items[key] = elem

	for c.order.Len() > c.maxEntries {
		c.evictOldest()
	}
}

// Invalidate removes key from the cache.
func (c *Cache) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.removeLocked(elem)
	}
}

// Stats returns a snapshot of hit/miss/eviction counters.
func (c *Cache) Stats() CacheStats {
	return CacheStats{
		Hits:      c.hits.Load(),
		Misses:    c.misses.Load(),
		Evictions: c.evictions.Load(),
	}
}

func (c *Cache) evictOldest() {
	back := c.order.Back()
	if back == nil {
		return
	}
	c.removeLocked(back)
	c.evictions.Add(1)
}

func (c *Cache) removeLocked(elem *list.Element) {
	e := elem.Value.(*entry)
	delete(c.items, e.key)
	c.order.Remove(elem)
}
