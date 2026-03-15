package cache

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// Cache manages a local disk cache of S3 objects.
type Cache interface {
	LocalPath(mountRelPath string) string
	Write(mountRelPath string, r io.Reader) error
	Delete(mountRelPath string) (freedBytes int64, err error)
	OpenRef(mountRelPath string)
	CloseRef(mountRelPath string)
	IsCached(mountRelPath string) bool
	// Stats returns the total bytes used and number of files stored in the cache.
	Stats() (bytesUsed int64, filesCached int)
}

type refEntry struct {
	count int32
}

// DiskCache is a filesystem-backed implementation of Cache.
type DiskCache struct {
	baseDir string
	mu      sync.Mutex
	refs    map[string]*refEntry
}

// New creates a DiskCache rooted at baseDir (creating it if needed).
func New(baseDir string) *DiskCache {
	os.MkdirAll(baseDir, 0755)
	return &DiskCache{
		baseDir: baseDir,
		refs:    make(map[string]*refEntry),
	}
}

// LocalPath returns the absolute filesystem path for a relative cache path.
func (c *DiskCache) LocalPath(relPath string) string {
	return filepath.Join(c.baseDir, relPath)
}

// Write atomically writes the contents of r to the cache at relPath.
func (c *DiskCache) Write(relPath string, r io.Reader) error {
	dst := c.LocalPath(relPath)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("cache.Write mkdir %s: %w", relPath, err)
	}
	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("cache.Write create tmp %s: %w", relPath, err)
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("cache.Write copy %s: %w", relPath, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("cache.Write close %s: %w", relPath, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("cache.Write rename %s: %w", relPath, err)
	}
	return nil
}

// Delete removes the cached file and returns the number of bytes freed.
// Returns an error if the file has open references.
func (c *DiskCache) Delete(relPath string) (int64, error) {
	c.mu.Lock()
	e := c.refs[relPath]
	if e != nil && atomic.LoadInt32(&e.count) > 0 {
		c.mu.Unlock()
		return 0, fmt.Errorf("cache.Delete %s: %d open descriptors, cannot clean", relPath, atomic.LoadInt32(&e.count))
	}
	c.mu.Unlock()

	dst := c.LocalPath(relPath)
	info, err := os.Stat(dst)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("cache.Delete stat %s: %w", relPath, err)
	}
	size := info.Size()
	if err := os.Remove(dst); err != nil {
		return 0, fmt.Errorf("cache.Delete remove %s: %w", relPath, err)
	}
	return size, nil
}

func (c *DiskCache) getOrCreateRef(relPath string) *refEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.refs[relPath]
	if !ok {
		e = &refEntry{}
		c.refs[relPath] = e
	}
	return e
}

// OpenRef increments the open reference count for relPath.
func (c *DiskCache) OpenRef(relPath string) {
	e := c.getOrCreateRef(relPath)
	atomic.AddInt32(&e.count, 1)
}

// CloseRef decrements the open reference count for relPath.
func (c *DiskCache) CloseRef(relPath string) {
	c.mu.Lock()
	e := c.refs[relPath]
	c.mu.Unlock()
	if e == nil {
		panic(fmt.Sprintf("cache.CloseRef %s: no ref entry exists", relPath))
	}
	newCount := atomic.AddInt32(&e.count, -1)
	if newCount < 0 {
		panic(fmt.Sprintf("cache.CloseRef %s: ref count went below 0", relPath))
	}
}

// IsCached returns true if the file exists in the cache.
func (c *DiskCache) IsCached(relPath string) bool {
	_, err := os.Stat(c.LocalPath(relPath))
	return err == nil
}

// Stats walks the cache directory and returns total bytes used and file count.
func (c *DiskCache) Stats() (bytesUsed int64, filesCached int) {
	filepath.Walk(c.baseDir, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		bytesUsed += info.Size()
		filesCached++
		return nil
	})
	return
}
