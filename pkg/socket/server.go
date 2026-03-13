package socket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"

	"github.com/your-org/gofsaas/pkg/cache"
	"github.com/your-org/gofsaas/pkg/resolver"
	"github.com/your-org/gofsaas/pkg/s3client"
	"github.com/your-org/gofsaas/pkg/state"
)

// Handler handles socket requests by coordinating the resolver, S3 client,
// state map, and cache.
type Handler struct {
	resolver resolver.Resolver
	s3       s3client.Client
	sm       state.StateMap
	cache    cache.Cache
}

// NewHandler creates a Handler with the given dependencies.
func NewHandler(r resolver.Resolver, s3 s3client.Client, sm state.StateMap, c cache.Cache) *Handler {
	return &Handler{resolver: r, s3: s3, sm: sm, cache: c}
}

// Handle dispatches a Request to the appropriate handler and returns a Response.
func (h *Handler) Handle(ctx context.Context, req Request) Response {
	switch req.Op {
	case "exists":
		return h.handleExists(ctx, req)
	case "fetch":
		return h.handleFetch(ctx, req)
	case "clean":
		return h.handleClean(req)
	case "status":
		return h.handleStatus()
	default:
		return Response{Error: fmt.Sprintf("unknown op: %q", req.Op)}
	}
}

func (h *Handler) handleExists(ctx context.Context, req Request) Response {
	bucket, key, err := h.resolver.Resolve(req.Path)
	if err != nil {
		return Response{Error: fmt.Sprintf("resolve %s: %v", req.Path, err)}
	}
	meta, err := h.s3.Head(ctx, bucket, key)
	if err != nil {
		if errors.Is(err, s3client.ErrNotFound) {
			return Response{Exists: false}
		}
		return Response{Error: fmt.Sprintf("s3.Head %s/%s: %v", bucket, key, err)}
	}
	mountRelPath := absToRelPath(req.Path)
	cached := h.cache.IsCached(mountRelPath)
	return Response{Exists: true, SizeBytes: meta.Size, Cached: cached}
}

func (h *Handler) fetchFn(ctx context.Context, req Request) func() error {
	mountRelPath := absToRelPath(req.Path)
	return func() error {
		bucket, key, resolveErr := h.resolver.Resolve(req.Path)
		if resolveErr != nil {
			return fmt.Errorf("resolve %s: %w", req.Path, resolveErr)
		}
		pr, pw := io.Pipe()
		var getErr error
		go func() {
			getErr = h.s3.Get(ctx, bucket, key, pw)
			pw.CloseWithError(getErr)
		}()
		writeErr := h.cache.Write(mountRelPath, pr)
		if getErr != nil {
			return getErr
		}
		return writeErr
	}
}

func (h *Handler) handleFetch(ctx context.Context, req Request) Response {
	if req.Wait {
		return h.handleFetchWait(ctx, req)
	}

	// Fire-and-forget: spawn the fetch in a background goroutine.
	go h.sm.Fetch(req.Path, h.fetchFn(ctx, req))

	return Response{OK: true}
}

func (h *Handler) handleFetchWait(ctx context.Context, req Request) Response {
	start := time.Now()
	mountRelPath := absToRelPath(req.Path)

	err := h.sm.Fetch(req.Path, h.fetchFn(ctx, req))
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}

	return Response{
		OK:         true,
		FromCache:  h.cache.IsCached(mountRelPath),
		DurationMs: time.Since(start).Milliseconds(),
	}
}

func (h *Handler) handleClean(req Request) Response {
	mountRelPath := absToRelPath(req.Path)
	freed, err := h.cache.Delete(mountRelPath)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	h.sm.Reset(req.Path)
	return Response{OK: true, FreedBytes: freed}
}

func (h *Handler) handleStatus() Response {
	return Response{OK: true}
}

// absToRelPath strips the leading slash from an absolute path to use as a
// cache-relative path.
func absToRelPath(absPath string) string {
	if len(absPath) > 0 && absPath[0] == '/' {
		return absPath[1:]
	}
	return absPath
}

// Server listens on a Unix socket and dispatches requests to a Handler.
type Server struct {
	handler  *Handler
	sockPath string
}

// NewServer creates a Server that listens on sockPath.
func NewServer(sockPath string, handler *Handler) *Server {
	return &Server{handler: handler, sockPath: sockPath}
}

// Run starts the Unix socket server and blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	os.Remove(s.sockPath)
	ln, err := net.Listen("unix", s.sockPath)
	if err != nil {
		return fmt.Errorf("socket.Server listen %s: %w", s.sockPath, err)
	}
	defer ln.Close()
	defer os.Remove(s.sockPath)

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				log.Printf("socket.Server accept: %v", err)
				continue
			}
		}
		go s.serveConn(ctx, conn)
	}
}

func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	var req Request
	if err := dec.Decode(&req); err != nil {
		if err != io.EOF {
			log.Printf("socket.Server decode: %v", err)
		}
		return
	}

	resp := s.handler.Handle(ctx, req)
	if err := enc.Encode(resp); err != nil {
		log.Printf("socket.Server encode: %v", err)
	}
}
