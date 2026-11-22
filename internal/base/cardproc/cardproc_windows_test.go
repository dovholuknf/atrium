//go:build windows

package cardproc

import (
	"errors"
	"testing"
	"unicode/utf16"
)

func TestStartTimeOfAPidPastUint32IsGone(t *testing.T) {
	if _, err := StartTime(1<<32 + 4); !errors.Is(err, ErrGone) {
		t.Fatalf("err = %v, want ErrGone rather than the start time of pid 4", err)
	}
}

func TestEnvValueMatchesTheNameWithoutCaseAndStopsAtTheEmptyEntry(t *testing.T) {
	block := utf16.Encode([]rune("=C:=C:/x\x00PATH=a\x00atrium_task_id=card-1\x00\x00ATRIUM_TASK_ID=late"))
	if got := envValue(block, "ATRIUM_TASK_ID"); got != "card-1" {
		t.Errorf("got %q, want card-1", got)
	}
	if got := envValue(block, "HOME"); got != "" {
		t.Errorf("got %q for a name not there", got)
	}
}
