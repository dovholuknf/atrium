//go:build integration

package link

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeEnd is an EndDoor whose room records the bodies it was handed.
func fakeEnd() (EndDoor, *[]map[string]any) {
	var got []map[string]any
	return EndDoor{Report: func(_ context.Context, _ *mcp.CallToolRequest, body map[string]any) (EndOutput, error) {
		got = append(got, body)
		return EndOutput{Recorded: true, Status: "done", LauncherTold: true}, nil
	}}, &got
}

func TestDoneRefusesAnIdThatIsNotACommitShape(t *testing.T) {
	for _, sha := range []string{"", "abc12", "zzzzzzz", "abc1234 ", strings.Repeat("a", 41), "abc123; rm"} {
		d, got := fakeEnd()
		_, _, err := d.Done(context.Background(), nil, doneInput{SHA: sha})
		if sha == "abc1234 " {
			// trimmed, so this one is fine
			if err != nil {
				t.Fatalf("%q: %v", sha, err)
			}
			continue
		}
		if err == nil || len(*got) != 0 {
			t.Fatalf("%q: err = %v, recorded %d, want a refusal and nothing recorded", sha, err, len(*got))
		}
	}
}

func TestDoneDeliversAsADoneReportWithTheEndedFlag(t *testing.T) {
	d, got := fakeEnd()
	_, out, err := d.Done(context.Background(), nil, doneInput{SHA: "abc1234"})
	if err != nil || !out.Recorded {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
	b := (*got)[0]
	if b["status"] != "done" || b["sha"] != "abc1234" || b["ended"] != true || b["recap"] != "done abc1234" {
		t.Fatalf("body = %v", b)
	}
}

func TestDoneTakesAnArtifactInPlaceOfASHAButNotBothOrNeither(t *testing.T) {
	d, got := fakeEnd()
	if _, _, err := d.Done(context.Background(), nil, doneInput{Artifact: " /tmp/report.md "}); err != nil {
		t.Fatal(err)
	}
	b := (*got)[0]
	if b["status"] != "done" || b["artifact"] != "/tmp/report.md" || b["ended"] != true || b["sha"] != nil {
		t.Fatalf("body = %v", b)
	}
	for name, in := range map[string]doneInput{
		"both":    {SHA: "abc1234", Artifact: "/tmp/r.md"},
		"neither": {},
	} {
		d, got := fakeEnd()
		if _, _, err := d.Done(context.Background(), nil, in); err == nil || len(*got) != 0 {
			t.Fatalf("%s: err = %v, recorded %d, want a refusal and nothing recorded", name, err, len(*got))
		}
	}
	if !strings.Contains(doneToolDesc, "sha or artifact location") {
		t.Fatalf("the description: %s", doneToolDesc)
	}
}

func TestBlockedRefusesAnEmptyReasonOrOver50Words(t *testing.T) {
	for name, reason := range map[string]string{
		"empty":             "",
		"blank":             " \n\t ",
		"over 50":           strings.Repeat("word ", 51),
		"over 50, on lines": strings.Repeat("word\n", 51),
	} {
		d, got := fakeEnd()
		_, _, err := d.Blocked(context.Background(), nil, blockedInput{Reason: reason})
		if err == nil || len(*got) != 0 {
			t.Fatalf("%s: err = %v, recorded %d, want a refusal and nothing recorded", name, err, len(*got))
		}
	}
}

func TestBlockedTakes50WordsWithNewlinesAndDeliversItAsBlocked(t *testing.T) {
	d, got := fakeEnd()
	reason := strings.Repeat("word\n", 49) + "word"
	if _, _, err := d.Blocked(context.Background(), nil, blockedInput{Reason: reason}); err != nil {
		t.Fatal(err)
	}
	b := (*got)[0]
	if b["status"] != "blocked" || b["ask"] != reason || b["ended"] != true {
		t.Fatalf("body = %v", b)
	}
}
