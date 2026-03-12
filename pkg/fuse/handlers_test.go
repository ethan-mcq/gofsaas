package fuse_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/your-org/gofsaas/pkg/fakes"
	fusepkg "github.com/your-org/gofsaas/pkg/fuse"
	"github.com/your-org/gofsaas/pkg/resolver"
	"github.com/your-org/gofsaas/pkg/s3client"
	"github.com/your-org/gofsaas/pkg/state"
)

// These tests exercise the FUSE handler logic (stat, fetch, list)
// without mounting a real FUSE filesystem.

func makeFS(s3c s3client.Client) *fusepkg.FS {
	return makeFSWithTTL(s3c, 30*time.Second)
}

func makeFSWithTTL(s3c s3client.Client, ttl time.Duration) *fusepkg.FS {
	r := resolver.New("/files", "my-bucket", "data/")
	sm := state.NewStateMap()
	c := fakes.NewInMemoryCache()
	return fusepkg.NewFS(r, s3c, sm, c, 4, ttl)
}

// headCountSpy wraps any s3client.Client and counts Head calls.
type headCountSpy struct {
	mu        sync.Mutex
	headCalls int
	delegate  s3client.Client
}

func (s *headCountSpy) Head(ctx context.Context, bucket, key string) (s3client.ObjectMeta, error) {
	s.mu.Lock()
	s.headCalls++
	s.mu.Unlock()
	return s.delegate.Head(ctx, bucket, key)
}
func (s *headCountSpy) Get(ctx context.Context, bucket, key string, dst io.Writer) error {
	return s.delegate.Get(ctx, bucket, key, dst)
}
func (s *headCountSpy) List(ctx context.Context, bucket, prefix string) ([]s3client.ObjectMeta, error) {
	return s.delegate.List(ctx, bucket, prefix)
}

func TestStatFile_Found(t *testing.T) {
	s3c := fakes.NewInMemoryS3Client()
	s3c.AddObject("my-bucket", "data/samples/HG001.bam", []byte("content"))
	r := resolver.New("/files", "my-bucket", "data/")

	meta, err := fusepkg.StatFile(context.Background(), r, s3c, "/files/samples/HG001.bam")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.Size != int64(len("content")) {
		t.Fatalf("size mismatch: got %d want %d", meta.Size, len("content"))
	}
}

func TestStatFile_NotFound(t *testing.T) {
	s3c := fakes.NewInMemoryS3Client()
	r := resolver.New("/files", "my-bucket", "data/")

	_, err := fusepkg.StatFile(context.Background(), r, s3c, "/files/missing.bam")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, s3client.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestFetchFile_Success(t *testing.T) {
	s3c := fakes.NewInMemoryS3Client()
	s3c.AddObject("my-bucket", "data/samples/HG001.bam", []byte("bam-data"))
	fuseFS := makeFS(s3c)

	err := fusepkg.FetchFile(context.Background(), fuseFS, "/files/samples/HG001.bam")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's in the cache
	if !fuseFS.Cache().IsCached("files/samples/HG001.bam") {
		t.Fatal("expected file to be cached after fetch")
	}
}

func TestFetchFile_NotFound(t *testing.T) {
	s3c := fakes.NewInMemoryS3Client()
	fuseFS := makeFS(s3c)

	err := fusepkg.FetchFile(context.Background(), fuseFS, "/files/missing.bam")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, s3client.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListDir_ReturnsEntries(t *testing.T) {
	s3c := fakes.NewInMemoryS3Client()
	s3c.AddObject("my-bucket", "data/samples/HG001.bam", []byte("a"))
	s3c.AddObject("my-bucket", "data/samples/HG002.bam", []byte("b"))
	s3c.AddObject("my-bucket", "data/samples/subdir/x.bam", []byte("c"))
	r := resolver.New("/files", "my-bucket", "data/")

	entries, err := fusepkg.ListDir(context.Background(), r, s3c, "/files/samples")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one entry")
	}
}

func TestGetattr_CacheHit_NoS3Call(t *testing.T) {
	base := fakes.NewInMemoryS3Client()
	base.AddObject("my-bucket", "data/samples/HG001.bam", []byte("content"))
	spy := &headCountSpy{delegate: base}

	fuseFS := makeFSWithTTL(spy, 30*time.Second)
	ctx := context.Background()

	// First call — populates the attr cache.
	meta1, err := fusepkg.StatFileCached(ctx, fuseFS, "/files/samples/HG001.bam")
	if err != nil {
		t.Fatalf("first StatFileCached: %v", err)
	}

	// Second call within TTL — must hit cache, no S3 HEAD.
	meta2, err := fusepkg.StatFileCached(ctx, fuseFS, "/files/samples/HG001.bam")
	if err != nil {
		t.Fatalf("second StatFileCached: %v", err)
	}

	if spy.headCalls != 1 {
		t.Fatalf("headCalls: got %d want 1", spy.headCalls)
	}
	if meta1.Size != meta2.Size {
		t.Fatalf("size mismatch between calls: %d vs %d", meta1.Size, meta2.Size)
	}
}

func TestGetattr_CacheExpiry_RefetchesS3(t *testing.T) {
	base := fakes.NewInMemoryS3Client()
	base.AddObject("my-bucket", "data/samples/HG001.bam", []byte("content"))
	spy := &headCountSpy{delegate: base}

	fuseFS := makeFSWithTTL(spy, 1*time.Millisecond)
	ctx := context.Background()

	// First call — populates the attr cache.
	if _, err := fusepkg.StatFileCached(ctx, fuseFS, "/files/samples/HG001.bam"); err != nil {
		t.Fatalf("first StatFileCached: %v", err)
	}

	// Wait for TTL to expire.
	time.Sleep(5 * time.Millisecond)

	// Second call — TTL expired, must hit S3 again.
	if _, err := fusepkg.StatFileCached(ctx, fuseFS, "/files/samples/HG001.bam"); err != nil {
		t.Fatalf("second StatFileCached: %v", err)
	}

	if spy.headCalls != 2 {
		t.Fatalf("headCalls: got %d want 2", spy.headCalls)
	}
}
