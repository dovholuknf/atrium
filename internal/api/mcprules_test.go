package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func postJSON(t *testing.T, srv *Server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestAddRuleForAnMCPTool(t *testing.T) {
	srv, st, _ := fileServer(t)

	rec := postJSON(t, srv, "/v1/rules", `{"tool":"mcp__mercurius__*","decision":"approve"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("a server rule with no prefix answered %d %s", rec.Code, rec.Body.String())
	}
	rec = postJSON(t, srv, "/v1/rules", `{"tool":"mcp__mercurius__x","prefix":"*","decision":"block"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("an explicit * prefix answered %d %s", rec.Code, rec.Body.String())
	}

	for _, body := range []string{
		`{"tool":"mcp__mercurius__x","prefix":"foo","decision":"approve"}`,
		`{"tool":"mcp__mercurius__x","kind":"path","prefix":"D:/git/x","decision":"approve"}`,
		`{"tool":"mcp__*","decision":"approve"}`,
	} {
		rec = postJSON(t, srv, "/v1/rules", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s answered %d, want 400", body, rec.Code)
		}
	}
	rules, err := st.Rules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 {
		t.Errorf("a refused rule was stored: %+v", rules)
	}
}

func TestImportBroadMCPDenyNeedsInclude(t *testing.T) {
	entries := `[{"tool":"mcp__*","pattern":"*","decision":"block","source":"test","broad":true},` +
		`{"tool":"mcp__mercurius__*","pattern":"*","decision":"approve","source":"test"}]`

	srv, st, _ := fileServer(t)
	rec := postJSON(t, srv, "/v1/rules/import", `{"rules":`+entries+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("import answered %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Added   int                 `json:"added"`
		Skipped []map[string]string `json:"skipped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Added != 1 || len(out.Skipped) != 1 {
		t.Fatalf("without include_broad: added %d skipped %+v", out.Added, out.Skipped)
	}

	srv, st, _ = fileServer(t)
	rec = postJSON(t, srv, "/v1/rules/import", `{"include_broad":true,"rules":`+entries+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("import answered %d %s", rec.Code, rec.Body.String())
	}
	rules, err := st.Rules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 {
		t.Fatalf("with include_broad both should be stored: %+v", rules)
	}
	if r, _ := st.MatchRule("mcp__atrium-control__atrium_report", "{}", ""); r == nil || r.Decision != "block" {
		t.Errorf("the bare deny should cover every MCP tool: %+v", r)
	}
}

// Approve-forever on an MCP call writes a rule about the tool, not about the
// compacted JSON of that one call.
func TestForeverOnAnMCPCallWritesAToolRule(t *testing.T) {
	srv, st, dir := fileServer(t)
	c := cardIn(t, st, dir)
	p, _, err := st.RecordPermission(c.ID, "mcp__mercurius__discourse_discourse_search",
		`{"query":"ziti"}`, "", "")
	if err != nil {
		t.Fatal(err)
	}
	// No transport is wired here, so the decision itself answers 501. The rule is
	// written before it, which is the part under test.
	postJSON(t, srv, "/v1/permissions/"+p.ID+"/decide", `{"decision":"approve","forever":true}`)

	rules, err := st.Rules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("wanted one rule, got %+v", rules)
	}
	r := rules[0]
	if r.Tool != "mcp__mercurius__discourse_discourse_search" || r.Prefix != "*" || r.Note != "" {
		t.Errorf("the rule is not a plain tool rule: %+v", r)
	}
	if got, _ := st.GetPermission(p.ID); got == nil || got.RuleCreated != r.Tool {
		t.Errorf("the request should record the rule it made: %+v", got)
	}
	if m, _ := st.MatchRule(r.Tool, `{"query":"something else"}`, ""); m == nil {
		t.Error("the rule did not match a different input")
	}
}

func TestListRulesMarksAnExactInputRule(t *testing.T) {
	srv, st, _ := fileServer(t)
	if _, err := st.AddRule("mcp__mercurius__x", `{"query":"ziti`, "approve", "", ""); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/rules", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	var out struct {
		Rules []store.Rule `json:"rules"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Rules) != 1 || out.Rules[0].Note != store.NoteExactInput {
		t.Fatalf("the list did not carry the note: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"note":"matches this exact input only"`) {
		t.Errorf("the wire field is not named note: %s", rec.Body.String())
	}
}
