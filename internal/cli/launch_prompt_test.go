package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLaunchPromptAtNameIsTheNamedPromptsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/actions" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"actions":[{"id":"run-tests","label":"run the tests","prompt":"Run the tests."},` +
			`{"id":"critique","label":"Critique","prompt":"Critique the branch."}]}`))
	}))
	defer srv.Close()

	for in, want := range map[string]string{
		"@run-tests":            "Run the tests.",
		"@critique":             "Critique the branch.",
		"fix the bug":           "fix the bug",
		"@README.md explain it": "@README.md explain it",
	} {
		got, err := namedPrompt(srv.URL, in)
		if err != nil || got != want {
			t.Errorf("namedPrompt(%q) = %q, %v, want %q", in, got, err, want)
		}
	}
	if _, err := namedPrompt(srv.URL, "@nope"); err == nil || !strings.Contains(err.Error(), "run-tests, critique") {
		t.Errorf("an unknown name: err = %v", err)
	}
}
