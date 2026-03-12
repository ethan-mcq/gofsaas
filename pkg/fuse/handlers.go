package fuse

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	bazil "bazil.org/fuse"

	"github.com/your-org/gofsaas/pkg/resolver"
	"github.com/your-org/gofsaas/pkg/s3client"
)

// StatFile performs a HEAD request for the file at absPath.
// Exported for use in tests.
func StatFile(ctx context.Context, r resolver.Resolver, s3 s3client.Client, absPath string) (s3client.ObjectMeta, error) {
	return statFile(ctx, r, s3, absPath)
}

// StatFileCached performs a HEAD request for the file at absPath, using the
// FS attr cache. On a live entry (expiry > now) no S3 call is made.
// Exported for use in tests.
func StatFileCached(ctx context.Context, fuseFS *FS, absPath string) (s3client.ObjectMeta, error) {
	return statFileCached(ctx, fuseFS, absPath)
}

// FetchFile ensures the file at absPath is fetched into the cache exactly once.
// Exported for use in tests.
func FetchFile(ctx context.Context, fuseFS *FS, absPath string) error {
	return fetchFile(ctx, fuseFS, absPath)
}

// ListDir lists the objects under absPath from S3 and returns FUSE directory entries.
// Exported for use in tests.
func ListDir(ctx context.Context, r resolver.Resolver, s3 s3client.Client, absPath string) ([]bazil.Dirent, error) {
	return listDir(ctx, r, s3, absPath)
}

// statFileCached checks the FS attr cache before falling through to a live S3 HEAD.
func statFileCached(ctx context.Context, fuseFS *FS, absPath string) (s3client.ObjectMeta, error) {
	now := time.Now()

	fuseFS.attrMu.RLock()
	e, ok := fuseFS.attrCache[absPath]
	fuseFS.attrMu.RUnlock()
	if ok && e.expiry.After(now) {
		return e.meta, nil
	}

	meta, err := statFile(ctx, fuseFS.resolver, fuseFS.s3, absPath)
	if err != nil {
		return s3client.ObjectMeta{}, err
	}

	fuseFS.attrMu.Lock()
	fuseFS.attrCache[absPath] = attrCacheEntry{meta: meta, expiry: now.Add(fuseFS.attrCacheTTL)}
	fuseFS.attrMu.Unlock()

	return meta, nil
}

// statFile performs a HEAD request for the file at absPath.
func statFile(ctx context.Context, r resolver.Resolver, s3 s3client.Client, absPath string) (s3client.ObjectMeta, error) {
	bucket, key, err := r.Resolve(absPath)
	if err != nil {
		return s3client.ObjectMeta{}, fmt.Errorf("statFile resolve %s: %w", absPath, err)
	}
	meta, err := s3.Head(ctx, bucket, key)
	if err != nil {
		return s3client.ObjectMeta{}, fmt.Errorf("statFile head %s/%s: %w", bucket, key, err)
	}
	return meta, nil
}

// fetchFile ensures the file at absPath is fetched into the cache exactly once.
func fetchFile(ctx context.Context, fuseFS *FS, absPath string) error {
	mountRelPath := absToRelPath(absPath)

	// Acquire semaphore slot
	fuseFS.sem <- struct{}{}
	defer func() { <-fuseFS.sem }()

	return fuseFS.sm.Fetch(absPath, func() error {
		bucket, key, err := fuseFS.resolver.Resolve(absPath)
		if err != nil {
			return fmt.Errorf("fetchFile resolve %s: %w", absPath, err)
		}
		pr, pw := io.Pipe()
		var getErr error
		go func() {
			getErr = fuseFS.s3.Get(ctx, bucket, key, pw)
			pw.CloseWithError(getErr)
		}()
		writeErr := fuseFS.cache.Write(mountRelPath, pr)
		if getErr != nil {
			return getErr
		}
		return writeErr
	})
}

// listDir lists the objects under absPath from S3 and returns FUSE directory entries.
func listDir(ctx context.Context, r resolver.Resolver, s3 s3client.Client, absPath string) ([]bazil.Dirent, error) {
	bucket, prefix, err := r.Resolve(absPath)
	if err != nil {
		return nil, fmt.Errorf("listDir resolve %s: %w", absPath, err)
	}
	// Ensure prefix ends with "/" for directory listing
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix = prefix + "/"
	}

	objects, err := s3.List(ctx, bucket, prefix)
	if err != nil {
		return nil, fmt.Errorf("listDir list %s/%s: %w", bucket, prefix, err)
	}

	seen := make(map[string]bool)
	var entries []bazil.Dirent

	for _, obj := range objects {
		// Compute path relative to this directory
		rel := strings.TrimPrefix(obj.Key, prefix)
		if rel == "" {
			continue
		}
		// Top-level name (may be file or subdirectory)
		parts := strings.SplitN(rel, "/", 2)
		name := parts[0]
		if seen[name] {
			continue
		}
		seen[name] = true

		dirent := bazil.Dirent{Name: name}
		if len(parts) == 2 && parts[1] != "" {
			dirent.Type = bazil.DT_Dir
		} else {
			dirent.Type = bazil.DT_File
		}
		entries = append(entries, dirent)
	}

	return entries, nil
}

// joinPath joins an absolute directory path with a child name.
func joinPath(dir, name string) string {
	return path.Join(dir, name)
}
