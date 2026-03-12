package s3client

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// RealClient is a production S3 client backed by AWS SDK v2.
type RealClient struct {
	s3 *s3.Client
}

// NewRealClient creates a RealClient from a configured AWS S3 client.
func NewRealClient(s3c *s3.Client) *RealClient {
	return &RealClient{s3: s3c}
}

// Head performs a HeadObject request.
func (c *RealClient) Head(ctx context.Context, bucket, key string) (ObjectMeta, error) {
	out, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isS3NotFound(err) {
			return ObjectMeta{}, fmt.Errorf("%w: %s/%s", ErrNotFound, bucket, key)
		}
		return ObjectMeta{}, fmt.Errorf("%w: head %s/%s: %v", ErrTransient, bucket, key, err)
	}
	size := int64(0)
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	etag := ""
	if out.ETag != nil {
		etag = *out.ETag
	}
	return ObjectMeta{Key: key, Size: size, ETag: etag}, nil
}

// Get downloads an object and writes to dst.
func (c *RealClient) Get(ctx context.Context, bucket, key string, dst io.Writer) error {
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isS3NotFound(err) {
			return fmt.Errorf("%w: %s/%s", ErrNotFound, bucket, key)
		}
		return fmt.Errorf("%w: get %s/%s: %v", ErrTransient, bucket, key, err)
	}
	defer out.Body.Close()
	if _, err := io.Copy(dst, out.Body); err != nil {
		return fmt.Errorf("%w: read body %s/%s: %v", ErrTransient, bucket, key, err)
	}
	return nil
}

// List lists objects with a given prefix.
func (c *RealClient) List(ctx context.Context, bucket, prefix string) ([]ObjectMeta, error) {
	var results []ObjectMeta
	paginator := s3.NewListObjectsV2Paginator(c.s3, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: list %s/%s: %v", ErrTransient, bucket, prefix, err)
		}
		for _, obj := range page.Contents {
			etag := ""
			if obj.ETag != nil {
				etag = *obj.ETag
			}
			size := int64(0)
			if obj.Size != nil {
				size = *obj.Size
			}
			key := ""
			if obj.Key != nil {
				key = *obj.Key
			}
			results = append(results, ObjectMeta{Key: key, Size: size, ETag: etag})
		}
	}
	return results, nil
}

func isS3NotFound(err error) bool {
	if err == nil {
		return false
	}
	var notFound *types.NoSuchKey
	if errors.As(err, &notFound) {
		return true
	}
	var notFoundBucket *types.NoSuchBucket
	if errors.As(err, &notFoundBucket) {
		return true
	}
	return false
}
