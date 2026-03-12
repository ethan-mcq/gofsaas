package resolver_test

import (
	"errors"
	"testing"

	"github.com/your-org/gofsaas/pkg/resolver"
)

func TestResolve_HappyPath(t *testing.T) {
	r := resolver.New("/files", "my-bucket", "data/")
	bucket, key, err := r.Resolve("/files/samples/HG001.bam")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bucket != "my-bucket" {
		t.Fatalf("bucket: got %q want %q", bucket, "my-bucket")
	}
	if key != "data/samples/HG001.bam" {
		t.Fatalf("key: got %q want %q", key, "data/samples/HG001.bam")
	}
}

func TestResolve_MountRootTrailingSlash(t *testing.T) {
	r := resolver.New("/files/", "my-bucket", "data/")
	_, _, err := r.Resolve("/files/foo.bam")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolve_PathNotUnderMount(t *testing.T) {
	r := resolver.New("/files", "my-bucket", "data/")
	_, _, err := r.Resolve("/other/foo.bam")
	if !errors.Is(err, resolver.ErrNotMounted) {
		t.Fatalf("expected ErrNotMounted, got %v", err)
	}
}

func TestResolve_ExactMountRoot(t *testing.T) {
	r := resolver.New("/files", "my-bucket", "data/")
	_, key, err := r.Resolve("/files")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "data/" {
		t.Fatalf("key: got %q want %q", key, "data/")
	}
}
