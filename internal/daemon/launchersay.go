package daemon

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// A SAY FROM THE CARD'S OWN LAUNCHER IS AN INSTRUCTION, NOT A PASTE.
//
// Every other peer say is wrapped in bracketed paste markers (see tellByTyping), and Claude Code shows
// bracketed paste as pasted content, which a worker rightly treats as untrusted data. A launcher's say
// reached a worker that way and cost a round trip asking for confirmation. So this one is typed as plain
// keystrokes on ONE line: a newline typed without the markers would submit early, and a long unbracketed
// burst is itself read as a paste. Text that is multi-line or longer than launcherSayInline goes to a file
// the line points at.

// launcherSayInline is the longest say typed inline.
const launcherSayInline = 300

// isLauncherOf says whether from is target's launcher: the sender recorded as launching it, by work item.
func (d *Daemon) isLauncherOf(from string, target *store.Task) bool {
	if from == "" || target == nil {
		return false
	}
	sender, err := d.st.GetByWireName(from)
	if err != nil {
		if sender, err = d.st.GetByWireName(d.st.Qualify(from)); err != nil {
			return false
		}
	}
	w, err := d.st.WorkItem(target.ID)
	return err == nil && w.LauncherID == sender.ID
}

// launcherBanner names the launcher, so the line says who is instructing the worker.
func launcherBanner(from string) string {
	return atriumLabel("your launcher @" + from + " instructs you:")
}

// launcherSayBody is the single line typed after the banner.
func launcherSayBody(taskID, text string) string {
	if !strings.ContainsAny(text, "\r\n\x1b") && len(text) <= launcherSayInline {
		return text
	}
	path := filepath.Join(os.TempDir(), "atrium-says", taskID+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
		if err = os.WriteFile(path, []byte(stripPasteMarkers(text)), 0o600); err == nil {
			return "read " + path + " and do what it says."
		}
	}
	// No file: flatten to one line rather than lose the words.
	return flattenSay(text)
}

// flattenSay joins lines with a space and drops escapes, so typing it cannot submit early.
func flattenSay(text string) string {
	text = strings.Map(func(r rune) rune {
		if r == '\x1b' {
			return -1
		}
		return r
	}, text)
	return strings.Join(strings.Fields(text), " ")
}
