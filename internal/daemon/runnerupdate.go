package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/runnersetup"
	"github.com/dovholuknf/atrium/internal/store"
)

// Is there a newer runner, asked at the moment it matters and never by running
// the runner.
//
// This replaces `scripts/sources/runner-updates.ps1`, which was a source on a
// ten minute timer. Three things were wrong with it and only one was the
// timer.
//
//  1. IT RAN THE RUNNER TO ASK ITS VERSION. `claude --version` and
//     `codex --version` are whole agent binaries starting up to print one
//     line, and on Windows each one allocated a console window: `hideWindow`
//     covers the command atrium spawns and not its grandchildren, so the
//     source's `pwsh` was quiet and the runners it shelled out to were not.
//     Now the version is read out of the installed package's own metadata,
//     which is a file, and the published version comes from one HTTP request.
//     No process is started at all.
//
//  2. IT ASKED WHEN NOBODY WAS LISTENING. Every ten minutes forever, including
//     the twenty three hours a day the operator is not about to start a
//     runner. The answer is only actionable at one moment, which is the moment
//     before a runner starts, so that is when it is asked.
//
//  3. ITS DEDUPLICATION KEY WAS THE VERSION, so each release raised a NEW
//     card and the previous one stayed in the inbox. Two releases nobody
//     actioned meant two identical-looking rows. The key is the package now,
//     and a newer version rewrites the card that is already there. See
//     `store.Offer` and `store.Refresh`.
//
// WHAT ATRIUM LEARNED, AND WHAT IT DID NOT. The rule the source header kept is
// kept here: atrium drives claude, codex, ollama and bare shells as peers and
// knows nothing about any of them. What it knows is that a harness row has a
// `package` field, and that the field names something the npm registry can be
// asked about. A row with an empty one is never asked, which is the right
// answer for a bare shell and for anything a platform installer put there.

// updateRegistry is where a published version is looked up.
//
// A variable so a test can point it somewhere that answers. Nothing else
// writes it.
var updateRegistry = "https://registry.npmjs.org"

// updateFresh is how long a published version is believed without asking
// again.
//
// Long, on purpose. The cost of a stale answer is being told about a release
// an hour late, and the cost of a short one is a network request in front of
// every launch. The first is not worth paying for.
const updateFresh = 6 * time.Hour

// updateBudget is the longest a launch waits for an answer it does not have.
//
// This is the one place atrium blocks a thing the operator asked for on
// something outside the machine, so it is bounded and the bound is short. A
// registry that has not answered in this long has not earned the delay, and
// the launch goes ahead knowing nothing, which is what happened before this
// existed.
const updateBudget = 4 * time.Second

// updateSetting is where the last answer per package is kept.
const updateSetting = "runner_latest"

// updateCheck is one package's published version and when that was learned.
type updateCheck struct {
	Latest string `json:"latest"`
	At     string `json:"at"`
}

// updates holds the checks in flight, so a wave of launches produces one
// request rather than one each.
type updates struct {
	mu     sync.Mutex
	flight map[string]chan struct{}
}

func (u *updates) init() {
	if u.flight == nil {
		u.flight = map[string]chan struct{}{}
	}
}

// claim reports whether this caller is the one that has to go and ask.
//
// The second return is the channel to wait on when it is not, which the caller
// is free to ignore: the rule is ONLY THE FIRST CALLER BLOCKS. A second launch
// arriving while a check is in flight goes ahead on whatever is cached rather
// than queueing behind somebody else's network request, because two people
// waiting on one answer is two launches delayed to tell one of them something.
func (u *updates) claim(pkg string) (bool, chan struct{}) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.init()
	if ch, ok := u.flight[pkg]; ok {
		return false, ch
	}
	ch := make(chan struct{})
	u.flight[pkg] = ch
	return true, ch
}

// release ends a check and wakes anything that chose to wait.
func (u *updates) release(pkg string, ch chan struct{}) {
	u.mu.Lock()
	delete(u.flight, pkg)
	u.mu.Unlock()
	close(ch)
}

