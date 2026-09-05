package api

import (
	"encoding/json"
	"io"
	"net/http"
)

// The board's side of reaching atrium from elsewhere.
//
// Every one of these is a thin pass-through: the daemon owns the child process
// a share runs as, and this file only decides what a missing handler means.
// Missing means the feature is not wired, which is a 501 rather than a crash,
// because the API is meant to be servable without a daemon behind it.

func (s *Server) getOverlays(w http.ResponseWriter, r *http.Request) {
	if s.Overlays == nil {
		writeJSON(w, http.StatusOK, map[string]any{"overlays": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"overlays": s.Overlays()})
}

func (s *Server) putOverlay(w http.ResponseWriter, r *http.Request) {
	if s.SaveOverlay == nil {
		writeErr(w, http.StatusNotImplemented, errNoOverlays)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<16))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.SaveOverlay(r.PathValue("kind"), body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.afterOverlayChange(w)
}

func (s *Server) startOverlay(w http.ResponseWriter, r *http.Request) {
	if s.StartOverlay == nil {
		writeErr(w, http.StatusNotImplemented, errNoOverlays)
		return
	}
	// A refusal here is a misconfiguration, not a server fault: no identity,
	// no service, a mode that is not a mode.
	if err := s.StartOverlay(r.PathValue("kind")); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.afterOverlayChange(w)
}

func (s *Server) stopOverlay(w http.ResponseWriter, r *http.Request) {
	if s.StopOverlay == nil {
		writeErr(w, http.StatusNotImplemented, errNoOverlays)
		return
	}
	if err := s.StopOverlay(r.PathValue("kind")); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.afterOverlayChange(w)
}

// setupOverlay gets this machine ready to share.
//
// The tool's own output comes back either way. On failure it is the only thing
// that says what went wrong, and a red box with no next step is how somebody
// gives up.
func (s *Server) setupOverlay(w http.ResponseWriter, r *http.Request) {
	if s.SetupOverlay == nil {
		writeErr(w, http.StatusNotImplemented, errNoOverlays)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<16))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	out, err := s.SetupOverlay(r.PathValue("kind"), body)
	s.answerSetup(w, out, err)
}

func (s *Server) teardownOverlay(w http.ResponseWriter, r *http.Request) {
	if s.TeardownOverlay == nil {
		writeErr(w, http.StatusNotImplemented, errNoOverlays)
		return
	}
	out, err := s.TeardownOverlay(r.PathValue("kind"))
	s.answerSetup(w, out, err)
}

// answerSetup returns the new state and what the tool said, and broadcasts
// either way: a setup that failed still changes what the panel should show.
func (s *Server) answerSetup(w http.ResponseWriter, out string, err error) {
	var state any = []any{}
	if s.Overlays != nil {
		state = s.Overlays()
	}
	s.Broadcast("overlays", state)

	msg := ""
	code := http.StatusOK
	if err != nil {
		msg = err.Error()
		// The operator's to fix, not a server fault: a spent token, a machine
		// that is already enabled, a tool that is not installed.
		code = http.StatusBadRequest
	}
	writeJSON(w, code, map[string]any{
		"overlays": state, "output": out, "error": msg, "ok": err == nil,
	})
}

// inspectToken reads a token without acting on it, so somebody can see which
// network it belongs to and whether it is still good before spending it.
func (s *Server) inspectToken(w http.ResponseWriter, r *http.Request) {
	if s.InspectToken == nil {
		writeErr(w, http.StatusNotImplemented, errNoOverlays)
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	claims, err := s.InspectToken(body.Token)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, claims)
}

// afterOverlayChange answers with the new state and tells every open board.
// A share is one thing for the machine, so two tabs must not disagree about
// whether it is up.
func (s *Server) afterOverlayChange(w http.ResponseWriter) {
	var state any = []any{}
	if s.Overlays != nil {
		state = s.Overlays()
	}
	s.Broadcast("overlays", state)
	writeJSON(w, http.StatusOK, map[string]any{"overlays": state})
}

// reserveName holds a zrok name, so the board's address survives a restart.
//
// Answers with the name selection to configure rather than with "ok", because
// the caller's next move is to put that string in the share's config and it
// should not have to reassemble it from what it sent.
func (s *Server) reserveName(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	sel, err := s.ReserveName(body.Namespace, body.Name)
	if err != nil {
		// Every reason this fails is something the operator has to act on:
		// not enabled yet, a name somebody else holds, an unreachable api. So
		// the message goes through rather than being flattened to a status.
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": sel})
}

// setApiEndpoint points this machine at a zrok instance.
//
// Sent as a field that may be empty rather than as a delete, because clearing
// it is a real instruction: go back to zrok's own default.
func (s *Server) setApiEndpoint(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.SetApiEndpoint(body.Endpoint); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// zitiServices reports what this identity may bind on its network.
//
// Always 200, including when the answer is "the controller would not talk to
// me". The reason belongs beside the service box as something to act on, and a
// failing status would make the board show a generic error instead of the one
// sentence that says what to fix.
func (s *Server) zitiServices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Capabilities())
}

// Lending one session, over a share that reaches nothing else.
//
// Separate endpoints from the overlay ones on purpose. `POST /v1/overlays/zrok/
// start` publishes THE BOARD, with every card on it, and putting both behind
// one verb is how somebody ends up handing over the whole machine when they
// meant to hand over one terminal.

func (s *Server) listGuestShares(w http.ResponseWriter, r *http.Request) {
	if s.GuestShares == nil {
		writeJSON(w, http.StatusOK, map[string]any{"shares": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"shares": s.GuestShares()})
}

func (s *Server) shareCard(w http.ResponseWriter, r *http.Request) {
	if s.ShareCard == nil {
		writeErr(w, http.StatusNotImplemented, errNoOverlays)
		return
	}
	var body struct {
		// public is a link anyone with it can open. private needs zrok on the
		// other end, which is a different audience entirely.
		Mode string `json:"mode"`
		// Writable decides whether the guest can type or only watch. Named in
		// the affirmative and defaulted to false by JSON's own rules, so a
		// caller that forgets it gets the safe answer.
		Writable bool `json:"writable"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	out, err := s.ShareCard(r.PathValue("id"), body.Mode, body.Writable)
	if err != nil {
		// 400 rather than 500. Everything that fails here is a thing to fix:
		// no zrok environment, an account at its limit, a card with no
		// terminal. A 500 would read as atrium being broken.
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) unshareCard(w http.ResponseWriter, r *http.Request) {
	if s.StopCardShare == nil {
		writeErr(w, http.StatusNotImplemented, errNoOverlays)
		return
	}
	if err := s.StopCardShare(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type overlayErr string

func (e overlayErr) Error() string { return string(e) }

const errNoOverlays overlayErr = "this daemon was built without overlay support"
