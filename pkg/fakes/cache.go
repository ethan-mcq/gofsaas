package fakes

import (
	"fmt"
	"io"
	"sync"
)

// InMemoryCache is a fake Cache for use in tests.
type InMemoryCache struct {
	mu      sync.Mutex
	entries map[string][]byte
	refs    map[string]int
}

// NewInMemoryCache creates a new InMemoryCache.
func NewInMemoryCache() *InMemoryCache {
	return &InMemoryCache{
		entries: make(map[string][]byte),
		refs:    make(map[string]int),
	}
}

// Write stores the contents of r at relPath.
func (c *InMemoryCache) Write(relPath string, r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("InMemoryCache.Write %s: %w", relPath, err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[relPath] = data
	return nil
}

// Delete removes the entry at relPath and returns the freed bytes.
func (c *InMemoryCache) Delete(relPath string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.refs[relPath] > 0 {
		return 0, fmt.Errorf("%s has %d open descriptors, cannot clean", relPath, c.refs[relPath])
	}
	data := c.entries[relPath]
	delete(c.entries, relPath)
	return int64(len(data)), nil
}

// OpenRef increments the open reference count for relPath.
func (c *InMemoryCache) OpenRef(relPath string) {
	c.mu.Lock()
	c.refs[relPath]++
	c.mu.Unlock()
}

// CloseRef decrements the open reference count for relPath.
func (c *InMemoryCache) CloseRef(relPath string) {
	c.mu.Lock()
	c.refs[relPath]--
	c.mu.Unlock()
}

// IsCached returns true if the entry exists.
func (c *InMemoryCache) IsCached(relPath string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.entries[relPath]
	return ok
}

// LocalPath returns a synthetic in-memory path.
func (c *InMemoryCache) LocalPath(relPath string) string {
	return "/inmem/" + relPath
}
