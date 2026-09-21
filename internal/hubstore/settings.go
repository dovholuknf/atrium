package hubstore

import (
	"database/sql"
	"strings"
	"time"
)

// The hub's own settings, kept in the `hub_setting` table the schema already
// carries. Keyed by name for the same reason the room's are: a column per
// setting is a migration per setting.
//
// ── why the hub owns a board skin at all ─────────────────
//
// A skin is about the BOARD's appearance, and `docs/hub-room-requirements.md`
// gives anything about the board to the hub. The ALL view is the hub's view, so
// the skin it wears is the hub's own, not one borrowed from whichever room
// sorts first. That borrow is the bug: with a second room mid-attach the ALL
// view would swap to that room's skin and back, and a save from ALL had no room
// to land in and was refused. See `internal/link/fanout.go`.
//
// This does not break "the hub holds nothing": that rule is about WORK, which
// is sessions, terminals and agents. A board rendering preference is none of
// those, and the hub already owns the board assets, its hash, auth and
// overlays.

// SettingBoardSkin names the hub's board skin in the setting table.
const SettingBoardSkin = "board_skin"

// SettingBoardAuto names the hub's board-wide auto-approve flag in the setting
// table.
//
// ── why the hub owns this, and the room keeps its own ────
//
// Board-wide "approve everything" is BOARD POLICY: one answer for every session
// on every room, including ones that have not started. `docs/hub-room-requirements.md`
// gives anything about the board to the hub, so this is the hub's to hold, the
// same as the skin above. It used to live in the room as `global_auto`, which
// only ever covered sessions THAT room's gate had seen and, in the ALL view, had
// no room to be written to at all. That is the bug this moves off the room.
//
// The room's own `global_auto` STAYS as the per-room switch: a room-scoped view
// still turns that room loose on its own. This flag is the wider one, enforced
// hub-side on the permission relay. See `internal/link/autoapprove.go`.
const SettingBoardAuto = "board_auto"

// boardAutoUntil marks a board-auto value that carries a deadline, encoded into
// the value rather than given a second key. Mirrors the room's own `until:`
// scheme (see store.SettingGlobalAuto) so the two read the same way: one thing
// to write, one thing to read, and a deadline with the switch off cannot be
// represented.
const boardAutoUntil = "until:"

// The credential a PUBLIC zrok board share is created behind.
//
// ── why the hub owns this, like the skin ─────────────────
//
// A public zrok share is a URL anyone can open, so it carries a login at the
// edge. `docs/ziti-zrok-flow-design.md` (decision 1) gives the operator two
// choices for that login, per share: zrok `updb` (a username and password) or
// OIDC. Either way it is BOARD POLICY, one answer for the one board, so it is
// the hub's to hold, the same as the skin and the board-wide auto flag above.
//
// zrok private and OpenZiti carry no credential: the overlay is already the
// gate. Only the public zrok channel needs this, and it is enforced at the zrok
// edge, not by the hub binary, which grows no login system of its own.
//
// ── why the password is stored in the clear ──────────────
//
// The hub hands the username and password to the zrok controller every time it
// creates the share, which is on every hub start (exposure survives a restart).
// A one-way hash cannot be replayed to zrok, so this is a credential the hub
// must be able to read back, not one it only ever compares. It is never sent to
// the board: the GET reports only WHETHER a password is set, so a stored
// password does not leave the machine over the board it protects.
const (
	// SettingShareAuth names the public-share login scheme: "updb", "oidc" or
	// "" for none. Empty means a public share has no login and, per the design,
	// must be refused at the point it would be created.
	SettingShareAuth = "share_auth"
	// SettingShareUser and SettingSharePass are the zrok updb username and
	// password, used when SettingShareAuth is "updb".
	SettingShareUser = "share_user"
	SettingSharePass = "share_pass"
	// SettingShareOIDC names the OIDC provider zrok fronts the share with, used
	// when SettingShareAuth is "oidc". The provider itself is configured on the
	// zrok account; this is only which one to name on the share.
	SettingShareOIDC = "share_oidc"
)

// ShareAuth is the public-share login as one value, so the share-creation call
// site reads it in one place rather than assembling four settings itself.
type ShareAuth struct {
	// Scheme is "updb", "oidc" or "" (none).
	Scheme string
	// User and Pass are the zrok updb credential, set only for the updb scheme.
	User string
	Pass string
	// OIDCProvider is the zrok OIDC provider name, set only for the oidc scheme.
	OIDCProvider string
}

// ShareAuth reads the public-share login as one value.
func (s *Store) ShareAuth() (ShareAuth, error) {
	var out ShareAuth
	for field, dst := range map[string]*string{
		SettingShareAuth: &out.Scheme,
		SettingShareUser: &out.User,
		SettingSharePass: &out.Pass,
		SettingShareOIDC: &out.OIDCProvider,
	} {
		v, err := s.HubSetting(field)
		if err != nil {
			return ShareAuth{}, err
		}
		*dst = v
	}
	return out, nil
}

