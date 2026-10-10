//go:build ignore

// Rewrites a branch's whole history so no commit message names the agent that wrote it, and every commit is
// authored and committed by one identity. Trees, parents' shape and both dates are kept: only messages, names and
// emails change, and so every sha.
//
//	go run scripts/scrub-history.go -from main -to main-scrubbed [-sign] [-map build.claude/scrub-map.tsv]
//	go run scripts/scrub-history.go -filter < message    # one message, the same rules, for land-claude-main.ps1
//
// It reads every commit through one `git cat-file --batch`, then writes each with one `git commit-tree`, oldest
// first, so a parent is always written before its child. -sign passes -S, which signs with user.signingkey.
//
// Before it says ok it checks the result: the same number of commits, the same tree at every commit, and no message
// matching a banned word. -map writes old sha, new sha per line, for anything that recorded an old sha.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var (
	from    = flag.String("from", "main", "the branch to rewrite")
	to      = flag.String("to", "main-scrubbed", "the branch to write, which must not exist")
	sign    = flag.Bool("sign", false, "sign every commit with user.signingkey")
	name    = flag.String("name", "", "author and committer name (default: git config user.name)")
	email   = flag.String("email", "", "author and committer email (default: git config user.email)")
	mapFile = flag.String("map", "", "write old sha, new sha here")
	seed    = flag.String("seed", "", "a -map from an earlier run: its commits are already rewritten, so only what -from adds is")
	filter  = flag.Bool("filter", false, "scrub one message from stdin to stdout and exit, as a filter-branch --msg-filter")
)

// rules run in order. A branch prefix goes first, so what remains is a word in a sentence.
var rules = []struct {
	re   *regexp.Regexp
	with string
}{
	{regexp.MustCompile(`\b([A-Za-z0-9_-]+)/claude/`), "$1/"},
	{regexp.MustCompile(`claude/main`), "main"},
	{regexp.MustCompile(`claude/`), ""},
	{regexp.MustCompile(`CLAUDE\.md`), "AGENTS.md"},
	{regexp.MustCompile(`build\.claude`), "the build directory"},
	{regexp.MustCompile(`CLAUDEVM`), "WORKVM"},
	{regexp.MustCompile(`(?i)claudevm`), "workvm"},
	{regexp.MustCompile(`claudeAuthCheck`), "authCheck"},
	{regexp.MustCompile(`(?i)claude-code`), "agent-cli"},
	{regexp.MustCompile(`(?i)Claude Code`), "the agent CLI"},
	{regexp.MustCompile(`claude-landing`), "landing"},
	{regexp.MustCompile(`claudeconf`), "agentconf"},
	{regexp.MustCompile(`CLAUDE`), "AGENT"},
	{regexp.MustCompile(`Claude`), "Agent"},
	{regexp.MustCompile(`(?i)claude`), "agent"},
}

// dropLine is a trailer naming a co-author or the vendor. The whole line goes.
var dropLine = regexp.MustCompile(`(?i)^\s*co.authored.by:|anthropic`)

// banned is checked in every message afterwards.
var banned = regexp.MustCompile(`(?i)claude|anthropic|co.authored.by`)

func scrub(msg string) string {
	var keep []string
	for _, l := range strings.Split(msg, "\n") {
		if dropLine.MatchString(l) {
			continue
		}
		for _, r := range rules {
			l = r.re.ReplaceAllString(l, r.with)
		}
		keep = append(keep, l)
	}
	return strings.TrimRight(strings.Join(keep, "\n"), "\n \t") + "\n"
}

type commit struct {
	sha, tree           string
	parents             []string
	authorDate, comDate string // "<unix> <tz>"
	msg                 string
}

