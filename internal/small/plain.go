package small

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

func WriteJSONErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func HasTag(tags []string, want string) bool {
	for _, t := range tags {
		if strings.EqualFold(strings.TrimSpace(t), want) {
			return true
		}
	}
	return false
}

// IsParked reports whether a card is parked.
func IsParked(t *store.Task) bool { return t != nil && t.ParkedAt != nil }

// ErrString is an error from a literal, without reaching for fmt.
type ErrString string

func (e ErrString) Error() string { return string(e) }

// FirstLine keeps a card's one line summary to one line.
func FirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

// WriteJSON answers code with v as JSON.
func WriteJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
