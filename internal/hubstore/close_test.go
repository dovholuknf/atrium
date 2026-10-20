package hubstore

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openClosable(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCloseWaitsForACallInFlight(t *testing.T) {
	s := openClosable(t)
	started, release := make(chan struct{}), make(chan struct{})
	callDone := make(chan error, 1)
	go func() {
		callDone <- s.guard(func() error {
			close(started)
			<-release
			_, err := s.db.Exec("SELECT 1")
			return err
		})
	}()
	<-started

	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case <-closed:
		t.Fatal("Close returned while a call was still running")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	if err := <-callDone; err != nil {
		t.Fatalf("the call in flight when Close ran failed: %v", err)
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return once the call finished")
	}
}

func TestACallAfterCloseIsRefusedAndDoesNotHalt(t *testing.T) {
	s := openClosable(t)
	var told error
	s.OnHalt = func(cause error) { told = cause }
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Rooms(); !errors.Is(err, ErrClosed) {
		t.Fatalf("a read after Close: want ErrClosed, got %v", err)
	}
	if err := s.guard(func() error { return s.db.QueryRow("SELECT 1").Err() }); !errors.Is(err, ErrClosed) {
		t.Fatalf("a guarded call after Close: want ErrClosed, got %v", err)
	}
	if halted, cause := s.Halted(); halted || told != nil {
		t.Fatalf("a Close asked for halted the store: %v %v", cause, told)
	}
}

func TestAFailureStillHaltsAnOpenStore(t *testing.T) {
	s := openClosable(t)
	err := s.guard(func() error { return errors.New("disk gone") })
	if !errors.Is(err, ErrHalted) {
		t.Fatalf("want ErrHalted, got %v", err)
	}
	if halted, _ := s.Halted(); !halted {
		t.Fatal("a real failure did not halt the store")
	}
}
