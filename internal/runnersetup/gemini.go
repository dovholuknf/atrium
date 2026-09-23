package runnersetup

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Gemini, measured against gemini-cli 0.60.0 by reading the installed bundle
// (`packages/core/dist/src/utils/trust.js` inside it), not the docs.
//
//   - Trust is `~/.gemini/trustedFolders.json`, a flat object from a path to
//     TRUST_FOLDER, TRUST_PARENT or DO_NOT_TRUST. GEMINI_CLI_TRUSTED_FOLDERS_PATH
//     moves the file, GEMINI_CLI_HOME moves the whole `~/.gemini`.
//   - TRUST_FOLDER covers the path and its subtree. TRUST_PARENT covers the
//     path's parent and its subtree. The longest matching rule key wins.
//   - Keys are normalised on read: absolute, symlinks resolved, lowercased on
//     Windows and macOS.
//   - An unknown trust level or a file that is not an object is FATAL to
//     gemini, so atrium never writes one and refuses to edit one.
//   - `security.folderTrust.enabled` in settings.json defaults to true. Off
//     trusts everything. GEMINI_CLI_TRUST_WORKSPACE=true|false and
//     GEMINI_RESTRICTED_MODE=true override the file.
//   - Sign-in is `security.auth.selectedType`. An API key comes from
//     GEMINI_API_KEY, a `.env` file, or the OS keychain with
//     `gemini-credentials.json` as its encrypted fallback. A Google sign-in is
//     `oauth_creds.json`. `~/.gemini/.env` is read only in a trusted folder.
var Gemini = &Adapter{
	ID: "gemini", Label: "gemini", Cmds: []string{"gemini"}, Package: "@google/gemini-cli",
	Checks: []Check{
		{ID: "trust", Label: "trusts the workspace", Run: geminiTrustCheck, Apply: geminiTrustApply},
		{ID: "auth", Label: "signed in", Run: geminiAuthCheck},
		{ID: "hooks", Label: "atrium hooks", Run: func(Env) Result {
			return Result{State: NA, Detail: "atrium has no hooks target for gemini yet, so a gemini " +
				"card shows its terminal and reports nothing about itself."}
		}},
	},
	Launch: geminiLaunch,
}

const (
	trustFolder = "TRUST_FOLDER"
	trustParent = "TRUST_PARENT"
	doNotTrust  = "DO_NOT_TRUST"
)

func geminiDir(env Env) string {
	if h := env.lookup("GEMINI_CLI_HOME"); h != "" {
		return filepath.Join(h, ".gemini")
	}
	return filepath.Join(env.Home, ".gemini")
}

func geminiTrustPath(env Env) string {
	if p := env.lookup("GEMINI_CLI_TRUSTED_FOLDERS_PATH"); p != "" {
		return p
	}
	return filepath.Join(geminiDir(env), "trustedFolders.json")
}

// geminiSettings is the part of settings.json atrium reads. Never written.
type geminiSettings struct {
	Security struct {
		FolderTrust struct {
			Enabled *bool `json:"enabled"`
		} `json:"folderTrust"`
		Auth struct {
			SelectedType string `json:"selectedType"`
		} `json:"auth"`
	} `json:"security"`
}

// readGeminiSettings answers the zero value for a missing file. err is set for
// a file atrium cannot parse, which gemini may still read: it allows comments.
func readGeminiSettings(env Env) (geminiSettings, string, error) {
	var s geminiSettings
	p := filepath.Join(geminiDir(env), "settings.json")
	raw, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return s, p, nil
	}
	if err != nil {
		return s, p, err
	}
	return s, p, json.Unmarshal(raw, &s)
}

// envOverride is what the environment says about trust, or "" when it says
// nothing and the file decides.
func geminiEnvOverride(env Env) string {
	if env.lookup("GEMINI_RESTRICTED_MODE") == "true" || env.lookup("GEMINI_CLI_TRUST_WORKSPACE") == "false" {
		return "distrust"
	}
	if env.lookup("GEMINI_CLI_TRUST_WORKSPACE") == "true" {
		return "trust"
	}
	return ""
}

// trustRules reads trustedFolders.json and checks it the way gemini does, so a
// file gemini would refuse is refused here too, before anything is written.
func trustRules(path string) (map[string]json.RawMessage, map[string]string, []byte, error) {
	obj, raw, _, err := readJSONObject(path)
	if err != nil {
		return nil, nil, raw, err
	}
	rules := map[string]string{}
	for k, v := range obj {
		var level string
		if err := json.Unmarshal(v, &level); err != nil || !knownTrust(level) {
			return nil, nil, raw, fmt.Errorf("%s has %q for %q, which gemini does not accept, so gemini "+
				"will not start until it is fixed", filepath.ToSlash(path), strings.Trim(string(v), `"`), k)
		}
		rules[k] = level
	}
	return obj, rules, raw, nil
}

