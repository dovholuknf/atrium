package forge

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

// fakeGH answers the gh commands of the CI reads, no network. It records each command.
type fakeGH struct {
	mu   sync.Mutex
	got  []Cmd
	logs string
	err  error
	// dl is written under --dir when `run download` runs.
	dl map[string]string
	// zipBytes, when set, is the artifact as the forge serves it.
	zipBytes []byte
	zips     int
}

// zipOf is a zip of the named files.
func zipOf(files map[string]string) []byte {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for n, body := range files {
		f, _ := w.Create(n)
		_, _ = f.Write([]byte(body))
	}
	_ = w.Close()
	return b.Bytes()
}

func (f *fakeGH) run(_ context.Context, c Cmd) ([]byte, error) {
	f.mu.Lock()
	f.got = append(f.got, c)
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	a := strings.Join(c.Args, " ")
	switch {
	case strings.HasPrefix(a, "run list"):
		return []byte(`[{"databaseId":37771754840,"workflowName":"ci","status":"completed","conclusion":"failure",` +
			`"headSha":"8918f846","headBranch":"claude/main","createdAt":"2026-10-08T10:00:00Z","url":"https://github.com/o/r/actions/runs/37771754840"}]`), nil
	case strings.HasPrefix(a, "run view") && strings.Contains(a, "--json"):
		return []byte(`{"databaseId":9,"workflowName":"ci","status":"completed","conclusion":"failure","headSha":"abc",` +
			`"headBranch":"b","createdAt":"t","url":"u","jobs":[{"databaseId":55,"name":"windows","status":"completed",` +
			`"conclusion":"failure","url":"ju","steps":[{"number":1,"name":"checkout","status":"completed","conclusion":"success"},` +
			`{"number":2,"name":"test","status":"completed","conclusion":"failure"}]}]}`), nil
	case strings.HasPrefix(a, "run view"):
		if c.Tail > 0 {
			w := &TailWriter{Lines: c.Tail, Bytes: c.Limit}
			_, _ = w.Write([]byte(f.logs))
			t, _, _ := w.Result()
			return []byte(t), nil
		}
		return []byte(f.logs), nil
	case strings.HasPrefix(a, "api") && strings.HasSuffix(a, "/zip"):
		f.mu.Lock()
		f.zips++
		f.mu.Unlock()
		if f.zipBytes != nil {
			_, err := c.Sink.Write(f.zipBytes)
			return nil, err
		}
		_, err := c.Sink.Write(zipOf(f.dl))
		return nil, err
	case strings.HasPrefix(a, "api"):
		return []byte(`{"artifacts":[{"id":1,"name":"ci","size_in_bytes":20,"expired":false,"created_at":"t"},` +
			`{"id":2,"name":"big","size_in_bytes":999999999999,"expired":false},{"id":3,"name":"old","size_in_bytes":1,"expired":true}]}`), nil
	}
	return nil, fmt.Errorf("unexpected gh %s", a)
}

var ciRef = Ref{Org: "o", Repo: "r"}

func TestCIRunsAreFilteredAndShaped(t *testing.T) {
	f := &fakeGH{}
	g := newGitHub("", f.run)
	runs, err := g.Runs(context.Background(), ciRef, RunQuery{Branch: "claude/main", SHA: "8918f846", Limit: 500})
	if err != nil || len(runs) != 1 {
		t.Fatalf("%+v %v", runs, err)
	}
	r := runs[0]
	if r.ID != 37771754840 || r.Workflow != "ci" || r.Conclusion != "failure" || r.Head != "8918f846" || r.Created == "" || r.URL == "" {
		t.Errorf("%+v", r)
	}
	a := strings.Join(f.got[0].Args, " ")
	for _, want := range []string{"run list", "--repo o/r", "--limit 50", "--branch claude/main", "--commit 8918f846"} {
		if !strings.Contains(a, want) {
			t.Errorf("%q lacks %q", a, want)
		}
	}
}

func TestCIRunHasJobsAndSteps(t *testing.T) {
	g := newGitHub("", (&fakeGH{}).run)
	d, err := g.RunDetail(context.Background(), ciRef, 9)
	if err != nil || len(d.Jobs) != 1 {
		t.Fatalf("%+v %v", d, err)
	}
	j := d.Jobs[0]
	if j.ID != 55 || j.Conclusion != "failure" || len(j.Steps) != 2 || j.Steps[1].Name != "test" || j.Steps[1].Conclusion != "failure" {
		t.Errorf("%+v", j)
	}
}

func TestCIMissingLoginIsAnAccessError(t *testing.T) {
	for _, e := range []error{errors.New("gh run list: To get started with GitHub CLI, please run:  gh auth login"),
		&exec.Error{Name: "gh", Err: exec.ErrNotFound}} {
		_, err := newGitHub("", (&fakeGH{err: e}).run).Runs(context.Background(), ciRef, RunQuery{})
		var ae *AccessError
		if !errors.As(err, &ae) || !strings.Contains(err.Error(), "gh auth login --hostname github.com") {
			t.Errorf("%v", err)
		}
	}
}

func TestCIOnBitbucketIsNotSupported(t *testing.T) {
	b, _ := New(Bitbucket, "", nil)
	_, err := CIOf(b)
	var ns *NotSupportedError
	if !errors.As(err, &ns) || ns.Code() != "not_supported" || !strings.Contains(err.Error(), "not supported on bitbucket") {
		t.Fatalf("%v", err)
	}
	g, _ := New(GitHub, "", nil)
	if _, err := CIOf(g); err != nil {
		t.Errorf("%v", err)
	}
}
