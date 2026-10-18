package hubstore

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// What the hub heard from CI, and who it told. See docs/fabric/ci-hook-design.md.
//
// THE REQUEST BODY IS NEVER STORED. A run is the handful of fields the hub parsed out of it, and a delivery is the
// one line the hub built from them.

const (
	SettingCISecret          = "ci_hook.secret_sha256"
	SettingCISecretPrev      = "ci_hook.secret_prev_sha256"
	SettingCISecretPrevUntil = "ci_hook.secret_prev_until"
	SettingCIRoutes          = "ci_hook.routes"
	SettingCIRepos           = "ci_hook.repos"
)

// CISecretGrace is how long the previous secret stays good after a rotation, so changing the GitHub secret is not a
// race.
const CISecretGrace = 24 * time.Hour

// CIKeep is how long a run and its deliveries are kept.
const CIKeep = 30 * 24 * time.Hour

// ErrCISecretExists is `atrium ci-hook secret` without --rotate on a hub that already has one.
var ErrCISecretExists = errors.New("this hub already has a ci-hook secret. --rotate mints another and keeps the old " +
	"one good for 24 hours")

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// MintCIHookSecret makes a new 32-byte secret and returns it in the clear, once. Only its SHA-256 is stored. With
// rotate, the hash it replaces stays valid for CISecretGrace.
func (s *Store) MintCIHookSecret(rotate bool) (string, error) {
	cur, err := s.HubSetting(SettingCISecret)
	if err != nil {
		return "", err
	}
	if cur != "" && !rotate {
		return "", ErrCISecretExists
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	secret := base64.RawURLEncoding.EncodeToString(raw[:])
	if cur != "" {
		if err := s.SetHubSetting(SettingCISecretPrev, cur); err != nil {
			return "", err
		}
		if err := s.SetHubSetting(SettingCISecretPrevUntil, ts(s.now().Add(CISecretGrace))); err != nil {
			return "", err
		}
	}
	if err := s.SetHubSetting(SettingCISecret, hashSecret(secret)); err != nil {
		return "", err
	}
	return secret, nil
}

// CIHookSecretOK is whether a presented secret is the current one, or the previous one inside its grace. The compare is
// on the hashes and in constant time. `why` says what was wrong, for the audit line: `no secret` or `wrong secret`.
func (s *Store) CIHookSecretOK(presented string) (ok bool, why string, err error) {
	cur, err := s.HubSetting(SettingCISecret)
	if err != nil {
		return false, "", err
	}
	if cur == "" {
		return false, "no secret configured", nil
	}
	if presented == "" {
		return false, "no secret", nil
	}
	got := hashSecret(presented)
	if constEq(got, cur) {
		return true, "", nil
	}
	prev, err := s.HubSetting(SettingCISecretPrev)
	if err != nil {
		return false, "", err
	}
	if prev != "" {
		until, err := s.HubSetting(SettingCISecretPrevUntil)
		if err != nil {
			return false, "", err
		}
		if at := parseOrNil(until); at != nil && s.now().Before(*at) && constEq(got, prev) {
			return true, "", nil
		}
	}
	return false, "wrong secret", nil
}

func constEq(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// CIHookRoutes is the stored routes JSON, empty when none were set.
func (s *Store) CIHookRoutes() (string, error) { return s.HubSetting(SettingCIRoutes) }

// SetCIHookRoutes stores the routes JSON. The caller has validated it.
func (s *Store) SetCIHookRoutes(raw string) error { return s.SetHubSetting(SettingCIRoutes, raw) }

// CIHookRepos is the list of repositories allowed to report, empty meaning any.
func (s *Store) CIHookRepos() ([]string, error) {
	raw, err := s.HubSetting(SettingCIRepos)
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil, err
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetCIHookRepos stores the allowed repositories.
func (s *Store) SetCIHookRepos(repos []string) error {
	raw, err := json.Marshal(repos)
	if err != nil {
		return err
	}
	return s.SetHubSetting(SettingCIRepos, string(raw))
}

// CIRun is one finished workflow run the hub was told about.
type CIRun struct {
	ID         string
	Repo       string
	Workflow   string
	Branch     string
	SHA        string
	RunID      int64
	Attempt    int
	Conclusion string
	URL        string
	Auth       string
	ReceivedAt time.Time
}

// CIRunAdd records a run. A repeat of (repo, run id, attempt) is dup and nothing is written.
func (s *Store) CIRunAdd(r CIRun) (CIRun, bool, error) {
	r.ID = newID()
	r.ReceivedAt = s.now()
	var dup bool
	err := s.guard(func() error {
		res, err := s.db.Exec(
			`INSERT INTO ci_run (id, repo, workflow, branch, sha, run_id, attempt, conclusion, url, auth, received_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT (repo, run_id, attempt) DO NOTHING`,
			r.ID, r.Repo, r.Workflow, r.Branch, r.SHA, r.RunID, r.Attempt, r.Conclusion, r.URL, r.Auth, ts(r.ReceivedAt))
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		dup = n == 0
		return nil
	})
	return r, dup, err
}

// CILastConclusion is the conclusion of the newest run recorded for a repo, workflow and branch.
func (s *Store) CILastConclusion(repo, workflow, branch string) (string, bool, error) {
	var c string
	err := s.guard(func() error {
		err := s.db.QueryRow(
			`SELECT conclusion FROM ci_run WHERE repo = ? AND workflow = ? AND branch = ?
			 ORDER BY received_at DESC, id DESC LIMIT 1`, repo, workflow, branch).Scan(&c)
		if errors.Is(err, sql.ErrNoRows) {
			c = ""
			return nil
		}
		return err
	})
	return c, c != "", err
}

// Delivery states.
const (
	CIPending     = "pending"
	CIDelivered   = "delivered"
	CIUnconfirmed = "unconfirmed"
	CIExpired     = "expired"
	CIRefused     = "refused"
	CIFolded      = "folded"
)

// CIDelivery is one target of one run. Repo, Workflow, Branch and RunID are the run's, read through the join.
type CIDelivery struct {
	ID       string
	RunRef   string
	Target   string
	Kind     string
	Wake     bool
	Text     string
	State    string
	Answer   string
	Card     string
	Attempts int
	Created  time.Time
	Updated  time.Time

	Repo     string
	Workflow string
	Branch   string
	RunID    int64
	Attempt  int
}

// CIDeliveryAdd records a target for a run, pending.
func (s *Store) CIDeliveryAdd(d CIDelivery) (string, error) {
	d.ID = newID()
	now := ts(s.now())
	err := s.guard(func() error {
		_, err := s.db.Exec(
			`INSERT INTO ci_delivery (id, run_ref, target, kind, wake, text, state, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, 'pending', ?, ?)`,
			d.ID, d.RunRef, d.Target, d.Kind, b2i(d.Wake), d.Text, now, now)
		return err
	})
	return d.ID, err
}

const ciDeliveryCols = `d.id, d.run_ref, d.target, d.kind, d.wake, d.text, d.state, d.answer, d.card, d.attempts,
	d.created_at, d.updated_at, r.repo, r.workflow, r.branch, r.run_id, r.attempt`

func scanCIDelivery(sc scanner) (CIDelivery, error) {
	var d CIDelivery
	var wake int
	var created, updated string
	if err := sc.Scan(&d.ID, &d.RunRef, &d.Target, &d.Kind, &wake, &d.Text, &d.State, &d.Answer, &d.Card, &d.Attempts,
		&created, &updated, &d.Repo, &d.Workflow, &d.Branch, &d.RunID, &d.Attempt); err != nil {
		return d, err
	}
	d.Wake = wake != 0
	if t := parseOrNil(created); t != nil {
		d.Created = *t
	}
	if t := parseOrNil(updated); t != nil {
		d.Updated = *t
	}
	return d, nil
}

func (s *Store) ciDeliveries(where string, args ...any) ([]CIDelivery, error) {
	var out []CIDelivery
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT `+ciDeliveryCols+` FROM ci_delivery d JOIN ci_run r ON r.id = d.run_ref `+
			where, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			d, err := scanCIDelivery(rows)
			if err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	return out, err
}

// CIDeliveriesPending is every delivery still to be made, oldest run first.
func (s *Store) CIDeliveriesPending() ([]CIDelivery, error) {
	return s.ciDeliveries(`WHERE d.state = 'pending' ORDER BY r.received_at, d.created_at, d.id`)
}

// CIDeliveriesOf is every delivery of one run, in the order they were added.
func (s *Store) CIDeliveriesOf(runRef string) ([]CIDelivery, error) {
	return s.ciDeliveries(`WHERE d.run_ref = ? ORDER BY d.created_at, d.id`, runRef)
}

// CIDeliverySet moves a delivery to a state, with the room's answer and the card it resolved to.
func (s *Store) CIDeliverySet(id, state, answer, card string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(
			`UPDATE ci_delivery SET state = ?, answer = ?, card = ?, attempts = attempts + 1, updated_at = ?
			 WHERE id = ?`, state, answer, card, ts(s.now()), id)
		return err
	})
}

// CISweep drops runs older than CIKeep and their deliveries.
func (s *Store) CISweep() (int64, error) {
	var n int64
	cut := ts(s.now().Add(-CIKeep))
	err := s.guard(func() error {
		if _, err := s.db.Exec(
			`DELETE FROM ci_delivery WHERE run_ref IN (SELECT id FROM ci_run WHERE received_at < ?)`, cut); err != nil {
			return err
		}
		res, err := s.db.Exec(`DELETE FROM ci_run WHERE received_at < ?`, cut)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		return nil
	})
	return n, err
}
