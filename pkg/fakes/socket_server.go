package fakes

import (
	"encoding/json"
	"net"
	"os"
	"sync"
	"testing"

	"github.com/your-org/gofsaas/pkg/socket"
)

// FakeSocketServer listens on a temp socket and returns preset JSON responses.
type FakeSocketServer struct {
	responses []string
	Received  []socket.Request
	mu        sync.Mutex
}

// NewFakeSocketServer creates a FakeSocketServer with the given preset JSON responses.
func NewFakeSocketServer(responses []string) *FakeSocketServer {
	return &FakeSocketServer{responses: responses}
}

// Mu returns the mutex protecting Received, for test synchronization.
func (f *FakeSocketServer) Mu() *sync.Mutex {
	return &f.mu
}

// Start starts the fake server and returns the socket path.
// It registers a cleanup function to close the listener and remove the socket.
func (f *FakeSocketServer) Start(t *testing.T) string {
	t.Helper()
	sockPath := t.TempDir() + "/test.sock"
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("FakeSocketServer.Start: %v", err)
	}
	t.Cleanup(func() {
		ln.Close()
		os.Remove(sockPath)
	})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.handle(conn)
		}
	}()
	return sockPath
}

func (f *FakeSocketServer) handle(conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)
	var req socket.Request
	if err := dec.Decode(&req); err != nil {
		return
	}
	f.mu.Lock()
	f.Received = append(f.Received, req)
	var resp string
	if len(f.responses) > 0 {
		resp = f.responses[0]
		f.responses = f.responses[1:]
	}
	f.mu.Unlock()
	if resp != "" {
		enc.Encode(json.RawMessage(resp))
	}
}
