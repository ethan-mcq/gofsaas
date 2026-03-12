package resolver

import (
	"errors"
	"path"
	"strings"
)

var ErrNotMounted = errors.New("path is not under a registered mount")

// Resolver converts an absolute mount path to an S3 bucket and key.
type Resolver interface {
	Resolve(absPath string) (bucket, key string, err error)
}

type resolver struct {
	mountPath string
	bucket    string
	prefix    string
}

// New creates a Resolver with one registered mount. Trims trailing slashes from mountPath.
func New(mountPath, bucket, prefix string) Resolver {
	return &resolver{
		mountPath: strings.TrimRight(mountPath, "/"),
		bucket:    bucket,
		prefix:    prefix,
	}
}

// Resolve converts an absolute mount path to (bucket, key).
func (r *resolver) Resolve(absPath string) (bucket, key string, err error) {
	cleaned := path.Clean(absPath)
	cleanedMount := path.Clean(r.mountPath)

	if cleaned == cleanedMount {
		return r.bucket, r.prefix, nil
	}

	if !strings.HasPrefix(cleaned, cleanedMount+"/") {
		return "", "", ErrNotMounted
	}

	rel := strings.TrimPrefix(cleaned, cleanedMount+"/")
	return r.bucket, r.prefix + rel, nil
}
