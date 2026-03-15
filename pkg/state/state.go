package state

import (
	"errors"
	"sync"

	"github.com/your-org/gofsaas/pkg/s3client"
)

// FileState represents the lifecycle state of a file in the cache.
type FileState int

const (
	StateUnknown  FileState = iota
	StateChecking           // reserved for future use
	StateNotFound           // reserved for future use
	StateFetching
	StateCached
	StatePermError
	StateTransientError // fetch failed transiently; entry is cleaned up after waiters drain
)

// StateMap manages the fetch lifecycle for paths.
type StateMap interface {
	// Fetch ensures the file at path is fetched exactly once.
	// Concurrent callers block until the single in-flight fetch completes.
	Fetch(path string, fetchFn func() error) error
	// Get returns the current state for path without triggering a fetch.
	Get(path string) (FileState, error)
	// Reset clears the state for path, allowing a future Fetch to re-run.
	Reset(path string)
	// Stats returns counts of files currently cached and in-flight.
	Stats() (filesCached, filesFetching int)
}

type entry struct {
	state    FileState
	err      error
	done     chan struct{} // closed when no longer in StateFetching
	waiters  int          // number of goroutines waiting on done
}

// ConcreteStateMap is the production implementation of StateMap.
type ConcreteStateMap struct {
	mu      sync.Mutex
	entries map[string]*entry
}

// NewStateMap returns an initialized ConcreteStateMap.
func NewStateMap() *ConcreteStateMap {
	return &ConcreteStateMap{
		entries: make(map[string]*entry),
	}
}

// Fetch ensures fetchFn is called at most once for a given path concurrently.
// If the path is already cached, fetchFn is skipped.
// If a fetch is already in progress, the caller blocks until it completes and
// gets the same result. Transient errors are cleared after all current waiters
// drain, allowing the next sequential Fetch to retry.
// Permanent (ErrNotFound) errors persist until Reset is called.
func (sm *ConcreteStateMap) Fetch(path string, fetchFn func() error) error {
	sm.mu.Lock()
	e, ok := sm.entries[path]
	if ok {
		switch e.state {
		case StateCached:
			sm.mu.Unlock()
			return nil

		case StatePermError:
			err := e.err
			sm.mu.Unlock()
			return err

		case StateFetching:
			// Another goroutine is fetching; register as a waiter and block.
			e.waiters++
			sm.mu.Unlock()
			<-e.done
			// Wake up. Read the result from the entry (safe since done is closed
			// and err is only written before close).
			sm.mu.Lock()
			result := e.err
			e.waiters--
			// If this was a transient error and we are the last waiter,
			// remove the entry so the next Fetch can retry.
			if e.state == StateTransientError && e.waiters == 0 {
				delete(sm.entries, path)
			}
			sm.mu.Unlock()
			return result
		}
	}

	// No entry (or entry was cleared). Start a new fetch.
	e = &entry{state: StateFetching, done: make(chan struct{})}
	sm.entries[path] = e
	sm.mu.Unlock()

	fetchErr := fetchFn()

	sm.mu.Lock()
	if fetchErr != nil {
		if errors.Is(fetchErr, s3client.ErrNotFound) {
			// Permanent: keep in map so subsequent callers get immediate error.
			e.state = StatePermError
			e.err = fetchErr
		} else {
			// Transient: keep in map as StateTransientError so waiters can read
			// the error after done is closed. The last waiter to drain will remove
			// the entry; if there are no waiters, remove it now.
			e.state = StateTransientError
			e.err = fetchErr
			if e.waiters == 0 {
				delete(sm.entries, path)
			}
		}
	} else {
		e.state = StateCached
		e.err = nil
	}
	close(e.done)
	sm.mu.Unlock()

	return fetchErr
}

// Get returns the current state for path without triggering a fetch.
// Returns StateUnknown for paths in a transient error state.
func (sm *ConcreteStateMap) Get(path string) (FileState, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	e, ok := sm.entries[path]
	if !ok {
		return StateUnknown, nil
	}
	if e.state == StateTransientError {
		return StateUnknown, nil
	}
	return e.state, e.err
}

// Reset clears the state for path, allowing a future Fetch to re-run.
func (sm *ConcreteStateMap) Reset(path string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.entries, path)
}

// Stats returns counts of entries in StateCached and StateFetching states.
func (sm *ConcreteStateMap) Stats() (filesCached, filesFetching int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for _, e := range sm.entries {
		switch e.state {
		case StateCached:
			filesCached++
		case StateFetching:
			filesFetching++
		}
	}
	return
}

// WaitersForPath returns the number of goroutines currently waiting on the
// in-flight fetch for path. Used in tests to synchronize.
func (sm *ConcreteStateMap) WaitersForPath(path string) int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	e, ok := sm.entries[path]
	if !ok {
		return 0
	}
	return e.waiters
}