// installedVersion reads the version of an installed npm package without
// running it. The one implementation is `runnersetup.InstalledVersion`, which
// the runner setup report uses too.
func installedVersion(exePath, pkg string) string {
	return runnersetup.InstalledVersion(exePath, pkg)
}

// latestPublished asks the registry what the current version is.
//
// One request to the `/latest` document, which is a few hundred bytes, rather
// than the package document, which for a runner with a long release history is
// megabytes. No npm, no node, no subprocess.
func latestPublished(ctx context.Context, pkg string) (string, error) {
	url := updateRegistry + "/" + strings.ReplaceAll(pkg, "/", "%2f") + "/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	// The abbreviated document, which is what npm itself asks for.
	req.Header.Set("Accept", "application/vnd.npm.install-v1+json, application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered %s", url, resp.Status)
	}
	var meta struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return "", err
	}
	if strings.TrimSpace(meta.Version) == "" {
		return "", errors.New("no version in the registry answer")
	}
	return strings.TrimSpace(meta.Version), nil
}

// cachedLatest reads what was last learned, and whether it is still fresh.
func (d *Daemon) cachedLatest(pkg string) (string, bool) {
	raw, err := d.st.Setting(updateSetting)
	if err != nil || strings.TrimSpace(raw) == "" {
		return "", false
	}
	var all map[string]updateCheck
	if err := json.Unmarshal([]byte(raw), &all); err != nil {
		return "", false
	}
	got, ok := all[pkg]
	if !ok || got.Latest == "" {
		return "", false
	}
	at, err := time.Parse(time.RFC3339, got.At)
	if err != nil {
		return got.Latest, false
	}
	return got.Latest, time.Since(at) < updateFresh
}

// rememberLatest writes one package's answer back.
func (d *Daemon) rememberLatest(pkg, latest string) {
	raw, _ := d.st.Setting(updateSetting)
	all := map[string]updateCheck{}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &all); err != nil {
			all = map[string]updateCheck{}
		}
	}
	all[pkg] = updateCheck{Latest: latest, At: time.Now().UTC().Format(time.RFC3339)}
	b, err := json.Marshal(all)
	if err != nil {
		return
	}
	if err := d.st.SetSetting(updateSetting, string(b)); err != nil {
		log.Printf("[atrium] could not remember the latest %s: %v", pkg, err)
	}
}

// newerVersion reports whether latest is above installed.
//
// Numeric per segment, not string comparison, which says 0.9.0 is newer than
// 0.10.0 and would raise a card telling you to downgrade. Anything that will
// not parse is NO OPINION and reports nothing, because a check that guesses
// raises work nobody asked for.
func newerVersion(latest, installed string) bool {
	a, ok1 := versionParts(latest)
	b, ok2 := versionParts(installed)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return len(a) > len(b)
}

// versionParts splits the leading dotted-numeric run of a version.
//
// Stops at the first thing that is not a number, so `1.2.3-rc1` compares as
// `1.2.3` rather than failing to parse. A prerelease sorting BELOW its own
// release is not modelled, and the cost of that is telling somebody on an rc
// that the release it came from is available.
func versionParts(v string) ([]int, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, false
	}
	var out []int
	for _, seg := range strings.Split(v, ".") {
		cut := seg
		if i := strings.IndexFunc(seg, func(r rune) bool { return r < '0' || r > '9' }); i >= 0 {
			cut = seg[:i]
		}
		n, err := strconv.Atoi(cut)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out, len(out) > 0
}

