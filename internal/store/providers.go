package store

import (
	"errors"
	"sort"
	"strings"
	"time"
)

// WHERE A REPOSITORY LIVES ON THIS MACHINE.
//
// A provider is the operator saying "under D:/git/github, directories are
// org/repo". Atrium reads that instead of walking the disk guessing at a
// convention, which is what the mechanism this replaces did.
//
// ATRIUM LEARNS NOTHING ABOUT ANY FORGE, and that is the rule a recogniser and
// a source already keep. A provider is a root and a layout. It does no URL
// parsing, no cloning, no network call, and holds no credential. It answers one
// question, "where does org/repo live", out of what somebody typed.
//
// The word `scm` is deliberately not used. `docs/scm-design.md` already owns it
// for two other features, both built, and three things under one noun is how a
// package grows a file nobody can describe. See `docs/providers-design.md`.

// ProviderKinds is every type a provider may be.
//
// A SLICE IN GO RATHER THAN A CHECK IN SQL. The requirement says the type is a
// field and not an assumption, and a CHECK constraint would make the second
// type a table-rebuild migration, because SQLite cannot alter one in place.
// Here it is one line.
var ProviderKinds = []string{"git"}

// Provider is one named root and what is true about it.
type Provider struct {
	// Name is as the operator typed it, and it is the key. Renaming is a
	// delete plus an add, because everything refers to a provider by this
	// string and a cascade is a separate question.
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Root is the directory holding orgs. `D:/git/github`.
	Root string `json:"root"`
	// Worktrees is the toggle, and WorktreeRoot is only meaningful while it is
	// on. Turning it off is refused while that directory holds anything: see
	// `worktreeRootBlockers` in internal/api.
	Worktrees    bool   `json:"worktrees"`
	WorktreeRoot string `json:"worktree_root"`
	// Host is optional and is the join to a recogniser. `github.com`. With it,
	// an org and a repo turn back into a URL, and a recogniser's cwd can one
	// day be a provider reference rather than an absolute path. Empty means
	// this provider is local only, which is a real arrangement.
	Host string `json:"host"`
	// Enabled off keeps every row and contributes nothing. The alternative for
	// a drive that is not plugged in would be deleting the provider, which
	// throws away every typed repository and every hidden decision.
	Enabled bool `json:"enabled"`
	// Exclude is one glob per line, matched against the path relative to the
	// root. "Never adopt these", where `hidden` on a repository is "I adopted
	// this and I do not want it".
	Exclude string `json:"exclude"`
	// MaxRepos bounds adoption for this provider. 0 means the built-in
	// default. Per provider because the failure is per root: one holding five
	// repositories beside one holding four hundred is the ordinary case.
	MaxRepos int `json:"max_repos"`

	// OBSERVED, NEVER TYPED. The rule the cards and the sources already
	// follow: what a machine reports never overwrites what a human wrote, and
	// it never arrives through a save. `RecordProviderScan` is the only writer.
	LastError  string     `json:"last_error"`
	LastScan   string     `json:"last_scan"`
	LastScanAt *time.Time `json:"last_scan_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ProviderRepo is one repository under a provider.
//
// THE ROW IS DURABLE AND THE PRESENCE IS DERIVED, which is the whole answer to
// "stored or computed". The backlog states it as either/or and names the cost
// of each side: storing means a repository deleted from disk lingers, deriving
// means the org and repo somebody typed are not authoritative. Neither cost has
// to be paid, because they attach to different fields.
//
// So the row survives, including for a repository nobody has cloned yet, and
// `Path` and `Present` are worked out on every read. A repository deleted from
// disk does not linger as a lie. It lingers as a row that says "not on disk",
// which is a different and more useful thing.
//
// The case this protects: a root on a drive that is not plugged in. If presence
// were stored, one discovery run against the dead drive would mark everything
// absent and a tidy-up would delete it. If rows were derived, the list would be
// empty and every typed entry gone. As written, the list is every repository
// ever adopted, each drawn as missing, and plugging the drive back in restores
// the picture with no action at all.
type ProviderRepo struct {
	Provider string `json:"provider"`
	Org      string `json:"org"`
	Repo     string `json:"repo"`
	// Hidden is a durable no. Without it there is nothing to stop the next
	// discovery re-adopting something deliberately dismissed.
	Hidden    bool      `json:"hidden"`
	AdoptedAt time.Time `json:"adopted_at"`
	Notes     string    `json:"notes"`

	// Derived on read, never stored and never written back.
	Path    string `json:"path"`
	Present bool   `json:"present"`
}

// providerKey folds a name so `GitHub` and `github` are one provider.
func providerKey(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// ProviderPath is where a repository lives, and it is the one place that
// decides.
//
// `root/org/repo`, with an empty org meaning `root/repo`. A repository sitting
// directly under the root with no org above it is a real arrangement on a real
// machine, so the three-part rule is a default rather than a law.
func ProviderPath(root, org, repo string) string {
	root = slashed(root)
	org = strings.Trim(strings.TrimSpace(org), "/")
	repo = strings.Trim(strings.TrimSpace(repo), "/")
	if root == "" || repo == "" {
		return ""
	}
	if org == "" {
		return root + "/" + repo
	}
	return root + "/" + org + "/" + repo
}

const providerColumns = `name, kind, root, worktrees, worktree_root, host, enabled,
	exclude, max_repos, last_error, last_scan, last_scan_at, created_at, updated_at`

func scanProvider(sc interface{ Scan(...any) error }) (*Provider, error) {
	var (
		p                  Provider
		worktrees, enabled int
		scanAt             string
		created, updated   string
	)
	if err := sc.Scan(&p.Name, &p.Kind, &p.Root, &worktrees, &p.WorktreeRoot, &p.Host,
		&enabled, &p.Exclude, &p.MaxRepos, &p.LastError, &p.LastScan, &scanAt,
		&created, &updated); err != nil {
		return nil, err
	}
	p.Worktrees = worktrees != 0
	p.Enabled = enabled != 0
	if scanAt != "" {
		t, err := parseTS(scanAt)
		if err != nil {
			return nil, err
		}
		p.LastScanAt = &t
	}
	var err error
	if p.CreatedAt, err = parseTS(created); err != nil {
		return nil, err
	}
	if p.UpdatedAt, err = parseTS(updated); err != nil {
		return nil, err
	}
	return &p, nil
}

// Providers lists every row, by name.
func (st *Store) Providers() ([]*Provider, error) {
	var out []*Provider
	err := st.guard(func() error {
		out = nil
		rows, err := st.db.Query(`SELECT ` + providerColumns +
			` FROM provider ORDER BY name_key ASC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			p, err := scanProvider(rows)
			if err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// ErrNoProvider is what a name nothing answers to comes back as.
var ErrNoProvider = errors.New("no provider by that name")

// Provider returns one row, found by folded name, so a URL does not have to
// match the operator's capitals.
func (st *Store) Provider(name string) (*Provider, error) {
	var p *Provider
	err := st.guard(func() error {
		row := st.db.QueryRow(`SELECT `+providerColumns+` FROM provider WHERE name_key = ?`,
			providerKey(name))
		got, err := scanProvider(row)
		if err != nil {
			return err
		}
		p = got
		return nil
	})
	return p, err
}

// SaveProvider creates or replaces a row.
//
// THE OBSERVED FIELDS ARE NOT WRITABLE. `last_error`, `last_scan` and
// `last_scan_at` are what atrium saw, so a save leaves whatever is there rather
// than taking it from the body. Same posture a source and a recogniser keep.
//
// What is refused here is what is true of the row on its own. Whether a root
// overlaps another provider's, and whether a worktree directory is empty, are
// questions about the FILESYSTEM, and they are answered in internal/api, which
// is where atrium reads disks.
func (st *Store) SaveProvider(p Provider) (*Provider, error) {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return nil, errors.New("a provider needs a name. it is the name you will refer to it by")
	}
	if strings.ContainsAny(p.Name, "/\\") {
		return nil, errors.New("a provider name cannot contain a slash")
	}
	p.Kind = strings.TrimSpace(p.Kind)
	if p.Kind == "" {
		p.Kind = "git"
	}
	if !knownKind(p.Kind) {
		return nil, errors.New("there is no provider type called " + p.Kind +
			". the only one so far is git")
	}
	p.Root = slashed(p.Root)
	p.WorktreeRoot = slashed(p.WorktreeRoot)
	if p.Root == "" {
		return nil, errors.New(
			"a provider needs a root folder: the directory its repositories live under")
	}
	// A TOGGLE THAT IS ON NEEDS SOMEWHERE TO PUT THEM. Refused here rather than
	// discovered when somebody presses make, which is well past the point where
	// anything can be done about it.
	if p.Worktrees && p.WorktreeRoot == "" {
		return nil, errors.New("worktree support needs a folder to put worktrees in")
	}
	if p.MaxRepos < 0 {
		p.MaxRepos = 0
	}
	p.Host = strings.TrimSpace(p.Host)

	err := st.guard(func() error {
		stamp := ts(now())
		created := stamp
		row := st.db.QueryRow(`SELECT created_at FROM provider WHERE name_key = ?`,
			providerKey(p.Name))
		var had string
		if err := row.Scan(&had); err == nil {
			created = had
		}
		worktrees, enabled := 0, 0
		if p.Worktrees {
			worktrees = 1
		}
		if p.Enabled {
			enabled = 1
		}
		// CONFLICT ON `name_key`, NOT ON `name`, and the difference is the
		// whole point of having the folded column. Saving `GitHub` over an
		// existing `github` has to be an update of that provider, and a
		// conflict target of `name` would miss it and fail on the unique index
		// instead. `name` is left alone by the update for the same reason: the
		// first spelling is the key everything else already refers to.
		_, err := st.db.Exec(`INSERT INTO provider
			(name, name_key, kind, root, worktrees, worktree_root, host, enabled,
			 exclude, max_repos, last_error, last_scan, last_scan_at, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,'','','',?,?)
			ON CONFLICT(name_key) DO UPDATE SET
				kind = excluded.kind, root = excluded.root,
				worktrees = excluded.worktrees, worktree_root = excluded.worktree_root,
				host = excluded.host, enabled = excluded.enabled,
				exclude = excluded.exclude, max_repos = excluded.max_repos,
				updated_at = excluded.updated_at`,
			p.Name, providerKey(p.Name), p.Kind, p.Root, worktrees, p.WorktreeRoot,
			p.Host, enabled, p.Exclude, p.MaxRepos, created, stamp)
		return err
	})
	if err != nil {
		return nil, err
	}
	return st.Provider(p.Name)
}

