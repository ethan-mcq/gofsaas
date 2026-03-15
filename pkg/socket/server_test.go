package socket_test

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/your-org/gofsaas/pkg/fakes"
	"github.com/your-org/gofsaas/pkg/s3client"
	"github.com/your-org/gofsaas/pkg/socket"
	"github.com/your-org/gofsaas/pkg/state"
)

// --- Spy types for socket handler tests ---

type resolverSpy struct {
	bucket     string
	prefix     string
	mountPoint string // stripped from absPath to get the rel path
	err        error
}

func (r *resolverSpy) Resolve(absPath string) (string, string, error) {
	if r.err != nil {
		return "", "", r.err
	}
	// Derive key by stripping mount prefix and prepending S3 prefix.
	rel := absPath
	if r.mountPoint != "" && len(absPath) > len(r.mountPoint) {
		rel = absPath[len(r.mountPoint)+1:] // +1 for the "/"
	} else if len(absPath) > 0 && absPath[0] == '/' {
		rel = absPath[1:]
	}
	key := r.prefix + rel
	return r.bucket, key, nil
}

// --- Tests ---

func TestHandle_Exists_Found(t *testing.T) {
	s3c := fakes.NewInMemoryS3Client()
	s3c.AddObject("my-bucket", "data/samples/HG001.bam", []byte("content"))
	cache := fakes.NewInMemoryCache()
	sm := state.NewStateMap()
	res := &resolverSpy{bucket: "my-bucket", prefix: "data/", mountPoint: "/files"}
	h := socket.NewHandler(res, s3c, sm, cache)

	resp := h.Handle(context.Background(), socket.Request{Op: "exists", Path: "/files/samples/HG001.bam"})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if !resp.Exists {
		t.Fatal("expected Exists=true")
	}
	if resp.SizeBytes != int64(len("content")) {
		t.Fatalf("size mismatch: got %d want %d", resp.SizeBytes, len("content"))
	}
}

func TestHandle_Exists_NotFound(t *testing.T) {
	s3c := fakes.NewInMemoryS3Client()
	cache := fakes.NewInMemoryCache()
	sm := state.NewStateMap()
	res := &resolverSpy{bucket: "my-bucket", prefix: "data/"}
	h := socket.NewHandler(res, s3c, sm, cache)

	resp := h.Handle(context.Background(), socket.Request{Op: "exists", Path: "/files/missing.bam"})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if resp.Exists {
		t.Fatal("expected Exists=false")
	}
}

func TestHandle_Fetch_NonBlocking(t *testing.T) {
	s3c := fakes.NewInMemoryS3Client()
	s3c.AddObject("my-bucket", "files/samples/HG001.bam", []byte("bam-data"))
	cache := fakes.NewInMemoryCache()
	sm := state.NewStateMap()
	res := &resolverSpy{bucket: "my-bucket"}
	h := socket.NewHandler(res, s3c, sm, cache)

	// Non-blocking fetch returns immediately with OK=true.
	resp := h.Handle(context.Background(), socket.Request{Op: "fetch", Path: "/files/samples/HG001.bam"})
	if !resp.OK {
		t.Fatalf("expected OK=true, got error: %s", resp.Error)
	}
	// DurationMs should be zero since it's fire-and-forget.
	if resp.DurationMs != 0 {
		t.Fatalf("expected DurationMs=0 for non-blocking fetch, got %d", resp.DurationMs)
	}
}

func TestHandle_FetchWait_Success(t *testing.T) {
	s3c := fakes.NewInMemoryS3Client()
	s3c.AddObject("my-bucket", "files/samples/HG001.bam", []byte("bam-data"))
	cache := fakes.NewInMemoryCache()
	sm := state.NewStateMap()
	res := &resolverSpy{bucket: "my-bucket"}
	h := socket.NewHandler(res, s3c, sm, cache)

	resp := h.Handle(context.Background(), socket.Request{Op: "fetch", Path: "/files/samples/HG001.bam", Wait: true})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if !resp.OK {
		t.Fatal("expected OK=true")
	}
}

