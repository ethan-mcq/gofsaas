package fakes_test

import (
	"strings"
	"testing"

	"github.com/your-org/gofsaas/pkg/cache"
	"github.com/your-org/gofsaas/pkg/fakes"
)

// CacheContract is a reusable test suite for any Cache implementation.
type CacheContract struct {
	NewCache func() cache.Cache
}

func (cc CacheContract) Test(t *testing.T) {
	t.Helper()
	t.Run("write then IsCached returns true", func(t *testing.T) {
		sut := cc.NewCache()
		sut.Write("a.bam", strings.NewReader("data"))
		if !sut.IsCached("a.bam") {
			t.Fatal("expected cached")
		}
	})
	t.Run("delete removes the file", func(t *testing.T) {
		sut := cc.NewCache()
		sut.Write("a.bam", strings.NewReader("data"))
		sut.Delete("a.bam")
		if sut.IsCached("a.bam") {
			t.Fatal("expected not cached")
		}
	})
	t.Run("delete fails if open refs > 0", func(t *testing.T) {
		sut := cc.NewCache()
		sut.Write("a.bam", strings.NewReader("data"))
		sut.OpenRef("a.bam")
		_, err := sut.Delete("a.bam")
		if err == nil {
			t.Fatal("expected error with open refs")
		}
	})
	t.Run("delete succeeds after all refs closed", func(t *testing.T) {
		sut := cc.NewCache()
		sut.Write("a.bam", strings.NewReader("data"))
		sut.OpenRef("a.bam")
		sut.CloseRef("a.bam")
		_, err := sut.Delete("a.bam")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("LocalPath is stable and deterministic", func(t *testing.T) {
		sut := cc.NewCache()
		p1 := sut.LocalPath("samples/HG001.bam")
		p2 := sut.LocalPath("samples/HG001.bam")
		if p1 != p2 {
			t.Fatal("LocalPath must be deterministic")
		}
	})
}

func TestFakeCache(t *testing.T) {
	CacheContract{
		NewCache: func() cache.Cache {
			return fakes.NewInMemoryCache()
		},
	}.Test(t)
}
