package fuse

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	bazil "bazil.org/fuse"
	"bazil.org/fuse/fs"

	"github.com/your-org/gofsaas/pkg/cache"
	"github.com/your-org/gofsaas/pkg/resolver"
	"github.com/your-org/gofsaas/pkg/s3client"
	"github.com/your-org/gofsaas/pkg/state"
)

// Config holds the parameters for the FUSE filesystem.
type Config struct {
	MountPoint           string
	Bucket               string
	Prefix               string
	CacheDir             string
	MaxConcurrentFetches int
	AttrCacheTTL         time.Duration // defaults to 30s if zero
}

type attrCacheEntry struct {
	meta   s3client.ObjectMeta
	expiry time.Time
}

// FS is the root FUSE filesystem object. It implements fs.FS.
type FS struct {
	resolver     resolver.Resolver
	mountPath    string
	s3           s3client.Client
	sm           state.StateMap
	cache        cache.Cache
	sem          chan struct{} // fetch concurrency limiter
	attrMu       sync.RWMutex
	attrCache    map[string]attrCacheEntry
	attrCacheTTL time.Duration
}

// NewFS creates a FS with the given dependencies.
func NewFS(r resolver.Resolver, mountPath string, s3c s3client.Client, sm state.StateMap, c cache.Cache, maxConcurrent int, attrCacheTTL time.Duration) *FS {
	if maxConcurrent <= 0 {
		maxConcurrent = 8
	}
	if attrCacheTTL <= 0 {
		attrCacheTTL = 30 * time.Second
	}
	return &FS{
		resolver:     r,
		mountPath:    strings.TrimRight(mountPath, "/"),
		s3:           s3c,
		sm:           sm,
		cache:        c,
		sem:          make(chan struct{}, maxConcurrent),
		attrCache:    make(map[string]attrCacheEntry),
		attrCacheTTL: attrCacheTTL,
	}
}

// Cache returns the underlying cache, for testing.
func (f *FS) Cache() cache.Cache {
	return f.cache
}

// Root returns the root node of the filesystem.
func (f *FS) Root() (fs.Node, error) {
	return &Dir{fs: f, absPath: f.mountPath}, nil
}

// Mount mounts the FUSE filesystem at mountPoint and serves requests until ctx is cancelled.
func Mount(ctx context.Context, cfg Config, fuseFS *FS) error {
	c, err := bazil.Mount(
		cfg.MountPoint,
		bazil.FSName("gofsaas"),
		bazil.Subtype("gofsaas"),
		bazil.ReadOnly(),
	)
	if err != nil {
		return fmt.Errorf("fuse.Mount %s: %w", cfg.MountPoint, err)
	}
	defer c.Close()

	go func() {
		<-ctx.Done()
		bazil.Unmount(cfg.MountPoint)
	}()

	if err := fs.Serve(c, fuseFS); err != nil {
		return fmt.Errorf("fuse.Serve: %w", err)
	}
	return nil
}

// Dir represents a directory node in the FUSE filesystem.
type Dir struct {
	fs      *FS
	absPath string
}

var _ fs.Node = &Dir{}
var _ fs.HandleReadDirAller = &Dir{}

// Attr fills in the attributes for a directory node.
func (d *Dir) Attr(ctx context.Context, a *bazil.Attr) error {
	a.Mode = os.ModeDir | 0555
	return nil
}

// ReadDirAll lists the children of this directory from S3.
func (d *Dir) ReadDirAll(ctx context.Context) ([]bazil.Dirent, error) {
	entries, err := ListDir(ctx, d.fs.resolver, d.fs.s3, d.absPath)
	if err != nil {
		log.Printf("fuse.ReadDirAll %s: %v", d.absPath, err)
		return nil, syscall.EIO
	}
	return entries, nil
}

// Lookup finds a child node by name.
func (d *Dir) Lookup(ctx context.Context, name string) (fs.Node, error) {
	return &File{fs: d.fs, absPath: d.absPath + "/" + name}, nil
}

// File represents a file node in the FUSE filesystem.
type File struct {
	fs      *FS
	absPath string
}

var _ fs.Node = &File{}
var _ fs.NodeOpener = &File{}

// Attr fills in the attributes for a file node, using the attr cache when possible.
func (f *File) Attr(ctx context.Context, a *bazil.Attr) error {
	meta, err := StatFileCached(ctx, f.fs, f.absPath)
	if err != nil {
		return syscall.ENOENT
	}
	a.Mode = 0444
	a.Size = uint64(meta.Size)
	return nil
}

// Open triggers a fetch and opens the cached file.
func (f *File) Open(ctx context.Context, req *bazil.OpenRequest, resp *bazil.OpenResponse) (fs.Handle, error) {
	if err := FetchFile(ctx, f.fs, f.absPath); err != nil {
		log.Printf("fuse.Open fetch %s: %v", f.absPath, err)
		return nil, syscall.EIO
	}
	mountRelPath := absToRelPath(f.absPath)
	localPath := f.fs.cache.LocalPath(mountRelPath)
	fh, err := os.Open(localPath)
	if err != nil {
		log.Printf("fuse.Open local open %s: %v", localPath, err)
		return nil, syscall.EIO
	}
	f.fs.cache.OpenRef(mountRelPath)
	return &Handle{file: f, fh: fh, mountRelPath: mountRelPath}, nil
}

// Handle is an open file handle.
type Handle struct {
	file         *File
	fh           *os.File
	mountRelPath string
}

var _ fs.Handle = &Handle{}
var _ fs.HandleReader = &Handle{}
var _ fs.HandleReleaser = &Handle{}

// Read reads from the cached file.
func (h *Handle) Read(ctx context.Context, req *bazil.ReadRequest, resp *bazil.ReadResponse) error {
	buf := make([]byte, req.Size)
	n, err := h.fh.ReadAt(buf, req.Offset)
	resp.Data = buf[:n]
	if err != nil && err.Error() != "EOF" {
		return syscall.EIO
	}
	return nil
}

// Release closes the file handle and decrements the ref count.
func (h *Handle) Release(ctx context.Context, req *bazil.ReleaseRequest) error {
	h.fh.Close()
	h.file.fs.cache.CloseRef(h.mountRelPath)
	return nil
}

// absToRelPath strips the leading slash from an absolute path.
func absToRelPath(absPath string) string {
	if len(absPath) > 0 && absPath[0] == '/' {
		return absPath[1:]
	}
	return absPath
}
