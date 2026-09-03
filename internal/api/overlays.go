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

type overlayErr string

func (e overlayErr) Error() string { return string(e) }

const errNoOverlays overlayErr = "this daemon was built without overlay support"
