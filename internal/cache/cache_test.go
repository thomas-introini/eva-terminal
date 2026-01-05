package cache

import (
	"sync"
	"testing"
	"time"
)

func TestCacheSetGet(t *testing.T) {
	c := New[string, int](time.Minute)

	// Test setting and getting
	c.Set("key1", 42)
	val, ok := c.Get("key1")
	if !ok {
		t.Fatal("expected to find key1")
	}
	if val != 42 {
		t.Errorf("expected 42, got %d", val)
	}

	// Test missing key
	_, ok = c.Get("nonexistent")
	if ok {
		t.Error("expected not to find nonexistent key")
	}
}

func TestCacheExpiry(t *testing.T) {
	// Create cache with very short TTL
	c := New[string, string](50 * time.Millisecond)

	c.Set("key", "value")

	// Should exist immediately
	val, ok := c.Get("key")
	if !ok {
		t.Fatal("expected to find key immediately")
	}
	if val != "value" {
		t.Errorf("expected 'value', got '%s'", val)
	}

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	// Should be expired now
	_, ok = c.Get("key")
	if ok {
		t.Error("expected key to be expired")
	}
}

func TestCacheExpiryWithMockedTime(t *testing.T) {
	c := New[string, string](time.Minute)

	// Mock time
	currentTime := time.Now()
	c.nowFunc = func() time.Time {
		return currentTime
	}

	c.Set("key", "value")

	// Should exist
	_, ok := c.Get("key")
	if !ok {
		t.Fatal("expected to find key")
	}

	// Advance time past TTL
	currentTime = currentTime.Add(2 * time.Minute)

	// Should be expired
	_, ok = c.Get("key")
	if ok {
		t.Error("expected key to be expired after time advance")
	}
}

func TestCacheDelete(t *testing.T) {
	c := New[string, int](time.Minute)

	c.Set("key", 100)
	c.Delete("key")

	_, ok := c.Get("key")
	if ok {
		t.Error("expected key to be deleted")
	}

	// Deleting non-existent key should not panic
	c.Delete("nonexistent")
}

func TestCacheClear(t *testing.T) {
	c := New[string, int](time.Minute)

	c.Set("key1", 1)
	c.Set("key2", 2)
	c.Set("key3", 3)

	c.Clear()

	if c.Len() != 0 {
		t.Errorf("expected cache to be empty, got len=%d", c.Len())
	}

	_, ok := c.Get("key1")
	if ok {
		t.Error("expected cache to be cleared")
	}
}

func TestCacheCleanup(t *testing.T) {
	c := New[string, string](50 * time.Millisecond)

	c.Set("key1", "val1")
	c.Set("key2", "val2")

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	// Add a fresh key
	c.Set("key3", "val3")

	// Cleanup should remove expired keys
	c.Cleanup()

	// Only key3 should remain in the map
	if c.Len() != 1 {
		t.Errorf("expected 1 item after cleanup, got %d", c.Len())
	}

	// key3 should still be accessible
	val, ok := c.Get("key3")
	if !ok {
		t.Fatal("expected key3 to exist after cleanup")
	}
	if val != "val3" {
		t.Errorf("expected 'val3', got '%s'", val)
	}
}

func TestCacheConcurrency(t *testing.T) {
	c := New[int, int](time.Minute)

	var wg sync.WaitGroup
	numGoroutines := 100

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c.Set(i, i*2)
		}(i)
	}
	wg.Wait()

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			val, ok := c.Get(i)
			if !ok {
				t.Errorf("expected to find key %d", i)
				return
			}
			if val != i*2 {
				t.Errorf("expected %d, got %d", i*2, val)
			}
		}(i)
	}
	wg.Wait()
}

func TestCacheWithStructKey(t *testing.T) {
	type CacheKey struct {
		Page    int
		Search  string
	}

	c := New[CacheKey, []string](time.Minute)

	key1 := CacheKey{Page: 1, Search: "coffee"}
	key2 := CacheKey{Page: 2, Search: "coffee"}

	c.Set(key1, []string{"product1", "product2"})
	c.Set(key2, []string{"product3"})

	val1, ok := c.Get(key1)
	if !ok {
		t.Fatal("expected to find key1")
	}
	if len(val1) != 2 {
		t.Errorf("expected 2 items, got %d", len(val1))
	}

	val2, ok := c.Get(key2)
	if !ok {
		t.Fatal("expected to find key2")
	}
	if len(val2) != 1 {
		t.Errorf("expected 1 item, got %d", len(val2))
	}

	// Different key should not match
	key3 := CacheKey{Page: 1, Search: "tea"}
	_, ok = c.Get(key3)
	if ok {
		t.Error("expected not to find different key")
	}
}

func TestCacheLen(t *testing.T) {
	c := New[string, int](time.Minute)

	if c.Len() != 0 {
		t.Errorf("expected empty cache, got len=%d", c.Len())
	}

	c.Set("a", 1)
	c.Set("b", 2)

	if c.Len() != 2 {
		t.Errorf("expected len=2, got %d", c.Len())
	}

	c.Delete("a")

	if c.Len() != 1 {
		t.Errorf("expected len=1, got %d", c.Len())
	}
}

func TestCacheOverwrite(t *testing.T) {
	c := New[string, int](time.Minute)

	c.Set("key", 1)
	c.Set("key", 2)

	val, ok := c.Get("key")
	if !ok {
		t.Fatal("expected to find key")
	}
	if val != 2 {
		t.Errorf("expected 2 (overwritten value), got %d", val)
	}
}



