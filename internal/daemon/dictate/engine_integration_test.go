//go:build integration

package dictate

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Set ATRIUM_TEST_WHISPER_SERVER, ATRIUM_TEST_WHISPER_MODEL and ATRIUM_TEST_WHISPER_WAV (a clip saying "claude slash
// main") to run it against a real whisper-server.
func TestRealWhisperServerTranscribes(t *testing.T) {
	bin, model, clip := os.Getenv("ATRIUM_TEST_WHISPER_SERVER"), os.Getenv("ATRIUM_TEST_WHISPER_MODEL"), os.Getenv("ATRIUM_TEST_WHISPER_WAV")
	if bin == "" || model == "" || clip == "" {
		t.Skip("no whisper-server configured")
	}
	wav, err := os.ReadFile(clip)
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Conf: func() (string, string) { return bin, model }}
	defer e.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	raw, err := e.Transcribe(ctx, wav, Prompt(nil))
	if err != nil {
		t.Fatal(err)
	}
	if got := Clean(raw, nil); !strings.Contains(got, "claude/main") {
		t.Fatalf("transcript %q lacks claude/main", got)
	}
}
