//go:build windows

package cardproc

import (
	"errors"
	"testing"
)

func TestStartTimeOfAPidPastUint32IsGone(t *testing.T) {
	if _, err := StartTime(1<<32 + 4); !errors.Is(err, ErrGone) {
		t.Fatalf("err = %v, want ErrGone rather than the start time of pid 4", err)
	}
}
