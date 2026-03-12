package s3client_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/your-org/gofsaas/pkg/s3client"
)

// Verify that the error sentinels work as expected.
func TestErrSentinels(t *testing.T) {
	wrapped := errors.New("s3 error")
	notFound := fmt.Errorf("%w: %v", s3client.ErrNotFound, wrapped)
	if !errors.Is(notFound, s3client.ErrNotFound) {
		t.Fatal("expected ErrNotFound")
	}
	transient := fmt.Errorf("%w: %v", s3client.ErrTransient, wrapped)
	if !errors.Is(transient, s3client.ErrTransient) {
		t.Fatal("expected ErrTransient")
	}
}
