package s3client

import (
	"context"
	"errors"
	"io"
)

var ErrNotFound = errors.New("not found")  // permanent — 404/403
var ErrTransient = errors.New("transient") // temporary — retry

// ObjectMeta holds S3 object metadata.
type ObjectMeta struct {
	Key  string
	Size int64
	ETag string
}

// S3Header performs a HEAD request.
type S3Header interface {
	Head(ctx context.Context, bucket, key string) (ObjectMeta, error)
}

// S3Getter downloads an object and writes it to dst.
type S3Getter interface {
	Get(ctx context.Context, bucket, key string, dst io.Writer) error
}

// S3Lister lists objects with a prefix.
type S3Lister interface {
	List(ctx context.Context, bucket, prefix string) ([]ObjectMeta, error)
}

// Client implements all three.
type Client interface {
	S3Header
	S3Getter
	S3Lister
}
