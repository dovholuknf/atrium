package daemon

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/store"
)

// Atrium's configuration as one file a repository can hold.
//
// The point is a machine that can be rebuilt from a checkout: the harnesses,
// the fixtures, the sources, the actions, the rules, the skin, the timers.
// `docs/scm-design.md` has the design and this is the outbound half of it.
//
// THE HARD PART IS WHAT MUST NOT LEAVE, and the failure mode is why. A secret
// pushed to a repository is still in the history after it is deleted, so this
// has exactly one chance to be right, on a machine nobody is watching, months
// after anybody read this comment.
//
// Two independent defences, and they fail differently on purpose:
//
//  1. THE ALLOWLIST IS THE STRUCTURE. Nothing here marshals a stored struct
//     and strips fields out of it. Every value is copied across by name into a
//     type declared in this file. A field added to `ZrokConfig` next year is
//     absent from the export because nobody wrote a line to include it, which
//     is the safe direction, and it needs no rule to remember.
//
//  2. THE RESULT IS SEARCHED FOR SECRETS BEFORE IT IS RETURNED. The allowlist
//     cannot help with a credential somebody typed into a field that is
//     legitimately exported, and they do: a source is an operator-authored
//     command line, and a command line is where a token goes. So the finished
//     document is scanned for the shapes credentials have, and a hit REFUSES
//     the whole export naming the field. Refusing is right. A redacted export
//     that silently drops something is an export somebody restores from and
//     finds half a configuration.

// ExportVersion is the shape of the document, so an importer written later can
// refuse one it does not understand rather than half applying it.
const ExportVersion = 1

// Export is everything atrium will hand to a repository.
type Export struct {
	Version    int    `json:"version"`
	ExportedAt string `json:"exported_at"`
	// Tenant is this atrium's name for itself, which is configuration rather
	// than identity: it prefixes wire names and nothing authenticates with it.
	Tenant string `json:"tenant,omitempty"`

	Settings  map[string]string   `json:"settings"`
	Harnesses []*store.Harness    `json:"harnesses"`
	Fixtures  []*store.Fixture    `json:"fixtures"`
	Sources   []*store.Source     `json:"sources"`
	Actions   []*store.CardAction `json:"actions"`
	Rules     []*store.Rule       `json:"rules"`
	// Recognisers are the rows that say what a URL means. Exported for the same
	// reason a source is: the understanding of somebody else's ticketing system
	// lives in a template rather than in the binary, and that understanding is
	// the part worth reviewing, diffing and keeping.
	//
	// A recogniser's `cwd` names this machine's worktree layout, and a `fetch`
	// is operator-authored free text. Both are the flagged category rather than
	// the omitted one: an export with them stripped restores a table that
	// matches URLs and points nowhere.
	Recognisers []*store.Recogniser `json:"recognisers"`
	Overlays    ExportOverlays      `json:"overlays"`
}

// ExportOverlays is the overlay configuration WITHOUT the parts that are
// credentials, written out field by field.
//
// Read this beside `ZrokConfig` and `ZitiConfig`. What is missing is the point:
// no account token, no share token, no identity path, no enrollment JWT. The
// share token is left out even though it is not a password, because it is an
// address somebody may be holding right now and a repository is not where an
// address like that belongs.
type ExportOverlays struct {
	Zrok ExportZrok `json:"zrok"`
	Ziti ExportZiti `json:"ziti"`
}

// ExportZrok is the shape of a zrok configuration, minus everything that lets
// anybody act as this account.
type ExportZrok struct {
	// Public and Private are what sharing is allowed to do here.
	Public  bool `json:"public"`
	Private bool `json:"private"`
	// OwnEnvironment says whether atrium keeps its own environment. Which one
	// it uses is configuration. The token inside it is not.
	OwnEnvironment bool `json:"own_environment"`
	// ApiEndpoint is which zrok instance. A public address, and the thing a
	// rebuilt machine most needs to be told.
	ApiEndpoint string `json:"api_endpoint,omitempty"`

	// NOT EXPORTED, and each for its own reason:
	//
	//   Name        a reserved address somebody may be holding. Rebuilding a
	//               machine should not silently reclaim it.
	//   ShareToken  the same, for a private share.
	//   Backend     this machine's own listening address, which means nothing
	//               anywhere else.
	//   Extra       free text passed to zrok, so anything can be in it.
}

// ExportZiti is a ziti configuration with no identity in it.
//
// AN IDENTITY IS A PRIVATE KEY. The configured value is a path, and a path is
// not a key, but exporting it puts the location of a key into a repository and
// invites an importer to point a second machine at the same one. The service
// name is public and is the useful half.
type ExportZiti struct {
	Service string `json:"service,omitempty"`
}

// exportedSettings is the allowlist for the settings table, by key.
//
// A LIST rather than "everything except", because the settings table is where
// a value with no home ends up, and the next thing put in it is not going to
// be considered against this comment. Anything not named here does not leave.
var exportedSettings = []string{
	store.SettingSweepDead,
	store.SettingPruneAfter,
	api.SettingBrowseRoots,
	api.SettingBoardSkin,
	api.SettingScrollbackLines,
	api.SettingEditor,
	api.SettingPasteKeep,
	api.SettingPastePreamble,
	SettingShellCommand,
}

