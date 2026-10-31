package daemon

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/daemon/screen"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/small"
)

// A room whose claude login is revoked. See docs/changes/bug-room-auth-revoked-silent.md.
//
// Claude Code answers every prompt with `OAuth token revoked · Please run /login` once the account's token is gone,
// and every card in the room then sits at that line until a human happens to look. Atrium owns the terminals, so it
// reads them: one card showing the line marks the ROOM as needing a login, the room refuses new claude launches
// with that reason, and the hub says so once on the board's bell and in `/_hub/rooms`.
//
// OBSERVED, NEVER ACTED ON. Atrium does not log in, does not touch a credential and does not kill a session that
// shows the line: a human running `/login` in any one of them fixes the account, and the others recover on their own.
//
// IT CLEARS ITSELF, three ways. A card that was showing the line stops showing it. The line goes unseen for
// loginNeededTTL (a flag raised from a card that has since died would otherwise hold launches for ever, and the
// launch that would prove the login is the very thing refused). Or the operator says so, `DELETE /v1/login-needed`.
// A wrong flag costs one refused launch and a sentence saying how to lift it, never a stuck room.

// loginNeededTTL is how long a flag outlives its last sighting.
const loginNeededTTL = 15 * time.Minute

// loginNeededSay is what a refused launch is told.
const loginNeededSay = "claude needs a login on this machine. run /login in any session here, or lift the flag " +
	"from the board once it is done"

// revokedLogin returns the line of a rendered screen that says the login is gone, or "".
//
// Claude Code's own words, so they live here and nowhere else. Two shapes: the revoked token, and the expired one,
// both followed by the instruction. Anything quoting them in prose (a card discussing this very bug) has to carry
// the whole sentence on one line of a screen, which is why the match is the line and not a substring of the screen.
func revokedLogin(text string) string {
	for _, ln := range strings.Split(text, "\n") {
		low := strings.ToLower(stripGlyphPrefix(strings.TrimSpace(ln)))
		if !strings.Contains(low, "please run /login") {
			continue
		}
		if strings.HasPrefix(low, "oauth token revoked") || strings.HasPrefix(low, "oauth token has expired") {
			return strings.TrimSpace(ln)
		}
	}
	return ""
}

// stripGlyphPrefix drops the bullets, box lines and spaces Claude Code draws before a message, so what is left
// starts with the message itself. A diff line or quoted prose starts with its own marks and does not match.
func stripGlyphPrefix(s string) string {
	return strings.TrimLeftFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || (r > 0x7f && !unicode.IsLetter(r) && !unicode.IsDigit(r))
	})
}

// loginState is the flag and what the scan remembers between ticks.
type loginState struct {
	mu    sync.Mutex
	on    bool
	since time.Time
	seen  time.Time
	line  string
	// card is the card the line was last read off, for the log and the board.
	card string
	// verdicts is, per runner, the last output stamp that was rendered and what it said, so a quiet screen is
	// not rendered again on every tick.
	verdicts map[string]loginVerdict
}

type loginVerdict struct {
	out  int64
	line string
	// acked: the operator lifted the flag while this verdict showed the line. It counts again only on a new
	// output stamp that still shows it.
	acked bool
}

// LoginNeeded is the flag as the board and the hub read it.
type LoginNeeded struct {
	Reason string    `json:"reason"`
	Line   string    `json:"line,omitempty"`
	Card   string    `json:"card,omitempty"`
	Since  time.Time `json:"since"`
}

// loginNeeded is the flag, or nil.
func (d *Daemon) loginNeeded() *LoginNeeded {
	d.login.mu.Lock()
	defer d.login.mu.Unlock()
	if !d.login.on {
		return nil
	}
	return &LoginNeeded{Reason: loginNeededSay, Line: d.login.line, Card: d.login.card, Since: d.login.since}
}

// LoginNeededReason is the flag's sentence for the link's beat, "" when it is down.
func (d *Daemon) LoginNeededReason() string {
	if l := d.loginNeeded(); l != nil {
		return l.Reason
	}
	return ""
}