func TestHandle_FetchWait_S3NotFound(t *testing.T) {
	s3c := fakes.NewInMemoryS3Client()
	cache := fakes.NewInMemoryCache()
	sm := state.NewStateMap()
	res := &resolverSpy{bucket: "my-bucket"}
	h := socket.NewHandler(res, s3c, sm, cache)

	resp := h.Handle(context.Background(), socket.Request{Op: "fetch", Path: "/files/missing.bam", Wait: true})
	if resp.OK {
		t.Fatal("expected OK=false")
	}
	if resp.Error == "" {
		t.Fatal("expected error message")
	}
}

func TestHandle_Clean_Success(t *testing.T) {
	s3c := fakes.NewInMemoryS3Client()
	s3c.AddObject("my-bucket", "files/samples/HG001.bam", []byte("bam-data"))
	c := fakes.NewInMemoryCache()
	sm := state.NewStateMap()
	res := &resolverSpy{bucket: "my-bucket"}
	h := socket.NewHandler(res, s3c, sm, c)

	// Fetch first (blocking so the file is cached before clean)
	h.Handle(context.Background(), socket.Request{Op: "fetch", Path: "/files/samples/HG001.bam", Wait: true})

	// Then clean
	resp := h.Handle(context.Background(), socket.Request{Op: "clean", Path: "/files/samples/HG001.bam"})
	if !resp.OK {
		t.Fatalf("expected OK=true, got error: %s", resp.Error)
	}
}

func TestHandle_UnknownOp(t *testing.T) {
	s3c := fakes.NewInMemoryS3Client()
	c := fakes.NewInMemoryCache()
	sm := state.NewStateMap()
	res := &resolverSpy{bucket: "my-bucket"}
	h := socket.NewHandler(res, s3c, sm, c)

	resp := h.Handle(context.Background(), socket.Request{Op: "bogus", Path: "/files/x.bam"})
	if resp.Error == "" {
		t.Fatal("expected error for unknown op")
	}
}

func TestHandle_Fetch_Deduplication(t *testing.T) {
	// Only one actual S3 Get should be called even with concurrent fetches
	s3c := fakes.NewInMemoryS3Client()
	s3c.AddObject("my-bucket", "files/samples/HG001.bam", []byte("bam-data"))
	getCount := 0
	var mu sync.Mutex
	dec := &fakes.S3ClientDecorator{
		Delegate: s3c,
		GetFunc: func(ctx context.Context, bucket, key string, dst io.Writer) error {
			mu.Lock()
			getCount++
			mu.Unlock()
			return s3c.Get(ctx, bucket, key, dst)
		},
	}
	c := fakes.NewInMemoryCache()
	sm := state.NewStateMap()
	res := &resolverSpy{bucket: "my-bucket"}
	h := socket.NewHandler(res, dec, sm, c)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.Handle(context.Background(), socket.Request{Op: "fetch", Path: "/files/samples/HG001.bam", Wait: true})
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if getCount != 1 {
		t.Fatalf("expected 1 S3 Get, got %d", getCount)
	}
}

func TestHandle_Fetch_TransientError_AllowsRetry(t *testing.T) {
	s3c := fakes.NewInMemoryS3Client()
	s3c.AddObject("my-bucket", "files/samples/HG001.bam", []byte("bam-data"))
	callCount := 0
	dec := &fakes.S3ClientDecorator{
		Delegate: s3c,
		GetFunc: func(ctx context.Context, bucket, key string, dst io.Writer) error {
			callCount++
			if callCount == 1 {
				return fmt.Errorf("%w: network error", s3client.ErrTransient)
			}
			return s3c.Get(ctx, bucket, key, dst)
		},
	}
	c := fakes.NewInMemoryCache()
	sm := state.NewStateMap()
	res := &resolverSpy{bucket: "my-bucket"}
	h := socket.NewHandler(res, dec, sm, c)

	resp1 := h.Handle(context.Background(), socket.Request{Op: "fetch", Path: "/files/samples/HG001.bam", Wait: true})
	if resp1.OK {
		t.Fatal("first fetch should fail")
	}

	resp2 := h.Handle(context.Background(), socket.Request{Op: "fetch", Path: "/files/samples/HG001.bam", Wait: true})
	if !resp2.OK {
		t.Fatalf("second fetch should succeed, got error: %s", resp2.Error)
	}
}

