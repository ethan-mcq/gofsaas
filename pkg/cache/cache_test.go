package cache_test

import (
	"os"
	"strings"
	"testing"

	"github.com/your-org/gofsaas/pkg/cache"
)

func TestWrite_CreatesFileWithContent(t *testing.T) {
	c := cache.New(t.TempDir())
	err := c.Write("samples/HG001.bam", strings.NewReader("data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c.IsCached("samples/HG001.bam") {
		t.Fatal("expected file to be cached")
	}
	p := c.LocalPath("samples/HG001.bam")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(b) != "data" {
		t.Fatalf("content: got %q want %q", string(b), "data")
	}
}

func TestIsCached_ReturnsFalseBeforeWrite(t *testing.T) {
	c := cache.New(t.TempDir())
	if c.IsCached("samples/HG001.bam") {
		t.Fatal("expected not cached before write")
	}
}

func TestDelete_FreesBytes(t *testing.T) {
	c := cache.New(t.TempDir())
	c.Write("samples/HG001.bam", strings.NewReader("data"))
	freed, err := c.Delete("samples/HG001.bam")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if freed != int64(len("data")) {
		t.Fatalf("freed: got %d want %d", freed, len("data"))
	}
	if c.IsCached("samples/HG001.bam") {
		t.Fatal("expected not cached after delete")
	}
}

func TestDelete_RefusesIfOpenRefs(t *testing.T) {
	c := cache.New(t.TempDir())
	c.Write("f.bam", strings.NewReader("x"))
	c.OpenRef("f.bam")
	_, err := c.Delete("f.bam")
	if err == nil {
		t.Fatal("expected error with open refs")
	}
}

func TestDelete_SucceedsAfterRefClosed(t *testing.T) {
	c := cache.New(t.TempDir())
	c.Write("f.bam", strings.NewReader("x"))
	c.OpenRef("f.bam")
	c.CloseRef("f.bam")
	_, err := c.Delete("f.bam")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