func main() {
	flag.Parse()
	if *filter {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			die(err.Error())
		}
		os.Stdout.WriteString(scrub(string(b)))
		return
	}
	if *name == "" {
		*name = gitOut("config", "user.name")
	}
	if *email == "" {
		*email = gitOut("config", "user.email")
	}
	if *name == "" || *email == "" {
		die("no name or email: set git config user.name and user.email, or pass -name and -email")
	}
	if exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/heads/"+*to).Run() == nil {
		die("branch " + *to + " already exists")
	}

	// A seeded commit is already rewritten, so rev-list leaves it out: each one goes in as ^sha on stdin.
	newSHA := map[string]string{}
	var exclude strings.Builder
	if *seed != "" {
		b, err := os.ReadFile(*seed)
		if err != nil {
			die(err.Error())
		}
		for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if f := strings.Fields(l); len(f) == 2 {
				newSHA[f[0]] = f[1]
				exclude.WriteString("^" + f[0] + "\n")
			}
		}
	}
	cmd := exec.Command("git", "rev-list", "--reverse", "--topo-order", "--stdin", *from)
	cmd.Stdin = strings.NewReader(exclude.String())
	out, err := cmd.Output()
	if err != nil {
		die("rev-list: " + err.Error())
	}
	shas := strings.Fields(string(out))
	fmt.Printf("%d commits on %s, as %s <%s>, signed: %v\n", len(shas), *from, *name, *email, *sign)
	if len(shas) == 0 {
		die("nothing to rewrite")
	}
	commits := readAll(shas)

	start := time.Now()
	for i, c := range commits {
		args := []string{"commit-tree", c.tree}
		if *sign {
			args = append(args, "-S")
		}
		for _, p := range c.parents {
			np, ok := newSHA[p]
			if !ok {
				die("parent " + p + " of " + c.sha + " was not written first")
			}
			args = append(args, "-p", np)
		}
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME="+*name, "GIT_AUTHOR_EMAIL="+*email, "GIT_AUTHOR_DATE="+c.authorDate,
			"GIT_COMMITTER_NAME="+*name, "GIT_COMMITTER_EMAIL="+*email, "GIT_COMMITTER_DATE="+c.comDate)
		cmd.Stdin = strings.NewReader(scrub(c.msg))
		var errb bytes.Buffer
		cmd.Stderr = &errb
		out, err := cmd.Output()
		if err != nil {
			die(fmt.Sprintf("commit-tree for %s: %v\n%s", c.sha, err, errb.String()))
		}
		newSHA[c.sha] = strings.TrimSpace(string(out))
		if (i+1)%250 == 0 {
			el := time.Since(start)
			left := time.Duration(float64(el) / float64(i+1) * float64(len(commits)-i-1))
			fmt.Printf("  %d/%d, %s left\n", i+1, len(commits), left.Round(time.Second))
		}
	}
	tip := newSHA[commits[len(commits)-1].sha]
	git("branch", *to, tip)

	if *mapFile != "" {
		var b strings.Builder
		for _, c := range commits {
			fmt.Fprintf(&b, "%s\t%s\n", c.sha, newSHA[c.sha])
		}
		if err := os.WriteFile(*mapFile, []byte(b.String()), 0o644); err != nil {
			die(err.Error())
		}
	}
	verify(commits, newSHA)
	fmt.Printf("ok: %s is %s, %d commits, written in %s\n", *to, tip[:10], len(commits), time.Since(start).Round(time.Second))
}