// DeleteProvider removes a provider and its repository rows.
//
// AND TOUCHES NO CARD. A card's directory is a string a human typed, and it
// outlives every configuration row that happens to describe it. See CLAUDE.md:
// a card outlives the process it describes.
func (st *Store) DeleteProvider(name string) error {
	return st.guard(func() error {
		key := providerKey(name)
		// Written out rather than left to ON DELETE CASCADE. SQLite enforces a
		// foreign key only while the `foreign_keys` pragma is on, and a feature
		// that quietly depends on a pragma is one that leaks rows the day
		// somebody opens the database with a different tool.
		if _, err := st.db.Exec(`DELETE FROM provider_repo
			WHERE provider IN (SELECT name FROM provider WHERE name_key = ?)`, key); err != nil {
			return err
		}
		_, err := st.db.Exec(`DELETE FROM provider WHERE name_key = ?`, key)
		return err
	})
}

// RecordProviderScan writes what a discovery run saw.
//
// Separate from `SaveProvider` deliberately: this is the observed half, and
// keeping it on its own call is what stops a form post from being able to claim
// a scan that never happened.
func (st *Store) RecordProviderScan(name, summary, failed string) error {
	return st.guard(func() error {
		_, err := st.db.Exec(
			`UPDATE provider SET last_scan = ?, last_error = ?, last_scan_at = ?
			   WHERE name_key = ?`,
			summary, failed, ts(now()), providerKey(name))
		return err
	})
}