// launchNeedsLogin refuses a claude launch while the flag is up. A runner that is not claude has its own login.
func (d *Daemon) launchNeedsLogin(req shared.LaunchRequest) error {
	if d.loginNeeded() == nil {
		return nil
	}
	// A harness is claude by its id or by the command it runs, which is how a renamed one still counts.
	h, err := d.st.Harness(req.Harness)
	if err != nil || !(strings.EqualFold(h.ID, "claude") || strings.Contains(strings.ToLower(h.Cmd), "claude")) {
		return nil
	}
	return small.ErrString(d.roomWord() + ": " + loginNeededSay)
}

// watchLoginNeeded is one pass over the runners' last screens, on the reaper's tick. It renders a runner only when
// its terminal has produced output since the last look, and it never writes the store.
func (d *Daemon) watchLoginNeeded(now time.Time) {
	runs := d.sup.all()
	d.login.mu.Lock()
	if d.login.verdicts == nil {
		d.login.verdicts = map[string]loginVerdict{}
	}
	live := make(map[string]bool, len(runs))
	d.login.mu.Unlock()

	var hit, card string
	for _, run := range runs {
		if run.buf == nil {
			continue
		}
		if t, err := d.st.Get(run.taskID); err != nil {
			continue
		} else if h, err := d.st.Harness(t.Runner); err != nil || !api.IsClaudeHarness(h) {
			continue
		}
		live[run.taskID] = true
		stamp := run.lastOut.Load()
		d.login.mu.Lock()
		v, known := d.login.verdicts[run.taskID]
		d.login.mu.Unlock()
		if !known || v.out != stamp {
			cols, rows := run.buf.CurrentSize()
			sc := screen.NewScreenSized(cols, rows)
			sc.Apply(run.buf.Tail(screen.FrameTailBytes))
			v = loginVerdict{out: stamp, line: revokedLogin(screen.FrameText(sc))}
			d.login.mu.Lock()
			d.login.verdicts[run.taskID] = v
			d.login.mu.Unlock()
		}
		if v.line != "" && !v.acked && hit == "" {
			hit, card = v.line, run.taskID
		}
	}

	d.login.mu.Lock()
	for id := range d.login.verdicts {
		if !live[id] {
			delete(d.login.verdicts, id)
		}
	}
	changed := false
	switch {
	case hit != "":
		d.login.seen, d.login.line, d.login.card = now, hit, card
		if !d.login.on {
			d.login.on, d.login.since = true, now
			changed = true
			log.Printf("[atrium] claude login needed: %s shows %q. new claude launches are refused until it clears",
				card, hit)
		}
	case d.login.on && (len(live) > 0 || now.Sub(d.login.seen) > loginNeededTTL):
		// A runner is up and none of them shows the line, or it has been unseen too long to trust.
		d.login.on = false
		changed = true
		log.Printf("[atrium] claude login needed: cleared, no screen shows the line any more")
	}
	d.login.mu.Unlock()
	if changed {
		d.publishHealth()
	}
}

// clearLoginNeeded is the operator lifting the flag. Reports whether it was up.
func (d *Daemon) clearLoginNeeded() bool {
	d.login.mu.Lock()
	was := d.login.on
	d.login.ackVerdicts()
	d.login.on = false
	d.login.mu.Unlock()
	if was {
		log.Printf("[atrium] claude login needed: lifted by the operator")
		d.publishHealth()
	}
	return was
}

// handleLoginNeeded is `/v1/login-needed`: GET says the flag, DELETE lifts it. On the human listener.
func (d *Daemon) handleLoginNeeded(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodDelete {
		d.clearLoginNeeded()
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"login_needed": d.loginHealth()})
}

// loginHealth is what `/v1/health` and the `health` event carry: absent when fine.
func (d *Daemon) loginHealth() map[string]any {
	l := d.loginNeeded()
	if l == nil {
		return nil
	}
	return map[string]any{"reason": l.Reason, "line": l.Line, "card": l.Card, "since": l.Since}
}

// ackVerdicts marks every cached verdict that shows the line as seen by the operator. The caller holds mu.
func (s *loginState) ackVerdicts() {
	for id, v := range s.verdicts {
		if v.line != "" {
			v.acked = true
			s.verdicts[id] = v
		}
	}
}
