package hubstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/cardcolors"
	"github.com/dovholuknf/atrium/internal/gitsync"
)

// The hub's own settings, kept in the `hub_setting` table the schema already
// carries. Keyed by name for the same reason the room's are: a column per
// setting is a migration per setting.
//
// ── why the hub owns a board skin at all ─────────────────
//
// A skin is about the BOARD's appearance, and `docs/archive/hub-room-requirements.md`
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

// SettingOverlayLegacy names the switch for the old, certificate-less room link
// over ziti and zrok (f-022).
//
// EMPTY OR `allow` KEEPS TODAY'S BEHAVIOUR, and `refuse` turns away a room that
// connects without a certificate. It is only ever set by a person, through
// `atrium rooms legacy`, and never flipped by anything the hub does: the whole
// point is that the operator chooses the moment after seeing who is still on the
// old path.
const SettingOverlayLegacy = "overlay_legacy"

// Values SettingOverlayLegacy takes. Anything else reads as allow, so a stray
// value can never lock a room out.
const (
	OverlayLegacyAllow  = "allow"
	OverlayLegacyRefuse = "refuse"
)

// SetOverlayLegacy writes the switch. Only allow and refuse are accepted.
func (s *Store) SetOverlayLegacy(value string) error {
	v := strings.ToLower(strings.TrimSpace(value))
	if v != OverlayLegacyAllow && v != OverlayLegacyRefuse {
		return errors.New("overlay_legacy is allow or refuse")
	}
	return s.SetHubSetting(SettingOverlayLegacy, v)
}

// OverlayLegacyRefused reports whether the old overlay path is switched off.
func (s *Store) OverlayLegacyRefused() (bool, error) {
	v, err := s.HubSetting(SettingOverlayLegacy)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(v), OverlayLegacyRefuse), nil
}

// SettingBoardAuto names the hub's board-wide auto-approve flag in the setting
// table.
//
// ── why the hub owns this, and the room keeps its own ────
//
// Board-wide "approve everything" is BOARD POLICY: one answer for every session
// on every room, including ones that have not started. `docs/archive/hub-room-requirements.md`
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
// edge. `docs/fabric/ziti-zrok-flow-design.md` (decision 1) gives the operator two
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

// SettingGitRepos names the repositories this hub mirrors and serves to its rooms, a JSON
// list of {name, checkout, branch}. Key-value like every other hub setting, so it needs no
// migration. See internal/gitsync and docs/fabric/git-sync-design.md.
const SettingGitRepos = "git_repos"

// GitRepos reads the list, refusing an entry that is not allowed, with why. A refusal here
// is a hub that mirrors nothing and says so, never one that mirrors a branch it should not.
func (s *Store) GitRepos() ([]gitsync.Repo, error) {
	v, err := s.HubSetting(SettingGitRepos)
	if err != nil {
		return nil, err
	}
	return gitsync.ParseRepos(v)
}

// SetGitRepos writes the list as JSON after validating it, so a branch that is not the
// integration branch is refused when it is typed and not when it is next mirrored.
func (s *Store) SetGitRepos(raw string) error {
	repos, err := gitsync.ParseRepos(raw)
	if err != nil {
		return err
	}
	if repos == nil {
		return s.SetHubSetting(SettingGitRepos, "")
	}
	out, err := json.Marshal(repos)
	if err != nil {
		return err
	}
	return s.SetHubSetting(SettingGitRepos, string(out))
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

// The hub's own git store (docs/fabric/hub-forge-design.md 3.1). Two settings in the same key-value
// table as every other, so there is NO MIGRATION: a name never written reads as its default.
const (
	// SettingGitStore is where the hub keeps its bare repositories, `<store>/<host>/<owner>/<repo>.git`.
	// Empty is `<hub atrium-dir>/git`.
	SettingGitStore = "git.store"
	// SettingGitCreateOnPush is whether a room's first push of a repository the hub does not have may
	// create it. Off until the operator turns it on. This item only stores it: f-new-hub-receive reads it.
	SettingGitCreateOnPush = "git.create_on_push"
)

// GitStorePath is where the hub's git store is: the setting, or `<hubDir>/git`.
func (s *Store) GitStorePath(hubDir string) (string, error) {
	v, err := s.HubSetting(SettingGitStore)
	if err != nil {
		return "", err
	}
	if v = strings.TrimSpace(v); v != "" {
		return v, nil
	}
	return filepath.Join(hubDir, "git"), nil
}

// SetGitStore sets the store's directory, which has to be an absolute path on the hub's machine.
// Empty puts it back to the default. Nothing already in the old directory is moved.
func (s *Store) SetGitStore(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return s.SetHubSetting(SettingGitStore, "")
	}
	if strings.ContainsAny(dir, "\x00\r\n") || !filepath.IsAbs(filepath.FromSlash(dir)) {
		return errors.New("git.store has to be an absolute path on the hub's machine")
	}
	clean := filepath.Clean(filepath.FromSlash(dir))
	if clean == filepath.Dir(clean) {
		return errors.New("git.store cannot be the root of a disk")
	}
	if st, err := os.Stat(clean); err == nil && !st.IsDir() {
		return errors.New("git.store is a file, and it has to be a directory")
	}
	return s.SetHubSetting(SettingGitStore, filepath.ToSlash(clean))
}

// GitCreateOnPush reads whether a first push may create a repository. A value that is not on reads
// as off, and so does a store that cannot answer: creating a repository is the thing to withhold.
func (s *Store) GitCreateOnPush() (bool, error) {
	v, err := s.HubSetting(SettingGitCreateOnPush)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(v), "on"), nil
}

// SetGitCreateOnPush turns it on or off.
func (s *Store) SetGitCreateOnPush(on bool) error {
	if on {
		return s.SetHubSetting(SettingGitCreateOnPush, "on")
	}
	return s.SetHubSetting(SettingGitCreateOnPush, "off")
}

// HubCardColors is the board's card colours, which the hub owns like the skin: colours belong to the
// board, not to whichever room sorts first. A row never set is seeded once; one set to nothing stays so.
func (s *Store) HubCardColors() (cardcolors.Colors, error) {
	raw, err := s.HubSetting(cardcolors.Setting)
	if err != nil {
		return cardcolors.Colors{Repos: map[string]string{}}, err
	}
	c, seed := cardcolors.Load(raw)
	if seed {
		if v, err := cardcolors.Encode(c); err == nil {
			_ = s.SetHubSetting(cardcolors.Setting, v)
		}
	}
	return c, nil
}

// SetHubCardColors validates and stores the board's card colours.
func (s *Store) SetHubCardColors(c cardcolors.Colors) error {
	v, err := cardcolors.Encode(c)
	if err != nil {
		return err
	}
	return s.SetHubSetting(cardcolors.Setting, v)
}
