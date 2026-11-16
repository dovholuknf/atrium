// Package statusline renders the Claude Code status line from the JSON payload it is given, and posts the 5h, weekly
// and context percents to the room. Every error degrades to a shorter line, never to a failure.
package statusline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type limit struct {
	UsedPercentage json.RawMessage `json:"used_percentage"`
	ResetsAt       json.RawMessage `json:"resets_at"`
}

// payload keeps every field raw: the usage log copies them through untouched, and the bar parses the few it shows.
type payload struct {
	SessionID   json.RawMessage `json:"session_id"`
	SessionName json.RawMessage `json:"session_name"`
	Model       struct {
		ID          json.RawMessage `json:"id"`
		DisplayName json.RawMessage `json:"display_name"`
	} `json:"model"`
	Effort struct {
		Level json.RawMessage `json:"level"`
	} `json:"effort"`
	FastMode  json.RawMessage `json:"fast_mode"`
	Version   json.RawMessage `json:"version"`
	Workspace struct {
		CurrentDir json.RawMessage `json:"current_dir"`
	} `json:"workspace"`
	Cwd        json.RawMessage `json:"cwd"`
	RateLimits struct {
		FiveHour limit `json:"five_hour"`
		SevenDay limit `json:"seven_day"`
	} `json:"rate_limits"`
	ContextWindow struct {
		TotalInputTokens  json.RawMessage `json:"total_input_tokens"`
		TotalOutputTokens json.RawMessage `json:"total_output_tokens"`
		ContextWindowSize json.RawMessage `json:"context_window_size"`
		UsedPercentage    json.RawMessage `json:"used_percentage"`
		CurrentUsage      json.RawMessage `json:"current_usage"`
	} `json:"context_window"`
	Cost           json.RawMessage `json:"cost"`
	PromptCache    json.RawMessage `json:"prompt_cache"`
	TranscriptPath json.RawMessage `json:"transcript_path"`
}

// logRecord is one line of ~/.claude/usage-log.jsonl, read by claude/usage/.
type logRecord struct {
	TS               int64           `json:"ts"`
	Time             string          `json:"time"`
	SessionID        json.RawMessage `json:"session_id"`
	SessionName      json.RawMessage `json:"session_name"`
	ModelID          json.RawMessage `json:"model_id"`
	ModelName        json.RawMessage `json:"model_name"`
	Effort           json.RawMessage `json:"effort"`
	FastMode         json.RawMessage `json:"fast_mode"`
	Version          json.RawMessage `json:"version"`
	Cwd              json.RawMessage `json:"cwd"`
	FiveHourPct      json.RawMessage `json:"five_hour_pct"`
	FiveHourResetsAt json.RawMessage `json:"five_hour_resets_at"`
	SevenDayPct      json.RawMessage `json:"seven_day_pct"`
	SevenDayResetsAt json.RawMessage `json:"seven_day_resets_at"`
	CtxUsed          json.RawMessage `json:"ctx_used"`
	CtxOut           json.RawMessage `json:"ctx_out"`
	CtxSize          json.RawMessage `json:"ctx_size"`
	CtxPct           json.RawMessage `json:"ctx_pct"`
	CurrentUsage     json.RawMessage `json:"current_usage"`
	Cost             json.RawMessage `json:"cost"`
	PromptCache      json.RawMessage `json:"prompt_cache"`
	TranscriptPath   json.RawMessage `json:"transcript_path"`
}

const sep = "\x1b[90m  |\x1b[0m"

var (
	keyStrip = regexp.MustCompile(`[^A-Za-z0-9-]`)
	intRe    = regexp.MustCompile(`^-?[0-9]+$`)
	cmdRe    = regexp.MustCompile(`^[[:space:]]*<(local-command|bash-)`)
)

