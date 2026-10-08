package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/roomspec"
	"github.com/dovholuknf/atrium/internal/store"
)

// exitCodeError ends the command with a specific exit code. The command has already said why, so nothing more is printed.
type exitCodeError struct{ Code int }

func (e exitCodeError) Error() string { return fmt.Sprintf("exit %d", e.Code) }

// Is makes it an already-said failure too, so cobra and Execute print nothing more.
func (e exitCodeError) Is(target error) bool { return target == errAlreadySaid }

// setupRefused says why the arguments or the spec cannot be used, on stderr, and ends the command with exit 1. alreadySaid
// prints nothing itself, so without this a refused spec would exit 1 in silence.
func setupRefused(cmd *cobra.Command, format string, a ...any) error {
	fmt.Fprintf(cmd.ErrOrStderr(), "atrium room setup: %s\n", fmt.Sprintf(format, a...))
	return alreadySaid(format, a...)
}

// roomSetupCmd is `atrium room setup`: converge THIS machine to a room.yaml. It runs on the room, as the room's account.
func roomSetupCmd() *cobra.Command {
	var (
		specPath, db, hubAddr, packDir string
		plan, apply, asJSON            bool
	)
	c := &cobra.Command{
		Use:   "setup --spec <file|-> (--plan | --apply)",
		Short: "Make this machine match a room.yaml: the work root, tool caches, room settings, agent pack",
		Long: `Reads a room.yaml (v1) and either says what is different (--plan, which cannot write: it is given no way to) or
makes it so (--apply). Run it on the room, as the room's account. --apply writes the lock to ~/.atrium/room.lock.

Exit codes: 0 everything is as the spec says, 13 an administrator must run the lines printed (this never needs admin itself),
1 the arguments are wrong, 3 a step failed.`,
		Args: cobra.NoArgs,
		RunE: speaksForItself(func(cmd *cobra.Command, _ []string) error {
			if plan == apply {
				return setupRefused(cmd, "pass exactly one of --plan or --apply")
			}
			if specPath == "" {
				return setupRefused(cmd, "--spec <file|-> is required")
			}
			var data []byte
			var err error
			if specPath == "-" {
				data, err = io.ReadAll(cmd.InOrStdin())
			} else {
				data, err = os.ReadFile(specPath)
			}
			if err != nil {
				return setupRefused(cmd, "read the spec: %v", err)
			}
			spec, err := roomspec.Parse(data)
			if err != nil {
				return setupRefused(cmd, "%v", err)
			}
			ad, err := roomspec.ForOS(spec.OS)
			if err != nil {
				return setupRefused(cmd, "%v", err)
			}
			db = orDefault(db, defaultRoomDB())
			fetch := setupFetcher(hubAddr, packDir)

			var lk roomspec.Lock
			if plan {
				// The plan is handed the read-only faces only, so nothing it is given can write even by a type assertion.
				v := roomspec.View{FS: readOnlyFS{roomspec.OSFS{}}, Env: readOnlyEnv{roomspec.OSEnv{}}, Tools: execTools{},
					Settings: roomSettings{db: db, readOnly: true}, Need: panelAgents()}
				if fetch != nil {
					v.Latest = fetch.Latest
				}
				lk = roomspec.Plan(spec, ad, v)
			} else {
				lk = roomspec.Apply(spec, ad, roomspec.Host{FS: roomspec.OSFS{}, Env: roomspec.OSEnv{}, Tools: execTools{},
					Settings: roomSettings{db: db}, Fetch: fetch, Need: panelAgents()})
			}
			if err := printSetup(cmd.OutOrStdout(), lk, asJSON); err != nil {
				return err
			}
			if code := roomspec.Code(lk.Steps); code != 0 {
				return exitCodeError{Code: code}
			}
			return nil
		}),
	}
	c.Flags().StringVar(&specPath, "spec", "", "the room.yaml, a file or - for stdin")
	c.Flags().BoolVar(&plan, "plan", false, "say what --apply would do; writes nothing")
	c.Flags().BoolVar(&apply, "apply", false, "make it so, and write ~/.atrium/room.lock")
	c.Flags().BoolVar(&asJSON, "json", false, "print the lock as JSON")
	c.Flags().StringVar(&db, "db", "", "the room's database (default: the room's own)")
	c.Flags().StringVar(&hubAddr, "hub-addr", "", "host:port of the hub's git mirror, where the agent pack comes from")
	c.Flags().StringVar(&packDir, "pack-dir", "", "a local checkout to take the agent pack from instead of the hub (offline, tests)")
	return c
}

func printSetup(w io.Writer, lk roomspec.Lock, asJSON bool) error {
	if asJSON {
		b, err := roomspec.MarshalLock(lk)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	}
	for _, s := range lk.Steps {
		fmt.Fprintf(w, "setup %s %s %s\n", s.Step, s.Status, s.Detail)
	}
	if len(lk.AdminLines) > 0 {
		fmt.Fprintln(w, "an administrator runs:")
		for _, l := range lk.AdminLines {
			fmt.Fprintf(w, "    %s\n", l)
		}
	}
	return nil
}

// panelAgents are the agents the review panel names, which the pack should carry.
func panelAgents() []string {
	var panel []struct {
		Agent string `json:"agent"`
	}
	if json.Unmarshal([]byte(store.DefaultPRPanel), &panel) != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range panel {
		if p.Agent != "" && !seen[p.Agent] {
			seen[p.Agent] = true
			out = append(out, p.Agent)
		}
	}
	return out
}

