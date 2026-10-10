//go:build integration

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

// A WORKTREE STEP THAT FAILED FOR WANT OF AN SCM FOLDER ANSWERS no_scm_root, 422, so the hub tries another room.
func TestWorktreeFailAnswersNoSCMRootWithItsOwnCode(t *testing.T) {
	for _, err := range []error{gitsync.ErrNoSCMRoot, fmt.Errorf("wrapped: %w", errNoScratchRoot)} {
		w := httptest.NewRecorder()
		worktreeFail(w, http.StatusBadRequest, err)
		var got map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &got)
		if w.Code != http.StatusUnprocessableEntity || got["code"] != "no_scm_root" || got["step"] != "worktree" {
			t.Fatalf("%v = %d %s", err, w.Code, w.Body)
		}
	}
	w := httptest.NewRecorder()
	worktreeFail(w, http.StatusBadRequest, fmt.Errorf("git said no"))
	if w.Code != http.StatusBadRequest || !json.Valid(w.Body.Bytes()) {
		t.Fatalf("other failure = %d %s", w.Code, w.Body)
	}
}