// verify reads the new history back and checks it against the old.
func verify(old []commit, newSHA map[string]string) {
	var shas []string
	for _, c := range old {
		shas = append(shas, newSHA[c.sha])
	}
	got := readAll(shas)
	// Two commits that differed only in who made them become the same commit, and a merge of the two keeps one
	// parent. So the new history has one commit per distinct new sha, not one per old commit.
	distinct := map[string]bool{}
	for _, s := range newSHA {
		distinct[s] = true
	}
	n := len(strings.Fields(gitOut("rev-list", *to)))
	if n != len(distinct) {
		die(fmt.Sprintf("%s has %d commits, want %d (%d on %s, %d made identical)", *to, n, len(distinct), len(old), *from, len(old)-len(distinct)))
	}
	// Seeded commits are in newSHA too, so count collapses among this run's commits only.
	rewritten := map[string]bool{}
	for _, c := range old {
		rewritten[newSHA[c.sha]] = true
	}
	if len(old) != len(rewritten) {
		fmt.Printf("  %d commits on %s were identical to another once rewritten, and are one commit now\n", len(old)-len(rewritten), *from)
	}
	bad := 0
	for i, c := range got {
		o := old[i]
		switch {
		case c.tree != o.tree:
			fmt.Printf("  tree differs: %s -> %s\n", o.sha[:10], c.sha[:10])
			bad++
		case c.authorDate != o.authorDate || c.comDate != o.comDate:
			fmt.Printf("  date differs: %s -> %s\n", o.sha[:10], c.sha[:10])
			bad++
		case len(c.parents) != distinctParents(o.parents, newSHA):
			fmt.Printf("  parents differ: %s -> %s\n", o.sha[:10], c.sha[:10])
			bad++
		case banned.MatchString(c.msg):
			fmt.Printf("  banned word left: %s -> %s: %q\n", o.sha[:10], c.sha[:10], banned.FindString(c.msg))
			bad++
		}
	}
	if bad > 0 {
		die(fmt.Sprintf("%d commits failed the check. %s is left for you to look at: git branch -D %s", bad, *to, *to))
	}
}

func distinctParents(ps []string, newSHA map[string]string) int {
	seen := map[string]bool{}
	for _, p := range ps {
		seen[newSHA[p]] = true
	}
	return len(seen)
}

// readAll reads each commit through one cat-file process, in the order given.
func readAll(shas []string) []commit {
	cmd := exec.Command("git", "cat-file", "--batch")
	in, _ := cmd.StdinPipe()
	outp, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		die(err.Error())
	}
	go func() {
		w := bufio.NewWriter(in)
		for _, s := range shas {
			w.WriteString(s + "\n")
		}
		w.Flush()
		in.Close()
	}()
	r := bufio.NewReaderSize(outp, 1<<20)
	out := make([]commit, 0, len(shas))
	for range shas {
		head, err := r.ReadString('\n')
		if err != nil {
			die("cat-file: " + err.Error())
		}
		f := strings.Fields(head)
		if len(f) != 3 || f[1] != "commit" {
			die("cat-file: not a commit: " + head)
		}
		var size int
		fmt.Sscan(f[2], &size)
		body := make([]byte, size+1)
		if _, err := io.ReadFull(r, body); err != nil {
			die("cat-file: " + err.Error())
		}
		out = append(out, parse(f[0], string(body[:size])))
	}
	cmd.Wait()
	return out
}

func parse(sha, raw string) commit {
	c := commit{sha: sha}
	hdr, msg, _ := strings.Cut(raw, "\n\n")
	c.msg = msg
	inSig := false
	for _, l := range strings.Split(hdr, "\n") {
		if inSig {
			if strings.HasPrefix(l, " ") {
				continue
			}
			inSig = false
		}
		k, v, _ := strings.Cut(l, " ")
		switch k {
		case "tree":
			c.tree = v
		case "parent":
			c.parents = append(c.parents, v)
		case "author":
			c.authorDate = date(v)
		case "committer":
			c.comDate = date(v)
		case "gpgsig", "gpgsig-sha256":
			inSig = true
		case "encoding", "mergetag":
			die(sha + " has a " + k + " header, which this tool does not carry over")
		}
	}
	return c
}

// date is the "<unix> <tz>" at the end of an author or committer line.
func date(v string) string {
	f := strings.Fields(v)
	if len(f) < 2 {
		die("bad identity line: " + v)
	}
	return f[len(f)-2] + " " + f[len(f)-1]
}

func git(args ...string) {
	cmd := exec.Command("git", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		die(fmt.Sprintf("git %v: %v", args, err))
	}
}

func gitOut(args ...string) string {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func die(why string) {
	fmt.Fprintln(os.Stderr, "STOP:", why)
	os.Exit(1)
}