const providerRepoColumns = `provider, org, repo, hidden, adopted_at, notes`

func scanProviderRepo(sc interface{ Scan(...any) error }) (*ProviderRepo, error) {
	var (
		r       ProviderRepo
		hidden  int
		adopted string
	)
	if err := sc.Scan(&r.Provider, &r.Org, &r.Repo, &hidden, &adopted, &r.Notes); err != nil {
		return nil, err
	}
	r.Hidden = hidden != 0
	var err error
	if r.AdoptedAt, err = parseTS(adopted); err != nil {
		return nil, err
	}
	return &r, nil
}

// ProviderRepos lists what is known under one provider.
//
// `Path` is filled in and `Present` is NOT. Whether a directory is there is a
// filesystem question and this package answers none of those. See
// `fillPresence` in internal/api.
func (st *Store) ProviderRepos(name string) ([]*ProviderRepo, error) {
	p, err := st.Provider(name)
	if err != nil {
		return nil, err
	}
	var out []*ProviderRepo
	err = st.guard(func() error {
		out = nil
		rows, err := st.db.Query(`SELECT `+providerRepoColumns+
			` FROM provider_repo WHERE provider = ? ORDER BY org_key ASC, repo_key ASC`, p.Name)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanProviderRepo(rows)
			if err != nil {
				return err
			}
			r.Path = ProviderPath(p.Root, r.Org, r.Repo)
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// SaveProviderRepo creates or updates one repository row.
//
// An org and a repo that name no row are allowed, and typing them is how a
// repository is added. That is what "a git provider expects an org and a repo"
// means: adding one is typing two fields rather than browsing to a directory.
// Atrium does not clone, so a row for a directory that is not there yet is a
// legitimate thing to hold.
func (st *Store) SaveProviderRepo(r ProviderRepo) (*ProviderRepo, error) {
	p, err := st.Provider(r.Provider)
	if err != nil {
		return nil, err
	}
	r.Provider = p.Name
	r.Org = strings.Trim(strings.TrimSpace(r.Org), "/")
	r.Repo = strings.Trim(strings.TrimSpace(r.Repo), "/")
	if r.Repo == "" {
		return nil, errors.New("a repository needs a name")
	}
	if strings.ContainsAny(r.Repo, "/\\") || strings.ContainsAny(r.Org, "/\\") {
		return nil, errors.New("an org and a repo are one path segment each, with no slashes")
	}
	err = st.guard(func() error {
		adopted := ts(now())
		row := st.db.QueryRow(`SELECT adopted_at FROM provider_repo
			WHERE provider = ? AND org_key = ? AND repo_key = ?`,
			r.Provider, providerKey(r.Org), providerKey(r.Repo))
		var had string
		if err := row.Scan(&had); err == nil {
			adopted = had
		}
		hidden := 0
		if r.Hidden {
			hidden = 1
		}
		_, err := st.db.Exec(`INSERT INTO provider_repo
			(provider, org, repo, org_key, repo_key, hidden, adopted_at, notes)
			VALUES (?,?,?,?,?,?,?,?)
			ON CONFLICT(provider, org_key, repo_key) DO UPDATE SET
				org = excluded.org, repo = excluded.repo,
				hidden = excluded.hidden, notes = excluded.notes`,
			r.Provider, r.Org, r.Repo, providerKey(r.Org), providerKey(r.Repo),
			hidden, adopted, r.Notes)
		return err
	})
	if err != nil {
		return nil, err
	}
	r.AdoptedAt = now()
	r.Path = ProviderPath(p.Root, r.Org, r.Repo)
	return &r, nil
}

// HideProviderRepo sets or clears the durable no.
func (st *Store) HideProviderRepo(provider, org, repo string, hidden bool) error {
	return st.guard(func() error {
		n := 0
		if hidden {
			n = 1
		}
		_, err := st.db.Exec(`UPDATE provider_repo SET hidden = ?
			WHERE provider IN (SELECT name FROM provider WHERE name_key = ?)
			  AND org_key = ? AND repo_key = ?`,
			n, providerKey(provider), providerKey(org), providerKey(repo))
		return err
	})
}

// ForgetProviderRepo removes a row for good.
//
// THE ONLY THING THAT EVER DELETES ONE. Discovery never does, which is what
// makes it safe to run against a root that is temporarily not there.
func (st *Store) ForgetProviderRepo(provider, org, repo string) error {
	return st.guard(func() error {
		_, err := st.db.Exec(`DELETE FROM provider_repo
			WHERE provider IN (SELECT name FROM provider WHERE name_key = ?)
			  AND org_key = ? AND repo_key = ?`,
			providerKey(provider), providerKey(org), providerKey(repo))
		return err
	})
}

// AdoptProviderRepos writes what a discovery run found, and answers how many
// rows were new.
//
// DISCOVERY ONLY EVER ADDS. It never deletes a row, never un-hides one, and
// running it twice adopts nothing the second time. That is what makes `hidden`
// a durable no rather than a suggestion the next scan overrules, and it is what
// makes a run against an unplugged drive harmless.
func (st *Store) AdoptProviderRepos(name string, found []ProviderRepo) (int, error) {
	p, err := st.Provider(name)
	if err != nil {
		return 0, err
	}
	added := 0
	err = st.guard(func() error {
		added = 0
		adopted := ts(now())
		for _, r := range found {
			org, repo := strings.TrimSpace(r.Org), strings.TrimSpace(r.Repo)
			if repo == "" {
				continue
			}
			res, err := st.db.Exec(`INSERT INTO provider_repo
				(provider, org, repo, org_key, repo_key, hidden, adopted_at, notes)
				VALUES (?,?,?,?,?,0,?,'')
				ON CONFLICT(provider, org_key, repo_key) DO NOTHING`,
				p.Name, org, repo, providerKey(org), providerKey(repo), adopted)
			if err != nil {
				return err
			}
			if n, err := res.RowsAffected(); err == nil && n > 0 {
				added++
			}
		}
		return nil
	})
	return added, err
}

// ProviderOrgs is the distinct orgs under a provider, for the picker's
// datalist.
//
// Derived rather than stored: an org is a directory that happens to hold
// repositories, and a row for one would mean deciding what an empty org folder
// is.
func (st *Store) ProviderOrgs(name string) ([]string, error) {
	repos, err := st.ProviderRepos(name)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, r := range repos {
		if r.Org == "" || seen[providerKey(r.Org)] {
			continue
		}
		seen[providerKey(r.Org)] = true
		out = append(out, r.Org)
	}
	sort.Slice(out, func(i, j int) bool { return providerKey(out[i]) < providerKey(out[j]) })
	return out, nil
}

func knownKind(k string) bool {
	for _, v := range ProviderKinds {
		if v == k {
			return true
		}
	}
	return false
}

// slashed is the house path form: forward slashes, no trailing one.
//
// Every path in atrium's code is forward slashed. A root typed as
// `D:\git\github` has to compare equal to one read back off the filesystem, or
// containment checks quietly stop matching.
func slashed(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\\", "/")
	for len(s) > 1 && strings.HasSuffix(s, "/") && !strings.HasSuffix(s, ":/") {
		s = strings.TrimSuffix(s, "/")
	}
	return s
}
