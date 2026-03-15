package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/your-org/gofsaas/pkg/cache"
	fusepkg "github.com/your-org/gofsaas/pkg/fuse"
	"github.com/your-org/gofsaas/pkg/gofsaas"
	"github.com/your-org/gofsaas/pkg/resolver"
	"github.com/your-org/gofsaas/pkg/s3client"
	"github.com/your-org/gofsaas/pkg/socket"
	"github.com/your-org/gofsaas/pkg/state"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	var err error
	switch cmd {
	case "mount":
		err = runMount(os.Args[2:])
	case "exists":
		err = runExists(os.Args[2:])
	case "fetch":
		err = runFetch(os.Args[2:])
	case "clean":
		err = runClean(os.Args[2:])
	case "status":
		err = runStatus(os.Args[2:])
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %q\n", cmd)
		printUsage()
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `gofsaas - demand-driven, blocking FUSE filesystem for S3

Usage:
  gofsaas mount   --mount <path> --bucket <name> --prefix <prefix> --cache-dir <dir> [--socket <path>]
  gofsaas exists  [--socket <path>] <file-path>
  gofsaas fetch   [--socket <path>] <file-path>
  gofsaas clean   [--socket <path>] <file-path>
  gofsaas status  [--socket <path>]

Socket default (in priority order):
  1. $GOFSAAS_SOCKET environment variable
  2. $XDG_RUNTIME_DIR/gofsaas.sock
  3. ~/.gofsaas/gofsaas.sock
`)
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// defaultSocketPath returns the default Unix socket path.
// Prefers $GOFSAAS_SOCKET, then $XDG_RUNTIME_DIR/gofsaas.sock,
// then ~/.gofsaas/gofsaas.sock.
func defaultSocketPath() string {
	if v := os.Getenv("GOFSAAS_SOCKET"); v != "" {
		return v
	}
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		return xdg + "/gofsaas.sock"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/gofsaas.sock"
	}
	return home + "/.gofsaas/gofsaas.sock"
}

func runMount(args []string) error {
	var mountPath, bucket, prefix, cacheDir, sockPath string
	var maxConcurrent int

	// Simple arg parsing (flag-style)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--mount":
			i++
			mountPath = args[i]
		case "--bucket":
			i++
			bucket = args[i]
		case "--prefix":
			i++
			prefix = args[i]
		case "--cache-dir":
			i++
			cacheDir = args[i]
		case "--socket":
			i++
			sockPath = args[i]
		case "--max-concurrent":
			i++
			fmt.Sscanf(args[i], "%d", &maxConcurrent)
		}
	}

	if mountPath == "" {
		mountPath = envOrDefault("GOFSAAS_MOUNT", "")
	}
	if bucket == "" {
		bucket = envOrDefault("GOFSAAS_BUCKET", "")
	}
	if prefix == "" {
		prefix = envOrDefault("GOFSAAS_PREFIX", "")
	}
	if cacheDir == "" {
		cacheDir = envOrDefault("GOFSAAS_CACHE_DIR", "/tmp/gofsaas-cache")
	}
	if sockPath == "" {
		sockPath = defaultSocketPath()
	}
	if maxConcurrent == 0 {
		maxConcurrent = 8
	}

	if mountPath == "" || bucket == "" {
		return fmt.Errorf("--mount and --bucket are required")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Load AWS config
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	s3c := s3client.NewRealClient(awss3.NewFromConfig(awsCfg))
	r := resolver.New(mountPath, bucket, prefix)
	sm := state.NewStateMap()
	c := cache.New(cacheDir)

	fuseFS := fusepkg.NewFS(r, s3c, sm, c, maxConcurrent, 30*time.Second)

	cfg := fusepkg.Config{
		MountPoint:          mountPath,
		Bucket:              bucket,
		Prefix:              prefix,
		CacheDir:            cacheDir,
		MaxConcurrentFetches: maxConcurrent,
	}

	// Ensure the socket directory exists before binding.
	if err := os.MkdirAll(socketDir(sockPath), 0700); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}

	// Start socket server in background
	handler := socket.NewHandler(r, s3c, sm, c)
	srv := socket.NewServer(sockPath, handler)
	go func() {
		if err := srv.Run(ctx); err != nil {
			log.Printf("socket server: %v", err)
		}
	}()

	log.Printf("gofsaas: mounting s3://%s/%s at %s", bucket, prefix, mountPath)
	return fusepkg.Mount(ctx, cfg, fuseFS)
}

func runExists(args []string) error {
	sockPath, path, err := parseSockAndPath(args)
	if err != nil {
		return err
	}
	c := gofsaas.NewClient(sockPath)
	resp, err := c.Exists(path)
	if err != nil {
		return fmt.Errorf("exists: %w", err)
	}
	return printJSON(resp)
}

func runFetch(args []string) error {
	sockPath, path, err := parseSockAndPath(args)
	if err != nil {
		return err
	}
	c := gofsaas.NewClient(sockPath)
	resp, err := c.Fetch(path)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	return printJSON(resp)
}

func runClean(args []string) error {
	sockPath, path, err := parseSockAndPath(args)
	if err != nil {
		return err
	}
	c := gofsaas.NewClient(sockPath)
	resp, err := c.Clean(path)
	if err != nil {
		return fmt.Errorf("clean: %w", err)
	}
	return printJSON(resp)
}

func runStatus(args []string) error {
	sockPath := defaultSocketPath()
	for i := 0; i < len(args); i++ {
		if args[i] == "--socket" && i+1 < len(args) {
			i++
			sockPath = args[i]
		}
	}
	c := gofsaas.NewClient(sockPath)
	resp, err := c.Status()
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}
	return printJSON(resp)
}

func parseSockAndPath(args []string) (sockPath, filePath string, err error) {
	sockPath = defaultSocketPath()
	for i := 0; i < len(args); i++ {
		if args[i] == "--socket" && i+1 < len(args) {
			i++
			sockPath = args[i]
		} else if args[i] != "" && args[i][0] != '-' {
			filePath = args[i]
		}
	}
	if filePath == "" {
		return "", "", fmt.Errorf("file path argument is required")
	}
	return sockPath, filePath, nil
}

// socketDir returns the directory containing the socket path.
func socketDir(sockPath string) string {
	for i := len(sockPath) - 1; i >= 0; i-- {
		if sockPath[i] == '/' {
			return sockPath[:i]
		}
	}
	return "."
}

func printJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
