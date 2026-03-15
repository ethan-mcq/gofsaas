package gofsaas_test

import (
	"testing"

	"github.com/your-org/gofsaas/pkg/fakes"
	"github.com/your-org/gofsaas/pkg/gofsaas"
)

func TestClient_Exists_SendsCorrectRequest(t *testing.T) {
	srv := fakes.NewFakeSocketServer([]string{`{"exists":true,"size_bytes":42}`})
	sockPath := srv.Start(t)

	c := gofsaas.NewClient(sockPath)
	resp, err := c.Exists("/files/samples/HG001.bam")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Exists {
		t.Fatal("expected Exists=true")
	}
	if resp.SizeBytes != 42 {
		t.Fatalf("size: got %d want 42", resp.SizeBytes)
	}

	srv.Mu().Lock()
	received := srv.Received
	srv.Mu().Unlock()

	if len(received) != 1 {
		t.Fatalf("expected 1 request, got %d", len(received))
	}
	if received[0].Op != "exists" {
		t.Fatalf("expected op=exists, got %q", received[0].Op)
	}
	if received[0].Path != "/files/samples/HG001.bam" {
		t.Fatalf("expected path=/files/samples/HG001.bam, got %q", received[0].Path)
	}
}

func TestClient_Fetch_SendsCorrectRequest(t *testing.T) {
	srv := fakes.NewFakeSocketServer([]string{`{"ok":true,"duration_ms":10}`})
	sockPath := srv.Start(t)

	c := gofsaas.NewClient(sockPath)
	resp, err := c.Fetch("/files/samples/HG001.bam")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatal("expected OK=true")
	}

	srv.Mu().Lock()
	received := srv.Received
	srv.Mu().Unlock()

	if len(received) != 1 || received[0].Op != "fetch" {
		t.Fatalf("expected op=fetch, got %v", received)
	}
}

func TestClient_FetchWait_SendsCorrectRequest(t *testing.T) {
	srv := fakes.NewFakeSocketServer([]string{`{"ok":true,"duration_ms":340,"from_cache":false}`})
	sockPath := srv.Start(t)

	c := gofsaas.NewClient(sockPath)
	resp, err := c.FetchWait("/files/samples/HG001.bam")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatal("expected OK=true")
	}

	srv.Mu().Lock()
	received := srv.Received
	srv.Mu().Unlock()

	if len(received) != 1 || received[0].Op != "fetch" {
		t.Fatalf("expected op=fetch, got %v", received)
	}
	if !received[0].Wait {
		t.Fatal("expected Wait=true")
	}
}

func TestClient_Clean_SendsCorrectRequest(t *testing.T) {
	srv := fakes.NewFakeSocketServer([]string{`{"ok":true,"freed_bytes":1024}`})
	sockPath := srv.Start(t)

	c := gofsaas.NewClient(sockPath)
	resp, err := c.Clean("/files/samples/HG001.bam")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatal("expected OK=true")
	}

	srv.Mu().Lock()
	received := srv.Received
	srv.Mu().Unlock()

	if len(received) != 1 || received[0].Op != "clean" {
		t.Fatalf("expected op=clean, got %v", received)
	}
}
