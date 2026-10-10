package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

// transcriptReply appends one transcript line for an assistant message.
func transcriptReply(t *testing.T, path, id string, at time.Time, sidechain bool, blocks ...map[string]any) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{
		"type": "assistant", "timestamp": at.Format(time.RFC3339Nano), "isSidechain": sidechain,
		"message": map[string]any{"id": id, "content": blocks},
	})
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	fmt.Fprintln(fh, string(b))
}

func textBlock(s string) map[string]any { return map[string]any{"type": "text", "text": s} }