// Render is the line for the payload in. spawn starts the detached post, so a room that is down never slows the draw.
func Render(in []byte, home string, now time.Time, spawn func(url string, body []byte)) string {
	var p payload
	parsed := unmarshal(in, &p)
	key := keyStrip.ReplaceAllString(str(p.SessionID), "")

	cwd := str(or(p.Workspace.CurrentDir, p.Cwd))
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	fhText, wkText := numText(p.RateLimits.FiveHour.UsedPercentage), numText(p.RateLimits.SevenDay.UsedPercentage)

	if parsed && key != "" {
		logUsage(home, key, &p, fhText, wkText, now)
		postTelemetry(home, key, &p, now, spawn)
	}
	ctxLimit := readCtxLimit(home, key, cwd, now)

	var b strings.Builder
	fmt.Fprintf(&b, "\x1b[32m%s\x1b[0m ", cwd)
	if br := gitBranch(cwd); br != "" {
		fmt.Fprintf(&b, "\x1b[33m(%s)\x1b[0m ", br)
	}
	fmt.Fprintf(&b, "\x1b[90m[%s]\x1b[0m", now.Format("Mon Jan 02, 15:04:05"))

	suffix := ""
	if r := untilReset(p.RateLimits.FiveHour.ResetsAt, now); r != "" {
		suffix = " (" + r + ")"
	}
	seg(&b, " 5h ", p.RateLimits.FiveHour.UsedPercentage, suffix)
	seg(&b, " wk ", p.RateLimits.SevenDay.UsedPercentage, "")

	// Context: raw tokens over the limit the session is cleared at, with a zone that gets louder as it fills: under
	// 75% sweet, 75-95% getting full, over 95% LAND THE PLANE. The limit is autoCompactWindow. The model window
	// counts only when no settings file sets it: on a 1M model that read 18% while the session was nearly due to
	// compact.
	u, uok := trunc(p.ContextWindow.TotalInputTokens)
	z := ctxLimit
	if z <= 0 {
		z, _ = trunc(p.ContextWindow.ContextWindowSize)
	}
	if uok && u >= 0 && z > 0 {
		pct := u * 100 / z
		switch {
		case pct > 95:
			fmt.Fprintf(&b, sep+"\x1b[1;97;41m  LAND THE PLANE  %d/%d  (%d%%)  /compact  \x1b[0m", u, z, pct)
		case pct >= 75:
			fmt.Fprintf(&b, sep+"\x1b[1;33m ctx %d/%d (%d%%) getting full\x1b[0m", u, z, pct)
		default:
			fmt.Fprintf(&b, sep+"\x1b[90m ctx \x1b[0m\x1b[32m%d\x1b[0m\x1b[90m/%d\x1b[0m\x1b[32m sweet\x1b[0m", u, z)
		}
	} else {
		seg(&b, " ctx ", p.ContextWindow.UsedPercentage, "")
	}

	// Transcript size: how big this session's .jsonl has grown. Big means more compaction and slower resumes.
	tpath := str(p.TranscriptPath)
	var size int64
	if fi, err := os.Stat(tpath); tpath != "" && err == nil && fi.Mode().IsRegular() {
		size = fi.Size()
	}
	if size > 0 {
		mb := size / 1000000
		hs := fmt.Sprintf("%dB", size)
		if size >= 1000000 {
			hs = fmt.Sprintf("%dM", mb)
		} else if size >= 1000 {
			hs = fmt.Sprintf("%dK", size/1000)
		}
		tc := 90
		if mb >= 50 {
			tc = 31
		} else if mb >= 20 {
			tc = 33
		}
		fmt.Fprintf(&b, sep+"\x1b[%dm tx %s\x1b[0m", tc, hs)
		if key != "" {
			turnTokens(&b, home, key, tpath, size)
		}
	}

	return b.String()
}

// unmarshal reports whether data is JSON. A field of the wrong type is skipped, not fatal.
func unmarshal(data []byte, v any) bool {
	err := json.Unmarshal(data, v)
	var te *json.UnmarshalTypeError
	return err == nil || errors.As(err, &te)
}

// present is false for null and false, so a field that says nothing falls back.
func present(raw json.RawMessage) bool {
	s := string(bytes.TrimSpace(raw))
	return s != "" && s != "null" && s != "false"
}

