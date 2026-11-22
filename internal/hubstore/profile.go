package hubstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

// SettingOperatorProfile names the operator's identity in the setting table, a JSON OperatorProfile. Unset reads as
// DefaultOperatorProfile, the hub machine's git identity, until the operator writes a profile.
const SettingOperatorProfile = "operator_profile"

// OperatorProfile is who the hub's work is by: the git author a land and a worker commit as, the name rules speak of,
// the agents that review and the seeds a new project starts from.
type OperatorProfile struct {
	Name      string   `json:"name"`
	Email     string   `json:"email"`
	Display   string   `json:"display,omitempty"`
	Reviewers []string `json:"reviewers,omitempty"`
	Seeds     []string `json:"seeds,omitempty"`
}

// DefaultOperatorProfile is the hub machine's git config --global user.name and user.email, never a baked-in person.
func DefaultOperatorProfile() (OperatorProfile, error) {
	a, err := gitsync.OperatorAuthor(context.Background())
	return OperatorProfile{Name: a.Name, Email: a.Email}, err
}

// Shown is the name rules and briefs use for the operator.
func (p OperatorProfile) Shown() string {
	if p.Display != "" {
		return p.Display
	}
	return p.Name
}

// OperatorProfile reads the profile, the default when none is written.
func (s *Store) OperatorProfile() (OperatorProfile, error) {
	v, err := s.HubSetting(SettingOperatorProfile)
	if err != nil {
		return OperatorProfile{}, err
	}
	var p OperatorProfile
	if err := json.Unmarshal([]byte(v), &p); v == "" || err != nil || p.Name == "" || p.Email == "" {
		return DefaultOperatorProfile()
	}
	return p, nil
}

// SetOperatorProfile writes the profile. A name and an email are the least a commit needs.
func (s *Store) SetOperatorProfile(p OperatorProfile) error {
	p.Name, p.Email, p.Display = strings.TrimSpace(p.Name), strings.TrimSpace(p.Email), strings.TrimSpace(p.Display)
	if p.Name == "" || p.Email == "" {
		return refuse(errors.New("a profile needs a name and an email"))
	}
	raw, _ := json.Marshal(p)
	return s.SetHubSetting(SettingOperatorProfile, string(raw))
}
