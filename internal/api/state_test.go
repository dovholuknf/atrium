package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dovholuknf/atrium/internal/link"
	"github.com/dovholuknf/atrium/internal/store"
)

// stateCards is what /v1/state answers, one raw payload per card, exactly as
// the room's announcer hands them to the hub.
func stateCards(t *testing.T, srv *Server) map[string]json.RawMessage {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/state", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/v1/state answered %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Cards []json.RawMessage `json:"cards"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	out := map[string]json.RawMessage{}
	for _, raw := range body.Cards {
		var head struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(raw, &head)
		out[head.ID] = raw
	}
	return out
}

// THE HUB'S NOTIFIER READS WHAT THIS ROOM SENDS (f-023). A real card with a
// real seen row goes through the real /v1/state and into link.NotifyIdentity,
// so the two halves cannot disagree about the shape again. Before the fix the
// payload carried no seen view at all, and a card with open questions came out
// as whatever its status said, never as a question.
func TestStateCarriesWhatTheNotifierReads(t *testing.T) {
	srv, st, dir := fileServer(t)
	asked := cardIn(t, st, dir)
	if err := st.NoteTurnEnded(asked.ID, store.TurnQuestions{Known: true, Block: true,
		List: []string{"land the cap first?"}}); err != nil {
		t.Fatal(err)
	}

	cards := stateCards(t, srv)
	got, ok := link.NotifyIdentity(asked.ID, cards[asked.ID])
	if !ok || got.Reason != link.ReasonQuestion {
		t.Fatalf("a card with an open question notified %+v ok=%v, want %q. payload %s",
			got, ok, link.ReasonQuestion, cards[asked.ID])
	}

	// Answered, the question is gone, and a turn nobody has seen is what is left.
	if _, err := st.MarkAnswered(asked.ID, "test"); err != nil {
		t.Fatal(err)
	}
	cards = stateCards(t, srv)
	if got, ok := link.NotifyIdentity(asked.ID, cards[asked.ID]); ok && got.Reason == link.ReasonQuestion {
		t.Errorf("an answered card still notified a question: %+v", got)
	}
}

// A room with no cards still answers a list, never null, which the hub reads
// as "clear" rather than "could not tell".
func TestStateWithNoCardsIsAnEmptyList(t *testing.T) {
	srv, _, _ := fileServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/state", nil))
	if got := rec.Body.String(); got != "{\"cards\":[]}\n" {
		t.Errorf("an empty room answered %q", got)
	}
}
