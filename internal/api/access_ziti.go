package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/dovholuknf/atrium/internal/base/edge"
	"github.com/dovholuknf/atrium/internal/zitijoin"
)

// zitiBody is what the Accessing Atrium form posts for an OpenZiti entry.
type zitiBody struct {
	Name       string `json:"name"`
	Controller string `json:"controller"`
	Token      string `json:"token"`  // join: the IdP sign-in JWT. jwt: the .jwt the controller issued.
	Mode       string `json:"mode"`   // join: "cert" (recommended) or "token"
	Signer     string `json:"signer"` // join, mode token: the IdP to sign in with at every start
	Ack        bool   `json:"ack"`    // join, mode token: the warning was read
	Cert       string `json:"cert"`   // import
	Key        string `json:"key"`    // import
}

// accessZiti brings one OpenZiti identity in, as its own file, and answers the settings of the entry that holds it.
// way is join (by URL), jwt (an uploaded .jwt) or import (a certificate and key), or signers (what a controller offers).
func (s *Server) accessZiti(w http.ResponseWriter, r *http.Request) {
	if !edge.LocalOperator(r) {
		writeErr(w, http.StatusForbidden, errors.New("add an OpenZiti identity from the machine it is for"+edge.ProxyNote(r)))
		return
	}
	if s.AccessZitiDir == nil {
		writeErr(w, http.StatusNotImplemented, errNoOverlays)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<18))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var b zitiBody
	if err := json.Unmarshal(raw, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	p := zitijoin.Params{Controller: b.Controller, Name: b.Name, Dir: s.AccessZitiDir()}
	var path string
	switch r.PathValue("way") {
	case "signers":
		list, err := zitijoin.Signers(r.Context(), p)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"signers": list})
		return
	case "join":
		switch b.Mode {
		case "", "cert":
			path, err = zitijoin.EnrollToCert(r.Context(), p, b.Token)
		case "token":
			if !b.Ack {
				writeErr(w, http.StatusBadRequest, errors.New("enroll to token keeps no credential here, so every hub "+
					"and room start needs an IdP sign-in. confirm that, or enroll to cert"))
				return
			}
			path, err = zitijoin.EnrollToToken(r.Context(), p, b.Signer)
		default:
			err = errors.New("mode is cert or token")
		}
	case "jwt":
		path, err = zitijoin.EnrollJWT(p, b.Token)
	case "import":
		path, err = zitijoin.Import(r.Context(), p, b.Cert, b.Key)
	default:
		writeErr(w, http.StatusNotFound, errors.New("no way of joining called "+r.PathValue("way")))
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	settings := map[string]string{"identity": path}
	if b.Controller != "" {
		settings["controller"] = b.Controller
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
}