// ── settings, through the store the way `room set` does ──────────────────────

// roomSettings reads and sets the room's settings. readOnly is a plan's: it reads without opening the store, so a plan does not
// migrate or otherwise touch the room's database.
type roomSettings struct {
	db       string
	readOnly bool
}

func (r roomSettings) Get(key string) (string, error) {
	read := store.SettingOfFile
	if r.readOnly {
		read = store.SettingOfFileReadOnly
	}
	v, err := read(r.db, key)
	switch {
	case errors.Is(err, store.ErrDatabaseInUse):
		return "", roomspec.ErrRoomRunning
	case errors.Is(err, os.ErrNotExist):
		return "", nil
	}
	return v, err
}

func (r roomSettings) Set(key, value string) error {
	_, err := storeRoomSetting(r.db, key, value)
	if err != nil && errors.Is(err, store.ErrDatabaseInUse) {
		return roomspec.ErrRoomRunning
	}
	return err
}

// readOnlyFS and readOnlyEnv hide everything but the read methods, so a plan holds no value it could type-assert into a writer.
type readOnlyFS struct{ roomspec.ReadFS }
type readOnlyEnv struct{ roomspec.ReadEnv }

// ── tools ────────────────────────────────────────────────────────────────────

type execTools struct{}

// Query asks a tool its own answer. It runs in an empty temp folder, so a go.mod or .npmrc of the folder it was typed in cannot
// change the answer, and with GOTOOLCHAIN=local, so asking `go env` can never download a toolchain.
func (execTools) Query(argv []string) (string, bool) {
	if len(argv) == 0 {
		return "", false
	}
	dir, err := os.MkdirTemp("", "atrium-ask-")
	if err != nil {
		return "", false
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// ── the pack source ──────────────────────────────────────────────────────────

// packFetcher gets a pack from the hub's git mirror, or from a local checkout.
type packFetcher struct {
	hubAddr, dir string
}

func setupFetcher(hubAddr, dir string) roomspec.Fetcher {
	if hubAddr == "" && dir == "" {
		return nil
	}
	return packFetcher{hubAddr: hubAddr, dir: dir}
}

func (f packFetcher) url(repo string) string {
	return "http://" + f.hubAddr + "/git/hub/github/" + repo + ".git"
}

// gitCmd runs git with prompts off for this child alone.
func gitCmd(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if dir == "" {
		cmd.Dir = os.TempDir() // never the folder the command was typed in: a repo there would be read as config
	}
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func (f packFetcher) Latest(repo, branch string) (string, error) {
	if f.hubAddr == "" {
		return "", errors.New("no hub address")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := gitCmd(ctx, "", "ls-remote", "--", f.url(repo), "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	if fields := strings.Fields(string(out)); len(fields) > 0 {
		return fields[0], nil
	}
	return "", fmt.Errorf("%s has no branch %s", repo, branch)
}

func (f packFetcher) Fetch(repo, branch, from string) (*roomspec.PackSource, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	root := f.dir
	if root == "" {
		tmp, err := os.MkdirTemp("", "atrium-pack-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(tmp)
		root = filepath.Join(tmp, "pack")
		if _, err := gitCmd(ctx, "", "clone", "--depth", "1", "--branch", branch, "--", f.url(repo), root); err != nil {
			return nil, err
		}
	}
	head, err := gitCmd(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	src := &roomspec.PackSource{Repo: repo, Branch: branch, Commit: strings.TrimSpace(string(head)), Files: map[string][]byte{}}
	base := filepath.Join(root, filepath.FromSlash(from))
	if err := walkPack(base, src); err != nil {
		return nil, err
	}
	if len(src.Files) == 0 {
		return nil, fmt.Errorf("%s/%s has no agents or skills in %q", repo, branch, from)
	}
	return src, nil
}

// walkPack reads <base>/agents/*.md and <base>/skills/<n>/** (n holding a SKILL.md). A link is never followed: it is named in
// Skipped, because a skill that is a link into another repository is not this pack's to give.
func walkPack(base string, src *roomspec.PackSource) error {
	if fi, err := os.Lstat(base); err != nil {
		return err
	} else if fi.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%s is a link, and a pack is not followed out of its repo", base)
	}
	agents, _ := os.ReadDir(filepath.Join(base, "agents"))
	for _, e := range agents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if e.Type()&fs.ModeSymlink != 0 {
			src.Skipped = append(src.Skipped, "agents/"+e.Name())
			continue
		}
		b, err := os.ReadFile(filepath.Join(base, "agents", e.Name()))
		if err != nil {
			return err
		}
		src.Files["agents/"+e.Name()] = b
	}
	skills, _ := os.ReadDir(filepath.Join(base, "skills"))
	for _, e := range skills {
		dir := filepath.Join(base, "skills", e.Name())
		if e.Type()&fs.ModeSymlink != 0 {
			src.Skipped = append(src.Skipped, "skills/"+e.Name())
			continue
		}
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
			continue
		}
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(base, p)
			rel = filepath.ToSlash(rel)
			if d.Type()&fs.ModeSymlink != 0 {
				src.Skipped = append(src.Skipped, rel)
				return nil
			}
			if d.IsDir() {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			src.Files[rel] = b
			return nil
		})
		if err != nil {
			return err
		}
	}
	sort.Strings(src.Skipped)
	return nil
}
