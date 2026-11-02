//go:build integration

package store

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestCloseReleasesTheFileWithCallsInFlight(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := s.Register(Observed{WireName: "w", Worktree: "/x", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	errs := make(chan error, 8)
	var started sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		started.Add(1)
		go func() {
			defer wg.Done()
			first := true
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := s.Resources(task.ID); err != nil && !errors.Is(err, ErrClosed) {
					errs <- err
					if first {
						started.Done()
					}
					return
				}
				if first {
					first = false
					started.Done()
				}
			}
		}()
	}
	started.Wait()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".moved"); err != nil {
		t.Fatalf("the file is still held after Close: %v", err)
	}
	close(stop)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("a call in flight at Close: %v", err)
	}
}
