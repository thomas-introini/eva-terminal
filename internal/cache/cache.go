// Package cache provides a generic in-memory TTL cache.
package cache

import (
	"sync"
	"time"
)

// entry holds a cached value with its expiration time.
type entry[V any] struct {
	value     V
	expiresAt time.Time
}

// Cache is a generic TTL cache with mutex protection.
type Cache[K comparable, V any] struct {
	mu      sync.RWMutex
	items   map[K]entry[V]
	ttl     time.Duration
	nowFunc func() time.Time // For testing
}

// New creates a new cache with the specified TTL.
func New[K comparable, V any](ttl time.Duration) *Cache[K, V] {
	return &Cache[K, V]{
		items:   make(map[K]entry[V]),
		ttl:     ttl,
		nowFunc: time.Now,
	}
}

// Get retrieves a value from the cache.
// Returns the value and true if found and not expired, otherwise zero value and false.
func (c *Cache[K, V]) Get(key K) (V, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	e, ok := c.items[key]
	if !ok {
		var zero V
		return zero, false
	}

	if c.nowFunc().After(e.expiresAt) {
		var zero V
		return zero, false
	}

	return e.value, true
}

// Set stores a value in the cache with the configured TTL.
func (c *Cache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[key] = entry[V]{
		value:     value,
		expiresAt: c.nowFunc().Add(c.ttl),
	}
}

// Delete removes a value from the cache.
func (c *Cache[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.items, key)
}

// Clear removes all items from the cache.
func (c *Cache[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[K]entry[V])
}

// Cleanup removes expired entries from the cache.
// This can be called periodically to free memory.
func (c *Cache[K, V]) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.nowFunc()
	for key, e := range c.items {
		if now.After(e.expiresAt) {
			delete(c.items, key)
		}
	}
}

// Len returns the number of items in the cache (including expired ones).
func (c *Cache[K, V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}