func or(a, b json.RawMessage) json.RawMessage {
	if present(a) {
		return a
	}
	return b
}

func str(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// numText is the number as it was written, or -1 when absent. The usage-log state stores it for comparison.
func numText(raw json.RawMessage) string {
	var f float64
	if !present(raw) || json.Unmarshal(raw, &f) != nil {
		return "-1"
	}
	return string(bytes.TrimSpace(raw))
}

// trunc drops the fraction.
func trunc(raw json.RawMessage) (int64, bool) {
	var f float64
	if !present(raw) || json.Unmarshal(raw, &f) != nil {
		return 0, false
	}
	return int64(f), true
}

// readState returns the whitespace-separated fields of a small state file, or nil.
func readState(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	line, _, _ := strings.Cut(string(data), "\n")
	return strings.Fields(line)
}

func field(f []string, i int) string {
	if i < len(f) {
		return f[i]
	}
	return ""
}

func atoi(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// logUsage appends the payload to ~/.claude/usage-log.jsonl when this session's 5h or 7d percent changes, else at
// most once a minute. Per-session state ("fh wk epoch") lives in one small file.
func logUsage(home, key string, p *payload, fh, wk string, now time.Time) {
	dir := filepath.Join(home, ".claude", "usage-log.state")
	st := readState(filepath.Join(dir, key))
	lts := atoi(field(st, 2))
	if fh == field(st, 0) && wk == field(st, 1) && now.Unix()-lts < 60 {
		return
	}
	rec := logRecord{
		TS: now.Unix(), Time: now.UTC().Format("2006-01-02T15:04:05Z"),
		SessionID: p.SessionID, SessionName: p.SessionName,
		ModelID: p.Model.ID, ModelName: p.Model.DisplayName,
		Effort: p.Effort.Level, FastMode: p.FastMode, Version: p.Version,
		Cwd:         or(p.Workspace.CurrentDir, p.Cwd),
		FiveHourPct: p.RateLimits.FiveHour.UsedPercentage, FiveHourResetsAt: p.RateLimits.FiveHour.ResetsAt,
		SevenDayPct: p.RateLimits.SevenDay.UsedPercentage, SevenDayResetsAt: p.RateLimits.SevenDay.ResetsAt,
		CtxUsed: p.ContextWindow.TotalInputTokens, CtxOut: p.ContextWindow.TotalOutputTokens,
		CtxSize: p.ContextWindow.ContextWindowSize, CtxPct: p.ContextWindow.UsedPercentage,
		CurrentUsage: p.ContextWindow.CurrentUsage,
		Cost:         p.Cost, PromptCache: p.PromptCache, TranscriptPath: p.TranscriptPath,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if enc.Encode(rec) != nil || os.MkdirAll(dir, 0o755) != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(home, ".claude", "usage-log.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_, err = f.Write(buf.Bytes())
	f.Close()
	if err == nil {
		os.WriteFile(filepath.Join(dir, key), fmt.Appendf(nil, "%s %s %d\n", fh, wk, now.Unix()), 0o644)
	}
}

// readCtxLimit returns autoCompactWindow from the settings files Claude Code reads it from, lowest precedence first,
// or -1 when none sets it. The value is cached per session ("limit epoch") and re-read at most once a minute.
func readCtxLimit(home, key, cwd string, now time.Time) int64 {
	if key == "" {
		key = "nosession"
	}
	dir := filepath.Join(home, ".claude", "statusline-ctx.state")
	st := readState(filepath.Join(dir, key))
	lim, ts := field(st, 0), field(st, 1)
	if intRe.MatchString(lim) && intRe.MatchString(ts) && now.Unix()-atoi(ts) < 60 {
		return atoi(lim)
	}
	cfg := os.Getenv("CLAUDE_CONFIG_DIR")
	if cfg == "" {
		cfg = filepath.Join(home, ".claude")
	}
	val := int64(-1)
	for _, f := range []string{
		filepath.Join(cfg, "settings.json"),
		filepath.Join(cwd, ".claude", "settings.json"),
		filepath.Join(cwd, ".claude", "settings.local.json"),
	} {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var s map[string]json.RawMessage
		if json.Unmarshal(data, &s) != nil || !present(s["autoCompactWindow"]) {
			continue
		}
		val = -1
		if t := string(bytes.TrimSpace(s["autoCompactWindow"])); intRe.MatchString(t) {
			val = atoi(t)
		}
	}
	if os.MkdirAll(dir, 0o755) == nil {
		os.WriteFile(filepath.Join(dir, key), fmt.Appendf(nil, "%d %d\n", val, now.Unix()), 0o644)
	}
	return val
}

// gitBranch reads HEAD straight from the repo that holds dir, so the bar costs no git process. A worktree's .git is a
// file pointing at its own HEAD. A detached HEAD reads "HEAD", as rev-parse --abbrev-ref prints it.
func gitBranch(dir string) string {
	for d := dir; d != ""; {
		g := filepath.Join(d, ".git")
		if fi, err := os.Stat(g); err == nil {
			if !fi.IsDir() {
				data, err := os.ReadFile(g)
				rest, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
				if err != nil || !ok {
					return ""
				}
				g = strings.TrimSpace(rest)
				if !filepath.IsAbs(g) {
					g = filepath.Join(d, g)
				}
			}
			head, err := os.ReadFile(filepath.Join(g, "HEAD"))
			if err != nil {
				return ""
			}
			if ref, ok := strings.CutPrefix(strings.TrimSpace(string(head)), "ref: "); ok {
				return strings.TrimPrefix(ref, "refs/heads/")
			}
			return "HEAD"
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
	return ""
}

// pctColor is green under 60, yellow to 85, red above.
func pctColor(p int64) int {
	switch {
	case p >= 85:
		return 31
	case p >= 60:
		return 33
	}
	return 32
}

// untilReset is "2h14m" until the given epoch, empty if unset or past.
func untilReset(raw json.RawMessage, now time.Time) string {
	at, ok := trunc(raw)
	if !ok || at <= 0 {
		return ""
	}
	d := at - now.Unix()
	switch {
	case d <= 0:
		return ""
	case d >= 3600:
		return fmt.Sprintf("%dh%02dm", d/3600, (d%3600)/60)
	}
	return fmt.Sprintf("%dm", d/60)
}

// seg appends a " | <label><pct>%<suffix>" segment, or nothing when the percent is absent.
func seg(b *strings.Builder, label string, raw json.RawMessage, suffix string) {
	pct, ok := trunc(raw)
	if !ok || pct < 0 {
		return
	}
	fmt.Fprintf(b, sep+"\x1b[90m%s\x1b[0m\x1b[%dm%d%%\x1b[0m", label, pctColor(pct), pct)
	if suffix != "" {
		fmt.Fprintf(b, "\x1b[90m%s\x1b[0m", suffix)
	}
}

// fmtk turns 1234567 into "1.2M" and 12345 into "12k".
func fmtk(n int64) string {
	switch {
	case n >= 1000000:
		return fmt.Sprintf("%d.%dM", n/1000000, (n%1000000)/100000)
	case n >= 1000:
		return fmt.Sprintf("%dk", n/1000)
	}
	return strconv.FormatInt(n, 10)
}

type usage struct {
	InputTokens  int64 `json:"input_tokens"`
	CacheRead    int64 `json:"cache_read_input_tokens"`
	CacheWrite   int64 `json:"cache_creation_input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type entry struct {
	Type        string `json:"type"`
	IsMeta      bool   `json:"isMeta"`
	IsSidechain bool   `json:"isSidechain"`
	Message     struct {
		ID      *string         `json:"id"`
		Content json.RawMessage `json:"content"`
		Usage   *usage          `json:"usage"`
	} `json:"message"`
}

// isPrompt is a user-typed prompt: not meta, not a sidechain, not a slash-command or ! echo, not a tool result.
func (e *entry) isPrompt() bool {
	if e.Type != "user" || e.IsMeta || e.IsSidechain {
		return false
	}
	c := bytes.TrimSpace(e.Message.Content)
	if len(c) == 0 {
		return false
	}
	switch c[0] {
	case '"':
		var s string
		return json.Unmarshal(c, &s) == nil && !cmdRe.MatchString(s)
	case '[':
		var items []struct {
			Type string `json:"type"`
		}
		if !unmarshal(c, &items) {
			return false
		}
		text := false
		for _, it := range items {
			if it.Type == "tool_result" {
				return false
			}
			text = text || it.Type == "text"
		}
		return text
	}
	return false
}

// turnTokens appends the tokens since the last prompt: context re-read, output, cache written. The figure grows
// while the turn runs and resets on the next prompt. "<key>.turn" holds the byte offset already read plus the running
// totals, so a render reads only the new transcript lines. Streamed replies repeat one message id across entries, so
// the open id is kept apart and merged by max. A first look at a big transcript starts 4 MB from the end.
//
// Cache misses: Claude Code caches the whole prefix, so uncached input stays at a few tokens and a miss shows up as a
// call that writes more cache than it reads (the context is re-written after the TTL lapses or the model changes). m
// counts those calls. Any miss turns the written figure red and appends "<m> miss".
func turnTokens(b *strings.Builder, home, key, tpath string, size int64) {
	dir := filepath.Join(home, ".claude", "usage-log.state")
	stPath := filepath.Join(dir, key+".turn")
	f := readState(stPath)
	off := int64(-1)
	if t := field(f, 0); intRe.MatchString(t) && !strings.HasPrefix(t, "-") {
		off = atoi(t)
	}
	r, w, o := atoi(field(f, 1)), atoi(field(f, 2)), atoi(field(f, 3))
	cid := field(f, 4)
	cr, cw, co, m := atoi(field(f, 5)), atoi(field(f, 6)), atoi(field(f, 7)), atoi(field(f, 8))
	if off < 0 || off > size {
		off, r, w, o, cid, cr, cw, co, m = 0, 0, 0, 0, "-", 0, 0, 0, 0
	}
	if size > off {
		if off == 0 && size > 4000000 {
			off = size - 4000000
		}
		data, err := readFrom(tpath, off)
		if err != nil {
			return
		}
		lines := bytes.Split(data, []byte("\n"))
		for _, line := range lines[:len(lines)-1] {
			var e entry
			if !unmarshal(line, &e) {
				continue
			}
			u := e.Message.Usage
			switch {
			case e.isPrompt():
				r, w, o, cid, cr, cw, co, m = 0, 0, 0, "-", 0, 0, 0, 0
			case e.Type == "assistant" && u != nil && !e.IsSidechain:
				c0, c1, c2 := u.CacheRead+u.InputTokens, u.CacheWrite, u.OutputTokens
				if e.Message.ID != nil && *e.Message.ID == cid {
					cr, cw, co = max(cr, c0), max(cw, c1), max(co, c2)
					continue
				}
				if cw > cr {
					m++
				}
				r, w, o = r+cr, w+cw, o+co
				cid = "-"
				if e.Message.ID != nil {
					cid = *e.Message.ID
				}
				cr, cw, co = c0, c1, c2
			}
		}
		off += int64(len(data) - len(lines[len(lines)-1]))
		if os.MkdirAll(dir, 0o755) == nil {
			os.WriteFile(stPath, fmt.Appendf(nil, "%d %d %d %d %s %d %d %d %d\n", off, r, w, o, cid, cr, cw, co, m), 0o644)
		}
	}
	if cw > cr {
		m++
	}
	kr, ko, kw := fmtk(r+cr), fmtk(o+co), fmtk(w+cw)
	if m > 0 {
		fmt.Fprintf(b, "\x1b[90m  | r/o/w\x1b[0m %s/%s/\x1b[1;31m%s %d miss\x1b[0m", kr, ko, kw, m)
	} else {
		fmt.Fprintf(b, "\x1b[90m  | r/o/w\x1b[0m %s/%s/%s", kr, ko, kw)
	}
}

func readFrom(path string, off int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(f)
}
