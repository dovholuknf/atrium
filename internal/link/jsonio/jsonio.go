// Package jsonio is the hub's JSON request and answer helpers, shared by the features that serve routes.
package jsonio

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// Body is the most a request body may carry: a why is at most 4000 characters.
const Body = 64 << 10

func JSON(w http.ResponseWriter, code int, v any) {
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func Fail(w http.ResponseWriter, code int, msg string) {
	JSON(w, code, map[string]string{"error": msg})
}

// Decode reads one JSON object, refusing a field it does not know and anything after the object.
func Decode(r *http.Request, into any) error { return DecodeMax(r, into, Body) }

// DecodeMax is Decode with a body of at most max bytes.
func DecodeMax(r *http.Request, into any, max int64) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, max))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("there is more after the object")
	}
	return nil
}

// StoreFail answers a store error: a refusal is the caller's to fix, anything else is the hub's.
func StoreFail(w http.ResponseWriter, err error) {
	code := http.StatusServiceUnavailable
	switch {
	case errors.Is(err, hubstore.ErrItemNotFound), errors.Is(err, hubstore.ErrFlowItemNotFound), errors.Is(err, hubstore.ErrNoEpoch):
		code = http.StatusNotFound
	case hubstore.IsRefusal(err):
		code = http.StatusBadRequest
	}
	Fail(w, code, err.Error())
}

// WriteJSONBody answers with a JSON body that no cache keeps.
func WriteJSONBody(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func WriteErrBody(w http.ResponseWriter, code int, msg string) {
	WriteJSONBody(w, code, map[string]any{"error": msg})
}