func knownTrust(level string) bool {
	return level == trustFolder || level == trustParent || level == doNotTrust
}

// normTrust is gemini's normalizePath plus its real-path step: absolute,
// symlinks resolved when the path exists, forward slashes, and lowercased on
// the platforms gemini treats as case-insensitive.
func normTrust(p, goos string) string {
	p = filepath.FromSlash(strings.TrimSpace(p))
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	p = filepath.ToSlash(filepath.Clean(p))
	if goos == "windows" || goos == "darwin" {
		p = strings.ToLower(p)
	}
	return p
}

func isSubpath(parent, child string) bool {
	parent = strings.TrimSuffix(parent, "/")
	return child == parent || strings.HasPrefix(child, parent+"/")
}

// trustLevel is gemini's isPathTrusted: the level of the longest rule whose
// scope holds path, and "" when no rule does.
func trustLevel(rules map[string]string, path, goos string) string {
	loc := normTrust(path, goos)
	best, level := -1, ""
	for rule, lv := range rules {
		scope := rule
		if lv == trustParent {
			scope = filepath.Dir(filepath.FromSlash(rule))
		}
		if isSubpath(normTrust(scope, goos), loc) {
			// Gemini compares the normalised key's length.
			if n := len(normTrust(rule, goos)); n > best {
				best, level = n, lv
			}
		}
	}
	return level
}

func trusted(level string) bool { return level == trustFolder || level == trustParent }

func geminiTrustCheck(env Env) Result {
	path := geminiTrustPath(env)
	r := Result{Path: filepath.ToSlash(path)}
	switch geminiEnvOverride(env) {
	case "trust":
		r.State, r.Detail = OK, "GEMINI_CLI_TRUST_WORKSPACE=true is set, so gemini trusts every folder."
		return r
	case "distrust":
		r.State, r.Fix = Fail, FixExplain
		r.Detail = "GEMINI_RESTRICTED_MODE=true or GEMINI_CLI_TRUST_WORKSPACE=false is set where this " +
			"runner starts, so gemini trusts no folder. atrium will not undo a setting somebody chose. " +
			"remove it from the runner's env or your environment to change it."
		return r
	}
	if s, _, err := readGeminiSettings(env); err == nil && s.Security.FolderTrust.Enabled != nil &&
		!*s.Security.FolderTrust.Enabled {
		r.State, r.Detail = OK, "folder trust is off in gemini's settings, so every folder is trusted."
		return r
	}
	_, rules, _, err := trustRules(path)
	if err != nil {
		r.State, r.Fix, r.Detail = Fail, FixExplain, err.Error()+". fix the file by hand."
		return r
	}
	if len(env.Roots) == 0 {
		r.State = Warn
		r.Detail = "this room has no provider roots, so atrium does not know where your work lives and " +
			"cannot trust it ahead of time. gemini will ask in each new folder. add a provider under " +
			"runners, providers."
		return r
	}
	var untrusted, denied, ok []string
	goos := env.goos()
	for _, root := range env.Roots {
		switch lv := trustLevel(rules, root, goos); {
		case lv == doNotTrust:
			denied = append(denied, root)
		case trusted(lv):
			ok = append(ok, root)
		default:
			untrusted = append(untrusted, root)
		}
	}
	switch {
	case len(untrusted) > 0:
		r.State, r.Fix, r.FixLabel, r.Targets = Fail, FixApply, "trust it", untrusted
		r.Detail = "gemini has never been told to trust " + strings.Join(untrusted, ", ") +
			", so it stops at its trust prompt in every new worktree there. trusting a root once covers " +
			"every folder under it."
	case len(denied) > 0:
		r.State, r.Fix = Fail, FixExplain
		r.Detail = "a DO_NOT_TRUST rule covers " + strings.Join(denied, ", ") + ". atrium will not " +
			"override a no. remove that rule from the file to change it."
	default:
		r.State, r.Detail = OK, "gemini trusts "+strings.Join(ok, ", ")+" and every folder under it."
	}
	return r
}

// addTrust adds one TRUST_FOLDER rule, leaving every existing key alone.
func addTrust(env Env, folder string) (Applied, error) {
	path := geminiTrustPath(env)
	obj, rules, raw, err := trustRules(path)
	if err != nil {
		return Applied{}, err
	}
	if trustLevel(rules, folder, env.goos()) != "" {
		return Applied{Path: filepath.ToSlash(path)}, nil
	}
	// Written the way gemini reads it back, which is also how gemini writes
	// its own entries on this machine: lowercased forward slashes.
	obj[normTrust(folder, env.goos())] = json.RawMessage(`"` + trustFolder + `"`)
	return writeJSONFile(path, obj, raw)
}