// neverExported names the settings keys that must not leave, so the test that
// guards this has something to assert against rather than a list of what is
// allowed.
//
// `global_auto` is here for a different reason from the rest: it is not a
// secret, it is a STATE. Whether this machine is approving everything right
// now is not configuration to restore onto another one, and an import that
// silently turned it on would be the worst possible thing to restore.
var neverExported = []string{
	store.SettingGlobalAuto,
	SettingOverlayZrok,
	SettingOverlayZiti,
	// The key that signs session cookies. Anybody holding it can mint a
	// session for any subject, which is every bit as good as the login it
	// stands behind.
	SettingAuthKey,
	// And the auth configuration, because it holds a client secret. The
	// issuer and the allow list would be safe on their own, which is exactly
	// the trap: a per-file rule would have to choose between exporting the
	// secret and exporting nothing, so the blob stays.
	SettingAuth,
}

// BuildExport gathers the configuration, or refuses and says why.
func (d *Daemon) BuildExport() (*Export, error) {
	out := &Export{
		Version:    ExportVersion,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Tenant:     d.st.Tenant(),
		Settings:   map[string]string{},
	}

	for _, key := range exportedSettings {
		v, err := d.st.Setting(key)
		if err != nil {
			return nil, fmt.Errorf("could not read the setting %q: %w", key, err)
		}
		// Absent rather than empty. A key nobody has ever set should not appear
		// in a file that is going to be read as "this is how the machine is".
		if strings.TrimSpace(v) != "" {
			out.Settings[key] = v
		}
	}

	var err error
	if out.Harnesses, err = d.st.Harnesses(); err != nil {
		return nil, err
	}
	if out.Fixtures, err = d.st.Fixtures(); err != nil {
		return nil, err
	}
	if out.Sources, err = d.st.Sources(); err != nil {
		return nil, err
	}
	if out.Actions, err = d.st.CardActions(); err != nil {
		return nil, err
	}
	if out.Rules, err = d.st.Rules(); err != nil {
		return nil, err
	}
	if out.Recognisers, err = d.st.Recognisers(); err != nil {
		return nil, err
	}

	// FIELD BY FIELD, which is the allowlist. See the header.
	zrok := d.zrokConfig()
	out.Overlays.Zrok = ExportZrok{
		Public:         zrok.Public,
		Private:        zrok.Private,
		OwnEnvironment: zrok.OwnEnvironment,
		ApiEndpoint:    zrok.ApiEndpoint,
	}
	out.Overlays.Ziti = ExportZiti{Service: d.zitiConfig().Service}

	if err := refuseIfItLooksLikeASecret(out); err != nil {
		return nil, err
	}
	return out, nil
}

// secretShapes are the patterns that mean somebody put a credential where
// configuration goes.
//
// Deliberately shaped rather than exhaustive. This is the second defence and
// it is guarding against a human typing a token into a source's command line,
// not against an attacker: nothing here would stop somebody determined to
// export a secret, and it is not supposed to. What it stops is the accident.
//
// The named prefixes are cheap and catch the common services. The long-blob
// rule is the one that earns its place, and its threshold is set high enough
// that a base64 line in a legitimate command is unlikely and a real token is
// caught: an API key that is short enough to slip under it is short enough to
// be guessable anyway.
var secretShapes = []struct {
	name string
	re   *regexp.Regexp
}{
	{"a private key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"a github token", regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{16,}`)},
	{"a slack token", regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`)},
	{"an aws access key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"a json web token", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.`)},
	{"something that looks like a key", regexp.MustCompile(`\b[A-Za-z0-9+/]{40,}={0,2}\b`)},
}

// refuseIfItLooksLikeASecret searches the finished document and stops the
// export if anything in it has the shape of a credential.
//
// The whole document, serialised, rather than a walk over the fields. A walk
// has to be kept in step with the types and this cannot: anything that ends up
// in the file is searched, including a field somebody adds later and forgets
// to think about.
//
// The error names WHERE, by finding the key whose value matched, because
// "your export contains a secret" with no location is an error somebody has to
// go hunting behind.
func refuseIfItLooksLikeASecret(out *Export) error {
	raw, err := json.Marshal(out)
	if err != nil {
		return err
	}
	for _, s := range secretShapes {
		hit := s.re.Find(raw)
		if hit == nil {
			continue
		}
		return fmt.Errorf("this export was stopped because something in it looks like %s, "+
			"and a secret pushed to a repository is still in its history after it is "+
			"deleted. it appears near %q. take it out of the configuration and use an "+
			"environment variable there instead", s.name, whereabouts(raw, hit))
	}
	return nil
}

// whereabouts is a short window of the document around a match, with the match
// itself removed.
//
// THE MATCH IS NOT ECHOED. An error message goes to a log, a terminal and
// probably a screenshot, and printing the credential to explain that it must
// not be printed is the joke that writes itself. What comes back is enough
// context to find it.
func whereabouts(raw, hit []byte) string {
	at := strings.Index(string(raw), string(hit))
	if at < 0 {
		return "somewhere in the export"
	}
	start := at - 60
	if start < 0 {
		start = 0
	}
	before := string(raw[start:at])
	// The nearest key before the match, which is the field it is in.
	if i := strings.LastIndex(before, `"`); i > 0 {
		if j := strings.LastIndex(before[:i], `"`); j >= 0 {
			return before[j+1 : i]
		}
	}
	return strings.TrimSpace(before)
}

// ExportSettingKeys is the allowlist, sorted, for anything that needs to state
// it rather than apply it. Copied rather than returned directly so a caller
// cannot reorder or extend the real one.
func ExportSettingKeys() []string {
	out := append([]string(nil), exportedSettings...)
	sort.Strings(out)
	return out
}

// NeverExportedKeys is the other half, for the same reason.
func NeverExportedKeys() []string {
	out := append([]string(nil), neverExported...)
	sort.Strings(out)
	return out
}
