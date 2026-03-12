package state_test

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/your-org/gofsaas/pkg/s3client"
	"github.com/your-org/gofsaas/pkg/state"
)

type FetchFnSpy struct {
	CallCount    int
	ReturnErrors []error
	BeforeFn     func() // called before returning, used as a barrier in tests
	mu           sync.Mutex
}

func (s *FetchFnSpy) Fn() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CallCount++
	var err error
	if len(s.ReturnErrors) > 0 {
		err = s.ReturnErrors[0]
		s.ReturnErrors = s.ReturnErrors[1:]
	}
	if s.BeforeFn != nil {
		s.mu.Unlock()
		s.BeforeFn()
		s.mu.Lock()
	}
	return err
}

func TestFetch_FirstCallerOwnsFetch(t *testing.T) {
	sm := state.NewStateMap()
	spy := &FetchFnSpy{}
	err := sm.Fetch("/files/a.bam", spy.Fn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spy.CallCount != 1 {
		t.Fatalf("expected CallCount 1, got %d", spy.CallCount)
	}
}

func TestFetch_AlreadyCached_SkipsFetch(t *testing.T) {
	sm := state.NewStateMap()
	spy := &FetchFnSpy{}
	sm.Fetch("/files/a.bam", spy.Fn)
	sm.Fetch("/files/a.bam", spy.Fn)
	if spy.CallCount != 1 {
		t.Fatalf("expected CallCount 1, got %d", spy.CallCount)
	}
}

func TestFetch_ConcurrentCallersJoinSingleFlight(t *testing.T) {
	sm := state.NewStateMap()
	spy := &FetchFnSpy{}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); sm.Fetch("/files/b.bam", spy.Fn) }()
	}
	wg.Wait()
	if spy.CallCount != 1 {
		t.Fatalf("expected CallCount 1, got %d", spy.CallCount)
	}
}

func TestFetch_FetchFnError_PropagatedToAllWaiters(t *testing.T) {
	const n = 3
	sm := state.NewStateMap()
	fetchErr := fmt.Errorf("%w: s3 error", s3client.ErrTransient)

	// Use a barrier so the fetcher blocks until all (n-1) other goroutines
	// have registered as waiters in the state map. This ensures all n goroutines
	// participate in the same in-flight fetch.
	ready := make(chan struct{})
	spy := &FetchFnSpy{
		ReturnErrors: []error{fetchErr},
		BeforeFn: func() {
			// Poll until n-1 goroutines are waiting in the state map.
			for sm.WaitersForPath("/files/c.bam") < n-1 {
				runtime.Gosched()
			}
			<-ready
		},
	}

	var errs [n]error
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); errs[i] = sm.Fetch("/files/c.bam", spy.Fn) }(i)
	}

	close(ready) // unblock the fetcher once all waiters are registered

	wg.Wait()
	for i, err := range errs {
		if !errors.Is(err, s3client.ErrTransient) {
			t.Fatalf("errs[%d]: expected ErrTransient, got %v", i, err)
		}
	}
}

func TestReset_AllowsRefetch(t *testing.T) {
	sm := state.NewStateMap()
	spy := &FetchFnSpy{}
	sm.Fetch("/files/d.bam", spy.Fn)
	sm.Reset("/files/d.bam")
	sm.Fetch("/files/d.bam", spy.Fn)
	if spy.CallCount != 2 {
		t.Fatalf("expected CallCount 2, got %d", spy.CallCount)
	}
}

func TestFetch_TransientError_ResetsToUnknown(t *testing.T) {
	sm := state.NewStateMap()
	transientErr := fmt.Errorf("%w: network error", s3client.ErrTransient)
	spy := &FetchFnSpy{ReturnErrors: []error{transientErr, nil}}

	err := sm.Fetch("/files/e.bam", spy.Fn)
	if !errors.Is(err, s3client.ErrTransient) {
		t.Fatalf("expected ErrTransient, got %v", err)
	}

	st, _ := sm.Get("/files/e.bam")
	if st != state.StateUnknown {
		t.Fatalf("expected StateUnknown after transient, got %v", st)
	}

	err = sm.Fetch("/files/e.bam", spy.Fn)
	if err != nil {
		t.Fatalf("expected success on retry, got %v", err)
	}
	if spy.CallCount != 2 {
		t.Fatalf("expected CallCount 2, got %d", spy.CallCount)
	}
}

func TestFetch_PermanentError_BlocksSubsequentFetches(t *testing.T) {
	sm := state.NewStateMap()
	permErr := fmt.Errorf("%w: not found", s3client.ErrNotFound)
	spy := &FetchFnSpy{ReturnErrors: []error{permErr}}

	err := sm.Fetch("/files/f.bam", spy.Fn)
	if !errors.Is(err, s3client.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	st, _ := sm.Get("/files/f.bam")
	if st != state.StatePermError {
		t.Fatalf("expected StatePermError, got %v", st)
	}

	err = sm.Fetch("/files/f.bam", spy.Fn)
	if !errors.Is(err, s3client.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on second fetch, got %v", err)
	}
	if spy.CallCount != 1 {
		t.Fatalf("expected CallCount still 1, got %d", spy.CallCount)
	}

	sm.Reset("/files/f.bam")
	err = sm.Fetch("/files/f.bam", spy.Fn)
	if err != nil {
		t.Fatalf("expected success after Reset, got %v", err)
	}
	if spy.CallCount != 2 {
		t.Fatalf("expected CallCount 2 after Reset+Fetch, got %d", spy.CallCount)
	}
}