// checkRunnerUpdate looks for a newer runner and files it in the inbox.
//
// `blocking` is the whole difference between a launch somebody pressed and a
// launch that happened on its own. A person starting a runner can be told
// before it starts, so a person's launch waits for an answer it does not
// already have. A fixture coming up at boot, a source's queued launch, or a
// peer asking for one has nobody in front of it to read the answer and must
// not be delayed by a network request, so those take whatever is cached and a
// background check refreshes it for next time.
//
// NEVER RETURNS AN ERROR TO THE LAUNCH. Every failure here is logged and
// swallowed. A runner that cannot start because atrium could not reach a
// registry would be a worse product than one that never checked.
func (d *Daemon) checkRunnerUpdate(h *store.Harness, blocking bool) {
	if h == nil {
		return
	}
	pkg := strings.TrimSpace(h.Package)
	if pkg == "" {
		return
	}
	exe := lookRunner(h.Cmd)
	if exe == "" {
		return
	}
	installed := installedVersion(exe, pkg)
	if installed == "" {
		return
	}

	latest, fresh := d.cachedLatest(pkg)
	if !fresh {
		// ONLY THE FIRST CALLER GOES AND ASKS, and only a caller with somebody
		// in front of it waits for the answer. A second launch arriving while
		// a check is in flight takes the cache and goes, because two launches
		// held up to tell one person one thing is not a trade worth making.
		mine, ch := d.up.claim(pkg)
		switch {
		case mine && blocking:
			// A FLARE, because this is the only thing in atrium that makes a
			// launch wait on something outside the machine. Up to four seconds
			// of a button doing nothing reads as a hang, and the fix for that
			// is to say what is happening rather than to shorten the wait
			// until the check stops being worth making.
			d.ap.Broadcast("runner-check", map[string]any{
				"runner": h.ID, "label": h.Label, "package": pkg, "checking": true,
			})
			latest = d.askRegistry(pkg, ch)
			d.ap.Broadcast("runner-check", map[string]any{
				"runner": h.ID, "label": h.Label, "package": pkg, "checking": false,
			})
		case mine:
			go d.askRegistry(pkg, ch)
		}
	}
	if latest == "" {
		return
	}
	if !newerVersion(latest, installed) {
		d.withdrawSatisfied(h, installed)
		return
	}
	d.releaseMovedOn(h, latest)
	d.offerRunnerUpdate(h, installed, latest)
}

// runnerUpdateSource is the intake source every update card carries.
const runnerUpdateSource = "runner-update"

// runnerUpdateURL is the link an update card carries, and THE STATE CONTRACT
// for which version the card last offered.
//
// The URL is not only a link. `offeredVersion` reads the version back out of it,
// because the card's own title is prose and `refreshOffered` writes no event. A
// tidy of how cards show links, or a source-side rewrite of `url`, would quietly
// break release detection, so build and parse stay next to each other and
// nothing else touches the shape. The package goes in as npm writes it, scope and
// slash included.
func runnerUpdateURL(pkg, version string) string {
	return "https://www.npmjs.com/package/" + pkg + "/v/" + version
}

// offeredVersion reads the version out of a URL `runnerUpdateURL` wrote, and
// answers empty for any other shape. Split on the LAST `/v/`, since a package
// name could itself contain one.
func offeredVersion(pkg, url string) string {
	rest, ok := strings.CutPrefix(url, "https://www.npmjs.com/package/")
	if !ok {
		return ""
	}
	i := strings.LastIndex(rest, "/v/")
	if i < 0 || rest[:i] != pkg {
		return ""
	}
	v := rest[i+len("/v/"):]
	if v == "" || strings.ContainsAny(v, "/?# ") {
		return ""
	}
	return v
}

// updateCardsFor lists the inbox's update cards for a package: the keyed one,
// and legacy ones from before the key dropped the version, whose external id is
// `<package>@<version>`.
func (d *Daemon) updateCardsFor(pkg string) (all []*store.Task) {
	inbox, err := d.st.Offered()
	if err != nil {
		log.Printf("[atrium] could not read the inbox for %s updates: %v", pkg, err)
		return nil
	}
	for _, t := range inbox {
		if t.Source == runnerUpdateSource &&
			(t.ExternalID == pkg || strings.HasPrefix(t.ExternalID, pkg+"@")) {
			all = append(all, t)
		}
	}
	return all
}

// withdrawSatisfied archives every inbox update card for a package whose offer
// the installed version already meets, because the update happened some other
// way. Only `backlog` cards: `Offered` lists nothing else, and a started card is
// theirs. Archived, not deleted, so the history keeps it.
func (d *Daemon) withdrawSatisfied(h *store.Harness, installed string) {
	for _, t := range d.updateCardsFor(h.Package) {
		reason := fmt.Sprintf("%s %s is installed, which is what this card offered", h.ID, installed)
		if _, err := d.st.WithdrawOffered(t.ID, reason); err != nil {
			log.Printf("[atrium] could not withdraw the %s update card: %v", h.ID, err)
		}
	}
}

