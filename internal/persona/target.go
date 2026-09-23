package persona

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// TargetMarkdown is the TARGET.md a persona run starts from: what to review,
// what to read, and the shape to answer in.
func TargetMarkdown(pack string, p Persona, runner string, t Target) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	key := t.RepoKey
	if key == "" {
		key = "(unknown: the worktree has no origin atrium could read)"
	}

	w("# Review target\n\n")
	w("atrium started this session as the **%s** persona (`%s`) on %s, to review one card's work.\n\n",
		p.Name, p.ID, runner)
	w("- worktree: `%s`\n", t.Worktree)
	w("- repo key: `%s`\n", key)
	w("- branch: `%s`\n", t.Branch)
	w("- default branch: `%s`\n", t.DefaultBranch)
	w("- merge base: `%s`\n", t.MergeBase)
	w("- diff range: `%s`\n\n", t.Range)
	w("The committed work to review:\n\n```\ngit -C \"%s\" --no-pager diff %s\n```\n\n", t.Worktree, t.Range)
	if t.Dirty {
		w("The worktree ALSO has uncommitted changes, which are part of the review:\n\n"+
			"```\ngit -C \"%s\" --no-pager diff HEAD\ngit -C \"%s\" status --porcelain\n```\n\n",
			t.Worktree, t.Worktree)
	} else {
		w("The worktree had no uncommitted changes when this run was made.\n\n")
	}
	w("Read whatever surrounding files or dependency source you need, not only the diff.\n\n")

	w("## Read from your persona folder, and only this\n\n")
	dir := filepath.ToSlash(p.dir)
	w("Your folder is `%s`. Knowledge is reviewed and wins a conflict. Memory is a hint to check, "+
		"not a rule.\n\n", dir)
	for _, k := range knowledgeFor(p.dir, t.RepoKey) {
		w("- knowledge: `%s`\n", k)
	}
	mem := memoryFor(p.dir, t.RepoKey)
	for _, m := range mem {
		w("- memory: `%s`\n", m)
	}
	if len(mem) == 0 {
		w("- memory: none applies to this repo\n")
	}
	w("\nDo not read knowledge or memory for other repos.\n\n")

	rej := rejectedFor(p.dir, t.RepoKey)
	w("## Findings you already got wrong here\n\n")
	if len(rej) == 0 {
		w("None recorded for this repo.\n\n")
	} else {
		w("Do not raise these again unless the code has changed so they are now real.\n\n")
		for _, l := range rej {
			w("%s\n", l)
		}
		w("\n")
	}

	w("## Rules\n\n")
	w("- Review only. Do not modify any file in the worktree.\n")
	w("- Do not build, compile, vet or run tests. Answer those questions by reading the code.\n")
	w("- To record a new lesson, write one memory file with a `repo:` line in its frontmatter "+
		"(`repo: %s`, or `repo: general`) and a one-line `Why:` in its body saying what taught it.\n",
		firstNonEmpty(t.RepoKey, "general"))
	w("- Do not edit your persona.md, knowledge, or anything else under `%s`.\n", filepath.ToSlash(pack))
	w("- When done, call `atrium_report` with status `done`, `no_commit` \"review only\", and the " +
		"json array below as the summary.\n\n")

	w("%s", findingSchema)
	return b.String()
}

// knowledgeFor is the knowledge files that exist for a repo key, general first.
func knowledgeFor(dir, key string) []string {
	var out []string
	for _, rel := range []string{KnowledgeFile("general"), KnowledgeFile(key)} {
		if key == "" && rel != KnowledgeFile("general") {
			continue
		}
		f := filepath.Join(dir, filepath.FromSlash(rel))
		if _, err := os.Stat(f); err == nil && !contains(out, filepath.ToSlash(f)) {
			out = append(out, filepath.ToSlash(f))
		}
	}
	return out
}

// memoryFor is the memory files that apply to a repo key: `repo: general`,
// `repo: <key>`, or no repo line at all, which is every lesson written before
// the lines existed.
func memoryFor(dir, key string) []string {
	entries, err := os.ReadDir(filepath.Join(dir, "memory"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || name == "MEMORY.md" || name == "rejected.md" {
			continue
		}
		f := filepath.Join(dir, "memory", name)
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		repo := parseLesson(string(raw)).Repo
		if repo == "" || repo == "general" || (key != "" && repo == key) {
			out = append(out, filepath.ToSlash(f))
		}
	}
	sort.Strings(out)
	return out
}

// rejectedFor is the lines of memory/rejected.md scoped to this repo or to
// general. A line for another repo does not apply.
func rejectedFor(dir, key string) []string {
	raw, err := os.ReadFile(filepath.Join(dir, "memory", "rejected.md"))
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "- ") {
			continue
		}
		m := rejectedRepo.FindStringSubmatch(l)
		if m != nil && (m[1] == "general" || (key != "" && m[1] == key)) {
			out = append(out, l)
		}
	}
	return out
}

// rejectedRepo reads the repo out of a rejected.md line, which the review
// panel writes as `- <date> repo: <key> | <claim> | wrong because <reason>`.
var rejectedRepo = regexp.MustCompile(`repo:\s*([^\s|]+)`)

func firstNonEmpty(a ...string) string {
	for _, s := range a {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// findingSchema is the review-panel skill's shared definitions, quoted so a
// run directory is self-contained. The skill is the source:
// dotfiles/claude/skills/review-panel/SKILL.md, "Shared definitions". When it
// changes, change this.
const findingSchema = "## Severity scale\n\n" +
	"- `blocking`: ship-stopper: data loss, crash/hang on a common path, security hole, or breaks the build.\n" +
	"- `high`: wrong behavior on a realistic path, or a security/correctness bug reachable behind a plausible condition.\n" +
	"- `medium`: bug on an edge case, or a real maintainability/fit problem that will bite later.\n" +
	"- `low`: minor correctness/style/fit issue, safe to defer.\n" +
	"- `nit`: cosmetic, no behavioral impact.\n\n" +
	"## Finding schema\n\n" +
	"Return your findings as a single fenced ```json block holding an array of objects with exactly these " +
	"fields (an empty array if you found nothing):\n\n" +
	"```json\n" +
	"[{\n" +
	"  \"severity\": \"blocking|high|medium|low|nit\",\n" +
	"  \"file\": \"path/relative/to/repo\",\n" +
	"  \"line\": 123,\n" +
	"  \"category\": \"correctness|security|fit|test|perf|style\",\n" +
	"  \"claim\": \"one-sentence statement of the problem\",\n" +
	"  \"evidence\": \"why it is real: the code path, with file:line refs that can be checked\",\n" +
	"  \"preexisting\": \"introduced|preexisting|worsened-by-pr\",\n" +
	"  \"prod_survival\": \"blocking/high only: why isn't this already broken in production?\",\n" +
	"  \"fix\": \"the concrete change that resolves it\",\n" +
	"  \"confidence\": \"high|medium|low\"\n" +
	"}]\n" +
	"```\n\n" +
	"- `evidence` is mandatory and cites real lines rather than restating the claim.\n" +
	"- A claim about how a dependency behaves quotes the exact line from that dependency's own source, " +
	"with its path, at the version the project pins. A dependency claim with no quoted line is dropped.\n" +
	"- `preexisting` says whether this change caused it. Only `introduced` and `worsened-by-pr` keep " +
	"their severity.\n" +
	"- `prod_survival` is required on every `blocking` and `high`. If it cannot be answered, lower the " +
	"finding or drop it.\n"
