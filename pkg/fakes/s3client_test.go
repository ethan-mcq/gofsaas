package fakes_test

import (
	"context"
	"errors"
	"testing"

	"github.com/your-org/gofsaas/pkg/fakes"
	"github.com/your-org/gofsaas/pkg/s3client"
)

func TestInMemoryS3Client_HeadFound(t *testing.T) {
	c := fakes.NewInMemoryS3Client()
	c.AddObject("bucket", "key/file.bam", []byte("content"))
	meta, err := c.Head(context.Background(), "bucket", "key/file.bam")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.Size != int64(len("content")) {
		t.Fatalf("size mismatch: got %d want %d", meta.Size, len("content"))
	}
}

func TestInMemoryS3Client_HeadNotFound(t *testing.T) {
	c := fakes.NewInMemoryS3Client()
	_, err := c.Head(context.Background(), "bucket", "missing.bam")
	if !errors.Is(err, s3client.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