// releaseMovedOn makes room for a fresh card when the keyed one has moved on.
//
// Two cases. A legacy-keyed inbox card cannot be refreshed, since its key holds
// a version and `Offer` will never find it, so it is withdrawn and the fresh card
// replaces it. A keyed card past `backlog`, or archived, is released when the
// version it offered is below the new latest. No recorded version counts as
// older, so it is released at most once. A keyed card still in the inbox is left
// for `Offer` to rewrite, which is also what happens to a backlog card with no
// URL: it is rewritten to say installed to latest and gains the new URL.
func (d *Daemon) releaseMovedOn(h *store.Harness, latest string) {
	pkg := h.Package
	for _, t := range d.updateCardsFor(pkg) {
		if t.ExternalID == pkg {
			continue
		}
		reason := fmt.Sprintf("%s %s is published, and this card predates the key that lets it be rewritten", h.ID, latest)
		if _, err := d.st.WithdrawOffered(t.ID, reason); err != nil {
			log.Printf("[atrium] could not withdraw the legacy %s update card: %v", h.ID, err)
		}
	}
	t, err := d.st.ByIntakeKey(store.IntakeKey(runnerUpdateSource, pkg))
	if err != nil {
		log.Printf("[atrium] could not look up the %s update card: %v", h.ID, err)
		return
	}
	if t == nil || (t.Status == store.StatusBacklog && t.ArchivedAt == nil) {
		return
	}
	if v := offeredVersion(pkg, t.URL); v != "" && !newerVersion(latest, v) {
		return
	}
	if err := d.st.ReleaseIntakeKey(t.ID); err != nil {
		log.Printf("[atrium] could not release the %s update card: %v", h.ID, err)
	}
}

// askRegistry performs the lookup and releases the claim. Returns the version
// it learned, or the stale one if it learned nothing.
func (d *Daemon) askRegistry(pkg string, ch chan struct{}) string {
	defer d.up.release(pkg, ch)

	ctx, cancel := context.WithTimeout(context.Background(), updateBudget)
	defer cancel()
	latest, err := latestPublished(ctx, pkg)
	if err != nil {
		// Logged once and dropped. A registry that is unreachable is not the
		// operator's problem at the moment they are starting a runner.
		log.Printf("[atrium] could not ask about %s: %v", pkg, err)
		got, _ := d.cachedLatest(pkg)
		return got
	}
	d.rememberLatest(pkg, latest)
	return latest
}

// lookRunner resolves a harness command to a path on this machine.
func lookRunner(cmd string) string {
	if strings.TrimSpace(cmd) == "" {
		return ""
	}
	p, err := exec.LookPath(cmd)
	if err != nil {
		return ""
	}
	return p
}

// offerRunnerUpdate puts the news in the inbox, as ONE card per package.
//
// The key is the package and not the package and version. A second release
// arriving before the first was actioned rewrites the card that is there
// rather than adding a row that says the same thing, which is what the source
// this replaces did and what the operator saw two of.
func (d *Daemon) offerRunnerUpdate(h *store.Harness, installed, latest string) {
	label := h.Label
	if label == "" {
		label = h.ID
	}
	item := store.IntakeItem{
		Source:     runnerUpdateSource,
		ExternalID: h.Package,
		URL:        runnerUpdateURL(h.Package, latest),
		Title:      fmt.Sprintf("%s: %s to %s", label, installed, latest),
		Why: fmt.Sprintf("a newer %s is published. updating replaces the binary, so "+
			"the runner has to exit first and any card running it will go dead.", label),
		Tags: []string{"runner-update", h.ID},
		Prompt: fmt.Sprintf("run `npm install -g %s` and then report what `%s --version` says. "+
			"nothing else.", h.Package, h.Cmd),
	}
	if _, created, err := d.st.Offer(item); err != nil {
		log.Printf("[atrium] could not file the %s update: %v", label, err)
	} else if created {
		log.Printf("[atrium] %s %s is available, %s is installed", label, latest, installed)
	}
}