// SetShareAuth writes the public-share login. The scheme decides which of the
// other fields matter; the rest are stored as given so switching schemes and
// switching back does not lose what was typed.
func (s *Store) SetShareAuth(a ShareAuth) error {
	for field, val := range map[string]string{
		SettingShareAuth: a.Scheme,
		SettingShareUser: a.User,
		SettingShareOIDC: a.OIDCProvider,
	} {
		if err := s.SetHubSetting(field, val); err != nil {
			return err
		}
	}
	// The password is written only when one is given, so saving the username
	// alone does not blank an already-set password. Clearing it needs an
	// explicit empty, which SetSharePass below is for.
	if a.Pass != "" {
		return s.SetHubSetting(SettingSharePass, a.Pass)
	}
	return nil
}

// SetSharePass writes the updb password on its own, including clearing it. Kept
// separate from SetShareAuth so a settings save that does not mention the
// password leaves the stored one alone, the way a password box that is left
// blank should.
func (s *Store) SetSharePass(pass string) error {
	return s.SetHubSetting(SettingSharePass, pass)
}

// HubSetting reads one hub setting. A name never written reads as empty rather
// than as an error, so a caller does not have to seed anything.
func (s *Store) HubSetting(name string) (string, error) {
	var v string
	err := s.guard(func() error {
		err := s.db.QueryRow(`SELECT value FROM hub_setting WHERE name = ?`, name).Scan(&v)
		if err == sql.ErrNoRows {
			v = ""
			return nil
		}
		return err
	})
	return v, err
}

// SetHubSetting writes one hub setting, trimming surrounding space so a stored
// name matches what the board compares against.
func (s *Store) SetHubSetting(name, value string) error {
	value = strings.TrimSpace(value)
	return s.guard(func() error {
		_, err := s.db.Exec(
			`INSERT INTO hub_setting (name, value, updated_at) VALUES (?, ?, ?)
			 ON CONFLICT(name) DO UPDATE SET value = excluded.value,
			                                 updated_at = excluded.updated_at`,
			name, value, ts(now()))
		return err
	})
}

// HubSkin is the skin the ALL view wears. Empty means unset, which the serving
// side reads as the default: a fresh hub looks exactly as it did before this
// existed.
func (s *Store) HubSkin() (string, error) { return s.HubSetting(SettingBoardSkin) }

// SetHubSkin records the skin the ALL view wears.
//
// The name is not validated here against the shipped skin list, because this
// package cannot import that list without pulling the board's HTTP layer into
// the hub's store. The serving side clamps an unknown name back to the default
// on the way out, so a bad value never leaves the board on a skin nothing
// matches. The board is the only writer and only sends names the daemon offered.
func (s *Store) SetHubSkin(name string) error { return s.SetHubSetting(SettingBoardSkin, name) }

// BoardAuto reports whether board-wide auto-approve is on, and when it stops.
//
// A read failure answers false, and a deadline that has passed answers false,
// both checked against the clock here rather than enforced by a timer. This sits
// on the permission path, and the safe answer to "should the hub stop asking" is
// no: a timer that has to fire is a timer that does not fire across a restart,
// and the flag surviving a restart it should not have is the failure worth
// designing against. Mirrors store.GlobalAutoUntil exactly.
//
// The second value is nil when it is on with no deadline, which is what turning
// it on by hand means.
func (s *Store) BoardAuto() (bool, *time.Time, error) {
	v, err := s.HubSetting(SettingBoardAuto)
	if err != nil {
		return false, nil, err
	}
	if v == "on" {
		return true, nil, nil
	}
	if !strings.HasPrefix(v, boardAutoUntil) {
		return false, nil, nil
	}
	deadline, err := time.Parse(TimeFormat, strings.TrimPrefix(v, boardAutoUntil))
	if err != nil {
		// A value that will not parse is not a licence to approve everything.
		return false, nil, nil
	}
	if !now().Before(deadline) {
		return false, &deadline, nil
	}
	return true, &deadline, nil
}

// SetBoardAuto turns board-wide auto-approve on or off, with an optional
// deadline. Turning it off always clears the deadline, for the same reason a
// card's does: "off until Tuesday" is not a thing anybody means.
func (s *Store) SetBoardAuto(on bool, until *time.Time) error {
	if !on {
		return s.SetHubSetting(SettingBoardAuto, "off")
	}
	if until == nil {
		return s.SetHubSetting(SettingBoardAuto, "on")
	}
	return s.SetHubSetting(SettingBoardAuto, boardAutoUntil+ts(*until))
}
