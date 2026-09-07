package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The account block has to be able to say why it is empty.
//
// It reports on somebody else's instance, so "the zrok instance would not
// answer" is the most likely thing it ever has to draw, and it is a sentence
// worth reading rather than a failure. The endpoint therefore answers 200 with
// the reason inside, the same posture `zitiServices` takes, because a failing
// status makes the board paint a generic error where the one useful sentence
// was going to go.

func zrokAccountGet(t *testing.T, srv *Server) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/overlays/zrok/account", nil))
	return rec
}

func TestTheAccountEndpointCarriesItsFailureRatherThanReturningOne(t *testing.T) {
	srv, _, _ := fileServer(t)
	srv.ZrokAccount = func() any {
		return map[string]any{"err": "could not reach the zrok instance"}
	}

	rec := zrokAccountGet(t, srv)
	if rec.Code != http.StatusOK {
		t.Fatalf("answered %d. a zrok instance that is down is something to show beside the "+
			"counters, not a failed request", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "could not reach") {
		t.Fatalf("the reason did not survive the round trip: %s", rec.Body.String())
	}
}

// The counts have to reach the board under the names it reads. This is the
// whole contract between the daemon and the panel, and nothing else checks it:
// a renamed field paints zeroes, which looks like an account with nothing on
// it rather than like a bug.
func TestTheAccountEndpointReportsTheCountsTheBoardReads(t *testing.T) {
	srv, _, _ := fileServer(t)
	srv.ZrokAccount = func() any {
		return map[string]any{
			"limited": true, "environments": 4, "shares": 3,
			"reserved_names": 34, "atrium_names": 6, "here_shares": 1, "here": true,
		}
	}

	body := zrokAccountGet(t, srv).Body.String()
	for _, field := range []string{
		"limited", "environments", "shares", "reserved_names", "atrium_names", "here_shares",
	} {
		if !strings.Contains(body, `"`+field+`"`) {
			t.Errorf("%q is not in the answer, so the panel draws nothing for it: %s", field, body)
		}
	}
}

// A daemon that supplies no account reader must not register the route at all,
// rather than register one that panics on a nil call. Same shape as every
// other optional field on `Server`.
func TestTheAccountEndpointIsAbsentWithoutADaemon(t *testing.T) {
	srv, _, _ := fileServer(t)

	if rec := zrokAccountGet(t, srv); rec.Code != http.StatusNotFound {
		t.Fatalf("answered %d with no ZrokAccount wired, wanted 404", rec.Code)
	}
}