func TestHandle_Fetch_PermanentError_BlocksRetry(t *testing.T) {
	s3c := fakes.NewInMemoryS3Client()
	callCount := 0
	dec := &fakes.S3ClientDecorator{
		Delegate: s3c,
		GetFunc: func(ctx context.Context, bucket, key string, dst io.Writer) error {
			callCount++
			return fmt.Errorf("%w: missing", s3client.ErrNotFound)
		},
	}
	c := fakes.NewInMemoryCache()
	sm := state.NewStateMap()
	res := &resolverSpy{bucket: "my-bucket"}
	h := socket.NewHandler(res, dec, sm, c)

	resp1 := h.Handle(context.Background(), socket.Request{Op: "fetch", Path: "/files/samples/missing.bam", Wait: true})
	if resp1.OK {
		t.Fatal("first fetch should fail")
	}
	if resp1.Error == "" {
		t.Fatal("should have error message")
	}

	resp2 := h.Handle(context.Background(), socket.Request{Op: "fetch", Path: "/files/samples/missing.bam", Wait: true})
	if resp2.OK {
		t.Fatal("second fetch should also fail")
	}
	if callCount != 1 {
		t.Fatalf("expected S3 Get called only once, got %d", callCount)
	}
}

func TestHandle_Status(t *testing.T) {
	s3c := fakes.NewInMemoryS3Client()
	s3c.AddObject("my-bucket", "files/samples/HG001.bam", []byte("bam-data"))
	s3c.AddObject("my-bucket", "files/samples/HG002.bam", []byte("more-data"))
	c := fakes.NewInMemoryCache()
	sm := state.NewStateMap()
	res := &resolverSpy{bucket: "my-bucket"}
	h := socket.NewHandler(res, s3c, sm, c)

	// Fetch two files so they show up in status.
	h.Handle(context.Background(), socket.Request{Op: "fetch", Path: "/files/samples/HG001.bam", Wait: true})
	h.Handle(context.Background(), socket.Request{Op: "fetch", Path: "/files/samples/HG002.bam", Wait: true})

	resp := h.Handle(context.Background(), socket.Request{Op: "status"})
	if !resp.OK {
		t.Fatalf("expected OK=true, got error: %s", resp.Error)
	}
	if resp.FilesCached != 2 {
		t.Fatalf("expected FilesCached=2, got %d", resp.FilesCached)
	}
	if resp.FilesFetching != 0 {
		t.Fatalf("expected FilesFetching=0, got %d", resp.FilesFetching)
	}
	wantBytes := int64(len("bam-data") + len("more-data"))
	if resp.CacheBytesUsed != wantBytes {
		t.Fatalf("expected CacheBytesUsed=%d, got %d", wantBytes, resp.CacheBytesUsed)
	}
}

func TestHandle_Clean_ThenRefetch(t *testing.T) {
	s3c := fakes.NewInMemoryS3Client()
	s3c.AddObject("my-bucket", "files/samples/HG001.bam", []byte("bam-data"))
	c := fakes.NewInMemoryCache()
	sm := state.NewStateMap()
	res := &resolverSpy{bucket: "my-bucket"}
	h := socket.NewHandler(res, s3c, sm, c)

	// Fetch (blocking)
	resp1 := h.Handle(context.Background(), socket.Request{Op: "fetch", Path: "/files/samples/HG001.bam", Wait: true})
	if !resp1.OK {
		t.Fatalf("first fetch failed: %s", resp1.Error)
	}

	// Clean
	resp2 := h.Handle(context.Background(), socket.Request{Op: "clean", Path: "/files/samples/HG001.bam"})
	if !resp2.OK {
		t.Fatalf("clean failed: %s", resp2.Error)
	}

	// Refetch (blocking)
	resp3 := h.Handle(context.Background(), socket.Request{Op: "fetch", Path: "/files/samples/HG001.bam", Wait: true})
	if !resp3.OK {
		t.Fatalf("refetch failed: %s", resp3.Error)
	}
}
