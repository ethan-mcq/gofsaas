package fakes

import (
	"context"
	"io"

	"github.com/your-org/gofsaas/pkg/s3client"
)

// S3ClientDecorator wraps any s3client.Client and allows individual methods
// to be overridden in a test without modifying the underlying fake.
type S3ClientDecorator struct {
	Delegate s3client.Client
	HeadFunc func(ctx context.Context, bucket, key string) (s3client.ObjectMeta, error)
	GetFunc  func(ctx context.Context, bucket, key string, dst io.Writer) error
	ListFunc func(ctx context.Context, bucket, prefix string) ([]s3client.ObjectMeta, error)
}

var _ s3client.Client = &S3ClientDecorator{}

// Head calls HeadFunc if set, otherwise delegates.
func (d *S3ClientDecorator) Head(ctx context.Context, bucket, key string) (s3client.ObjectMeta, error) {
	if d.HeadFunc != nil {
		return d.HeadFunc(ctx, bucket, key)
	}
	return d.Delegate.Head(ctx, bucket, key)
}

// Get calls GetFunc if set, otherwise delegates.
func (d *S3ClientDecorator) Get(ctx context.Context, bucket, key string, dst io.Writer) error {
	if d.GetFunc != nil {
		return d.GetFunc(ctx, bucket, key, dst)
	}
	return d.Delegate.Get(ctx, bucket, key, dst)
}

// List calls ListFunc if set, otherwise delegates.
func (d *S3ClientDecorator) List(ctx context.Context, bucket, prefix string) ([]s3client.ObjectMeta, error) {
	if d.ListFunc != nil {
		return d.ListFunc(ctx, bucket, prefix)
	}
	return d.Delegate.List(ctx, bucket, prefix)
}
