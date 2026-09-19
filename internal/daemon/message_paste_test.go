package daemon

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// A long, multi-line message typed into a supervised claude session must arrive
// as one bracketed-paste block, not split at its newlines into separate
// submissions that leave only the tail. See the brief for msg-truncation.

// multiLineReport is past the pipe's four kilobytes and full of newlines, which
// is the shape that used to arrive truncated to its last line.
func multiLineReport() string {
	return "briefing follows.\n" +
		strings.Repeat("a line of the briefing that the runner must not read as Enter.\n", 120)
}

func TestALongMessageIsTypedAsOneBracketedPaste(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := pasteTask(t, d, "paste-message", "claude")
	report := multiLineReport()
	if len(report) < 4096 {
		t.Fatalf("the specimen is %d bytes, which is under the pipe size this is about", len(report))
	}

	// 4096 is what a ConPTY write was measured at, so the message crosses it.
	p := &stubbornPty{most: 4096}
	d.sup.add(&runner{taskID: task.ID, pty: p})
	// Off the supervisor before the daemon shuts down: this fake runner has no
	// process or done channel, so the wind-down path would panic on it.
	defer d.sup.remove(task.ID)

	body, err := json.Marshal(map[string]string{"text": report})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/v1/tasks/"+task.ID+"/message", bytes.NewReader(body))
	req.SetPathValue("id", task.ID)
	rec := httptest.NewRecorder()
	d.handleMessage(rec, req)

	if rec.Code != 200 {
		t.Fatalf("handleMessage returned %d: %s", rec.Code, rec.Body.String())
	}
	got := string(p.got)

	if p.calls < 2 {
		t.Fatal("the specimen went in one write, so this proves nothing about installments")
	}
	if !strings.HasPrefix(got, "\x1b[200~") {
		t.Fatal("the message was not opened with the bracketed paste marker, so its newlines submit line by line")
	}
	if !strings.Contains(got, "\x1b[201~") {
		t.Fatal("the paste never closes its bracket")
	}
	// The whole message survived the short writes, newlines and all.
	if !strings.Contains(got, report) {
		t.Fatal("the message was truncated or altered on the way through")
	}
	// The submitting Enter is outside the brackets, or the runner reads it as a
	// newline in the pasted text rather than a submission.
	if strings.Index(got, "\x1b[201~") > strings.LastIndex(got, "\r") {
		t.Fatal("the Enter is inside the brackets, so nothing is submitted")
	}
	if !strings.HasSuffix(got, "\r") {
		t.Fatal("nothing submitted the message")
	}
}
