//go:build integration

package link

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// TestNotifyCommandNeverSeesWhatACardHolds runs a whole announcement through the
// real notifier and the real command sink, with a card whose payload carries the
// command a permission wants, a question, a recap, a diff and a path. The fake
// command records everything it was given, and none of it may be there. This is
// the claim docs/rnd/telegram-notify-design.md rests on.
func TestNotifyCommandNeverSeesWhatACardHolds(t *testing.T) {
	sink, out := recordingCommand(t)
	secrets := []string{"CANARY-CMD-rm-rf", "CANARY-QUESTION", "CANARY-RECAP", "CANARY-DIFF", "CANARY-PATH", "CANARY-TOKEN"}

	st := newFakeStore()
	n := NewNotifier(st)
	n.SetSink(sink)
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	n.Start(ctx)
	if err := n.Configure(true, []string{os.Args[0]}); err != nil {
		t.Fatal(err)
	}

	rich := ncard("a", `,"alias":"build box","status":"needs-permission","waiting_since":"T1",`+
		`"recap":"CANARY-RECAP needs: CANARY-TOKEN","command":"CANARY-CMD-rm-rf /",`+
		`"permission":{"tool":"Bash","input":"CANARY-CMD-rm-rf /"},"diff":"CANARY-DIFF +x",`+
		`"cwd":"/home/CANARY-PATH","seen":{"questions_at":"T0","open_questions":["CANARY-QUESTION?"]}`)
	n.Announced("sparta", nil)
	n.Announced("sparta", []CardState{rich})

	var raw []byte
	until(t, "the command to run", func() bool {
		raw, _ = os.ReadFile(out)
		return bytes.Count(raw, []byte("\n")) == 1
	})
	for _, s := range secrets {
		if strings.Contains(string(raw), s) {
			t.Fatalf("%q reached the command: %s", s, raw)
		}
	}
	var rec struct {
		Name, Reason, Card, Room, Stdin string
		Args                            []string
	}
	if err := json.Unmarshal(bytes.TrimSpace(raw), &rec); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	if rec.Name != "build box" || rec.Reason != "permission" || rec.Card != "sparta~a" || rec.Room != "sparta" {
		t.Fatalf("fields %+v", rec)
	}
	var line map[string]string
	if err := json.Unmarshal([]byte(rec.Stdin), &line); err != nil || len(line) != 4 {
		t.Fatalf("stdin is not the four fields: %q", rec.Stdin)
	}
	if len(rec.Args) != 0 {
		t.Fatalf("argv %v", rec.Args)
	}
	until(t, "the run to be recorded", func() bool { return n.Status().Sent == 1 })
	time.Sleep(50 * time.Millisecond)
	if s := n.Status(); strings.Contains(strings.Join(s.Command, " "), "CANARY") {
		t.Fatalf("status %+v", s)
	}
}
