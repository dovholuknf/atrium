package daemon

import (
	"net/http"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// ItemTag marks a card launched with `item`: a task whose only routing is "tell whoever wanted it when it is done".
// The same tag as link.ItemTag. See docs/rnd/atrium-items-decisions.md.
//
// STAGE 1 IS CONVENTION, NOT A TABLE. An item launch changes what the worker is told and what it can reach, and
// stores nothing new: the tag is the whole mark. An ordinary launch is unchanged in every way.
const ItemTag = "atrium:item"

// isItem is whether a card was launched as an item.
func isItem(t *store.Task) bool { return t != nil && hasTag(t.Tags, ItemTag) }

// ItemEndingLine is how an item worker is told to end. LaunchEndingLine without a launcher in it: an item worker is
// not told who asked for the work, because it has nobody to talk to but the tool that ends it. Kept equal to
// link.itemReportLine by a test in internal/link.
const ItemEndingLine = "Before you end your turn, call either:\n" +
	"- atrium_done <sha or artifact> [one line] when the work is finished\n" +
	"- atrium_blocked <up to 50 words> when something stops you, such as a question you need answered, a tool you need installed, or a permission you need.\n" +
	"Then stop. Say nothing more.\n" +
	"Run tests and builds in the foreground, with a longer command timeout if needed. If a command must run in the background, wait for it in the same turn. Never end a turn while a command you started is still running.\n" +
	"Your commit is read from your worktree or any hub branch, so never ask where to push and never offer a push to origin. If you are asked to stop, answer with atrium_done and your last sha."

// ItemBriefPrompt is BriefPrompt for an item, which closes on ItemEndingLine.
func ItemBriefPrompt(file, prompt string) string {
	return briefPromptEnding(file, prompt, ItemEndingLine)
}

// openingSender is who the opening prompt is recorded as coming from: the session that asked for the launch, which
// is what makes the first turn owe it a report, and nobody for the operator's own launch. An item's is atrium, so
// nothing the worker reads names a launcher. atrium is not the card's launcher, so the first turn owes nothing.
func openingSender(req LaunchRequest) string {
	if req.Item || hasTag(req.Tags, ItemTag) {
		return store.ReservedHandle
	}
	if by := strings.TrimSpace(req.SpawnedBy); by != "" && by != store.HumanLauncher {
		return by
	}
	return ""
}

// itemSayRefused answers a say from a running item card and says so. FALSE for any other sender, including an
// unknown one, so nothing else changes.
//
// NO EXCEPTIONS: not for the operator's launcher and not for the card's owner. The tool class already drops
// atrium_say, but the class is tidiness and not a boundary (internal/link/ctlclass.go), so the endpoint says it too.
// A card that has ended can say what it likes: only a card that is still running is cut off.
func (d *Daemon) itemSayRefused(from string) bool {
	from = strings.TrimSpace(from)
	if from == "" {
		return false
	}
	sender, err := d.st.GetByWireName(d.st.Qualify(from))
	if err != nil || !isItem(sender) {
		return false
	}
	return sender.Status != store.StatusDead && sender.Status != store.StatusDone
}

// itemSayRefusal is the answer to a say from a running item card.
const itemSayRefusal = "item workers cannot say anything. end with atrium_done when the work is finished or " +
	"atrium_blocked when something stops you. If a person asks you something in your terminal, answer them there."

// writeItemSayRefused answers a say that itemSayRefused refused.
func writeItemSayRefused(w http.ResponseWriter) {
	writeJSONErr(w, http.StatusForbidden, errString(itemSayRefusal))
}

// itemResultKey is the notice key of an item's result: the card, the generation of its session and the word. A
// re-delivery of the same result is the same key, so it is one notice however the words differ. A `blocked` is a
// different word, because a worker that said it was blocked and later finished has two things to tell its launcher.
func itemResultKey(taskID string, generation int, status string) string {
	word := "result"
	if status == ReportBlocked {
		word = ReportBlocked
	}
	return taskID + "/" + itoa(generation) + "/" + word
}
