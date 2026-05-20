package cache_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/engram/internal/cache"
)

func TestNew_Defaults(t *testing.T) {
	t.Parallel()
	c := cache.New(cache.Options{})
	// zero-value options should use defaults: 1000 entries, 5min TTL
	// verify it works without panicking and can store/retrieve
	c.Set("k", []byte("v"))
	got, ok := c.Get("k")
	if !ok {
		t.Fatal("expected hit after Set with default options")
	}
	if string(got) != "v" {
		t.Errorf("want v, got %s", got)
	}
}

func TestGetSet(t *testing.T) {
	t.Parallel()
	c := cache.New(cache.Options{MaxEntries: 10, TTL: time.Minute})
	c.Set("hello", []byte("world"))
	val, ok := c.Get("hello")
	if !ok {
		t.Fatal("expected hit")
	}
	if string(val) != "world" {
		t.Errorf("want world, got %s", val)
	}
}

func TestGet_Miss(t *testing.T) {
	t.Parallel()
	c := cache.New(cache.Options{MaxEntries: 10, TTL: time.Minute})
	_, ok := c.Get("nonexistent")
	if ok {
		t.Fatal("expected miss for nonexistent key")
	}
	stats := c.Stats()
	if stats.Misses != 1 {
		t.Errorf("want 1 miss, got %d", stats.Misses)
	}
}

func TestGet_Expired(t *testing.T) {
	t.Parallel()
	c := cache.New(cache.Options{MaxEntries: 10, TTL: 10 * time.Millisecond})
	c.Set("key", []byte("val"))
	time.Sleep(20 * time.Millisecond)
	_, ok := c.Get("key")
	if ok {
		t.Fatal("expected miss for expired key")
	}
}

func TestSet_Overwrite(t *testing.T) {
	t.Parallel()
	c := cache.New(cache.Options{MaxEntries: 10, TTL: time.Minute})
	c.Set("key", []byte("first"))
	c.Set("key", []byte("second"))
	val, ok := c.Get("key")
	if !ok {
		t.Fatal("expected hit")
	}
	if string(val) != "second" {
		t.Errorf("want second, got %s", val)
	}
}

func TestSet_EvictsLRU(t *testing.T) {
	t.Parallel()
	c := cache.New(cache.Options{MaxEntries: 3, TTL: time.Minute})
	c.Set("a", []byte("1"))
	c.Set("b", []byte("2"))
	c.Set("c", []byte("3"))
	// access a and b to make c the LRU
	c.Get("a")
	c.Get("b")
	// inserting d should evict c (LRU)
	c.Set("d", []byte("4"))
	_, ok := c.Get("c")
	if ok {
		t.Fatal("c should have been evicted")
	}
	stats := c.Stats()
	if stats.Evictions < 1 {
		t.Errorf("want at least 1 eviction, got %d", stats.Evictions)
	}
}

func TestInvalidate(t *testing.T) {
	t.Parallel()
	c := cache.New(cache.Options{MaxEntries: 10, TTL: time.Minute})
	c.Set("key", []byte("val"))
	c.Invalidate("key")
	_, ok := c.Get("key")
	if ok {
		t.Fatal("expected miss after invalidate")
	}
}

func TestInvalidate_NonExistent(t *testing.T) {
	t.Parallel()
	c := cache.New(cache.Options{MaxEntries: 10, TTL: time.Minute})
	// should not panic
	c.Invalidate("does-not-exist")
}

func TestStats(t *testing.T) {
	t.Parallel()
	c := cache.New(cache.Options{MaxEntries: 2, TTL: time.Minute})
	c.Set("a", []byte("1"))
	c.Set("b", []byte("2"))
	c.Get("a")     // hit
	c.Get("miss1") // miss
	c.Get("miss2") // miss
	c.Set("c", []byte("3")) // evicts oldest

	stats := c.Stats()
	if stats.Hits != 1 {
		t.Errorf("want 1 hit, got %d", stats.Hits)
	}
	if stats.Misses != 2 {
		t.Errorf("want 2 misses, got %d", stats.Misses)
	}
	if stats.Evictions < 1 {
		t.Errorf("want at least 1 eviction, got %d", stats.Evictions)
	}
}

func TestConcurrentAccess(t *testing.T) {
	t.Parallel()
	c := cache.New(cache.Options{MaxEntries: 100, TTL: time.Minute})
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", n)
			c.Set(key, []byte(fmt.Sprintf("val-%d", n)))
			c.Get(key)
		}(i)
	}
	wg.Wait()
}
