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
				return alreadySaid("pass exactly one of --plan or --apply")
			}
			if specPath == "" {
				return alreadySaid("--spec <file|-> is required")
			}
			var data []byte
			var err error
			if specPath == "-" {
				data, err = io.ReadAll(cmd.InOrStdin())
			} else {
				data, err = os.ReadFile(specPath)
			}
			if err != nil {
				return alreadySaid("read the spec: %v", err)
			}
			spec, err := roomspec.Parse(data)
			if err != nil {
				return alreadySaid("%v", err)
			}
			ad, err := roomspec.ForOS(spec.OS)
			if err != nil {
				return alreadySaid("%v", err)
			}
			db = orDefault(db, defaultRoomDB())
			fetch := setupFetcher(hubAddr, packDir)

			var lk roomspec.Lock
			if plan {
				v := roomspec.View{FS: roomspec.OSFS{}, Env: roomspec.OSEnv{}, Tools: execTools{}, Settings: roomSettings{db: db}, Need: panelAgents()}
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

type roomSettings struct{ db string }

func (r roomSettings) Get(key string) (string, error) {
	v, err := store.SettingOfFile(r.db, key)
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

// ── tools ────────────────────────────────────────────────────────────────────

type execTools struct{}

func (execTools) Query(argv []string) (string, bool) {
	if len(argv) == 0 {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).Output()
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
