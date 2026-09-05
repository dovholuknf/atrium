package api

import (
	"net/http"
	"strings"
	"testing"
)

// AN EXPRESSION MAY BE STORED WHERE IT WAS TYPED.
//
// This is the whole test file, and it exists because the thing it guards is
// invisible. The board compiles two operator-supplied functions with
// `new Function` to group cards, which is safe for exactly one reason:
// `groupingPrefs()` reads `localStorage`, so the code running in a browser was
// typed into that browser by whoever was sitting at it.
//
// Moving that storage to the daemon is the obvious next feature. Grouping is
// board-wide, and today it is lost when you open a different browser, so
// somebody will reach for it. It would work perfectly, on the first machine,
// which is what makes it dangerous: these functions run with full page scope,
// so a stored expression is one machine typing code that another machine runs,
// with `fetch` in hand and the whole board to read.
//
// A comment can be deleted. A test that fails names the decision at the moment
// somebody makes the mistake.

func TestAGroupingExpressionCannotBeStoredOnTheDaemon(t *testing.T) {
	for _, field := range []string{"group_by", "group_order"} {
		srv, _, _ := fileServer(t)
		rec := settingsPost(t, srv, `{"`+field+`":"return task.repo"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s was accepted with %d. read the comment in settings.go before "+
				"changing this: the expression runs in whichever browser loads the board",
				field, rec.Code)
		}
		// The refusal has to carry the reason. Somebody who just added the
		// field is going to read this message and nothing else, and "400" on
		// its own reads as a bug in the endpoint rather than as a decision.
		body := rec.Body.String()
		if !strings.Contains(body, "browser") {
			t.Errorf("the refusal for %s does not say why: %s", field, body)
		}
		if !strings.Contains(body, "backlog") {
			t.Errorf("the refusal for %s does not say where the argument is: %s", field, body)
		}
	}
}

// The guard reads the raw body, so it has to keep working beside the fields
// that ARE settings. A request carrying both is refused whole rather than
// half-applied.
func TestAGoodSettingBesideAGroupingExpressionIsStillRefused(t *testing.T) {
	srv, _, _ := fileServer(t)

	rec := settingsPost(t, srv, `{"board_skin":"noir","group_by":"return task.repo"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("answered %d, wanted 400", rec.Code)
	}
	if got := settingsGet(t, srv)["board_skin"]; got == "noir" {
		t.Fatal("a refused request still applied the setting beside the refused field")
	}
}

// And it does not fire on anything else, which is the failure that would make
// somebody delete the guard rather than understand it.
func TestOrdinarySettingsAreUnaffected(t *testing.T) {
	srv, _, _ := fileServer(t)

	if rec := settingsPost(t, srv, `{"board_skin":"moss","browse_roots":"/tmp"}`); rec.Code != http.StatusOK {
		t.Fatalf("an ordinary settings write answered %d: %s", rec.Code, rec.Body.String())
	}
}
