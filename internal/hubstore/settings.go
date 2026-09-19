package hubstore

import (
	"database/sql"
	"strings"
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
