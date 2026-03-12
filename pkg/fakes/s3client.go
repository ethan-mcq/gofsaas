package fakes

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/your-org/gofsaas/pkg/s3client"
)

// InMemoryS3Client is a fake s3client.Client for use in tests.
type InMemoryS3Client struct {
	objects map[string][]byte // "bucket/key" → content
}

// NewInMemoryS3Client creates a new InMemoryS3Client.
func NewInMemoryS3Client() *InMemoryS3Client {
	return &InMemoryS3Client{
		objects: make(map[string][]byte),
	}
}

// AddObject adds an object to the fake store.
func (c *InMemoryS3Client) AddObject(bucket, key string, data []byte) {
	c.objects[bucket+"/"+key] = data
}

// Head returns metadata for the object, or ErrNotFound.
func (c *InMemoryS3Client) Head(_ context.Context, bucket, key string) (s3client.ObjectMeta, error) {
	data, ok := c.objects[bucket+"/"+key]
	if !ok {
		return s3client.ObjectMeta{}, fmt.Errorf("%w: %s/%s", s3client.ErrNotFound, bucket, key)
	}
	return s3client.ObjectMeta{Key: key, Size: int64(len(data))}, nil
}

// Get writes the object content to dst, or returns ErrNotFound.
func (c *InMemoryS3Client) Get(_ context.Context, bucket, key string, dst io.Writer) error {
	data, ok := c.objects[bucket+"/"+key]
	if !ok {
		return fmt.Errorf("%w: %s/%s", s3client.ErrNotFound, bucket, key)
	}
	_, err := io.Copy(dst, strings.NewReader(string(data)))
	return err
}

// List returns all objects whose keys match the given prefix.
func (c *InMemoryS3Client) List(_ context.Context, bucket, prefix string) ([]s3client.ObjectMeta, error) {
	var results []s3client.ObjectMeta
	bk := bucket + "/"
	for k, data := range c.objects {
		if !strings.HasPrefix(k, bk) {
			continue
		}
		key := strings.TrimPrefix(k, bk)
		if strings.HasPrefix(key, prefix) {
			results = append(results, s3client.ObjectMeta{Key: key, Size: int64(len(data))})
		}
	}
	return results, nil
}