func geminiTrustApply(env Env, target string) (Applied, error) {
	return addTrust(env, target)
}

// geminiLaunch trusts the launch folder before gemini starts in it, when the
// folder is inside a workspace root and no rule already decides it. A rule that
// says no stays a no.
func geminiLaunch(env Env, cwd string) (string, error) {
	if geminiEnvOverride(env) != "" {
		return "", nil
	}
	if s, _, err := readGeminiSettings(env); err == nil && s.Security.FolderTrust.Enabled != nil &&
		!*s.Security.FolderTrust.Enabled {
		return "", nil
	}
	goos := env.goos()
	here := normTrust(cwd, goos)
	inside := false
	for _, root := range env.Roots {
		if isSubpath(normTrust(root, goos), here) {
			inside = true
			break
		}
	}
	if !inside {
		return "", nil
	}
	res, err := addTrust(env, cwd)
	if err != nil {
		return "", err
	}
	if !res.Changed {
		return "", nil
	}
	return "trusted " + filepath.ToSlash(cwd) + " for gemini in " + res.Path, nil
}

func geminiAuthCheck(env Env) Result {
	s, settingsPath, err := readGeminiSettings(env)
	r := Result{Path: filepath.ToSlash(settingsPath)}
	if err != nil {
		r.State = Warn
		r.Detail = "atrium cannot read gemini's settings.json (" + err.Error() + "), so it cannot tell " +
			"how gemini signs in."
		return r
	}
	signIn := "gemini"
	switch s.Security.Auth.SelectedType {
	case "":
		r.State, r.Fix, r.Command = Fail, FixExplain, signIn
		r.Detail = "gemini has no sign-in method chosen, so its first launch stops to ask. run gemini in " +
			"a terminal once and pick one, or type /auth inside it."
	case "gemini-api-key":
		return geminiKeyCheck(env, r)
	case "oauth-personal":
		if exists(filepath.Join(geminiDir(env), "oauth_creds.json")) {
			r.State, r.Detail = OK, "signed in with a Google account."
			return r
		}
		r.State, r.Fix, r.Command = Fail, FixExplain, signIn
		r.Detail = "gemini is set to sign in with Google and has no saved sign-in. run gemini in a " +
			"terminal and type /auth."
	default:
		r.State = NA
		r.Detail = "gemini signs in with " + s.Security.Auth.SelectedType + ", which atrium cannot check."
	}
	return r
}

// geminiKeyCheck looks for where an API key could come from, by name only.
// Atrium never reads the key's value into anything it keeps.
func geminiKeyCheck(env Env, r Result) Result {
	setKey := `setx GEMINI_API_KEY "<your key>"`
	if env.goos() != "windows" {
		setKey = `echo 'export GEMINI_API_KEY="<your key>"' >> ~/.profile`
	}
	for _, k := range []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"} {
		if strings.TrimSpace(env.RowEnv[k]) != "" {
			r.State, r.Fix, r.Command = Warn, FixExplain, setKey
			r.Detail = k + " is in this runner's env in atrium, and atrium does not hold credentials. " +
				"put it in your own environment or ~/.gemini/.env and remove it from the runner."
			return r
		}
	}
	if env.inherited("GEMINI_API_KEY") != "" {
		r.State, r.Detail = OK, "GEMINI_API_KEY is in the environment the runner starts with."
		return r
	}
	for _, f := range []string{filepath.Join(geminiDir(env), ".env"), filepath.Join(env.Home, ".env")} {
		if envFileNames(f, "GEMINI_API_KEY") {
			r.State, r.Detail = OK, "GEMINI_API_KEY is set in "+filepath.ToSlash(f)+"."
			if strings.HasSuffix(filepath.ToSlash(f), ".gemini/.env") {
				r.Detail += " gemini reads that file only in a folder it trusts."
			}
			return r
		}
	}
	if exists(filepath.Join(geminiDir(env), "gemini-credentials.json")) {
		r.State, r.Detail = OK, "gemini keeps a key in its encrypted credentials file."
		return r
	}
	r.State, r.Fix, r.Command = Warn, FixExplain, setKey
	r.Detail = "atrium sees no GEMINI_API_KEY. gemini may keep the key in the OS keychain, which atrium " +
		"does not read. if gemini asks for a key when it starts, set one, and restart the room so it " +
		"picks it up."
	if env.Prepare {
		r.Detail += " this runner's prepare command may also set it."
	}
	return r
}

// envFileNames reports whether a .env file assigns name. Only the name is
// looked at.
func envFileNames(path, name string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(sc.Text()), "export "))
		if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == name &&
			strings.Trim(strings.TrimSpace(v), `"'`) != "" {
			return true
		}
	}
	return false
}
