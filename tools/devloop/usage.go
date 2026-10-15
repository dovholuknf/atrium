package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A port of the sg4 build.ps1 that summarized a night of usage. Inputs are the per-room exports it read
// (usage-<room>-*.jsonl, tasks-<room>-*.json, newest by name) plus the says, paged from each room over the hub.

type turn struct {
	ID           string `json:"id"`
	TaskID       string `json:"task_id"`
	Cause        string `json:"cause"`
	StartedAt    string `json:"started_at"`
	EndedAt      string `json:"ended_at"`
	Model        string `json:"model"`
	Input        int64  `json:"input"`
	CacheWrite5m int64  `json:"cache_write_5m"`
	CacheWrite1h int64  `json:"cache_write_1h"`
	CacheRead    int64  `json:"cache_read"`
	Output       int64  `json:"output"`
	Room         string `json:"-"`
}

func (t turn) cacheWrite() int64 { return t.CacheWrite5m + t.CacheWrite1h }

type card struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	DisplayTitle string `json:"display_title"`
	Status       string `json:"status"`
	SpawnedBy    string `json:"spawned_by"`
	CreatedAt    string `json:"created_at"`
	Room         string `json:"-"`
}

type say struct {
	ID     string `json:"id"`
	From   string `json:"from"`
	To     string `json:"to"`
	ToTask string `json:"to_task"`
	Kind   string `json:"kind"`
	When   string `json:"when"`
	State  string `json:"state"`
	Chan   string `json:"channel"`
	Chars  int    `json:"chars"`
	SentAt string `json:"sent_at"`
	Room   string `json:"-"`
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// inWindow keeps turns that started at or after since, and before until when until is set.
func inWindow(ts []turn, since, until time.Time) []turn {
	var out []turn
	for _, t := range ts {
		s := parseTime(t.StartedAt)
		if s.Before(since) || (!until.IsZero() && !s.Before(until)) {
			continue
		}
		out = append(out, t)
	}
	return out
}

func cardTitle(cards map[string]card, room, id string) string {
	if c, ok := cards[room+"~"+id]; ok {
		if c.DisplayTitle != "" {
			return c.DisplayTitle
		}
		return c.Title
	}
	return id
}

// matchSays pairs each say (not to another room) with the receiver's first say-caused turn that began from 5
// seconds before it to 10 minutes after it, each turn used once. Ordered by send time.
func matchSays(says []say, turns []turn) [][]string {
	sorted := append([]say(nil), says...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].SentAt < sorted[j].SentAt })
	used := map[string]bool{}
	var rows [][]string
	for _, s := range sorted {
		if strings.Contains(s.To, "@") {
			continue
		}
		at := parseTime(s.SentAt)
		var best *turn
		for i := range turns {
			t := &turns[i]
			st := parseTime(t.StartedAt)
			if t.Room != s.Room || t.TaskID != s.ToTask || t.Cause != "say" || used[t.ID] ||
				st.Before(at.Add(-5*time.Second)) || st.After(at.Add(10*time.Minute)) {
				continue
			}
			if best == nil || t.StartedAt < best.StartedAt {
				best = t
			}
		}
		row := []string{s.SentAt, s.Room, s.From, s.To, s.Kind, s.When, s.State, s.Chan, strconv.Itoa(s.Chars)}
		if best != nil {
			used[best.ID] = true
			row = append(row, "true", n(best.Input), n(best.cacheWrite()), n(best.CacheRead), n(best.Output))
		} else {
			row = append(row, "false", "", "", "", "")
		}
		rows = append(rows, row)
	}
	return rows
}

var saysHeader = []string{"time", "room", "from", "to", "kind", "when", "delivered", "channel", "chars",
	"turn_started", "input", "cache_write", "cache_read", "output"}

func n(v int64) string { return strconv.FormatInt(v, 10) }

type totals struct{ turns, sayTurns, input, cacheWrite, cacheRead, output int64 }

func (a *totals) add(t turn) {
	a.turns++
	if t.Cause == "say" {
		a.sayTurns++
	}
	a.input += t.Input
	a.cacheWrite += t.cacheWrite()
	a.cacheRead += t.CacheRead
	a.output += t.Output
}

func (a totals) row(room, card string) []string {
	return []string{room, card, n(a.turns), n(a.sayTurns), n(a.input), n(a.cacheWrite), n(a.cacheRead), n(a.output)}
}

var tokensHeader = []string{"room", "card", "turns", "say_turns", "input", "cache_write", "cache_read", "output"}

// tokenRows is one row per room and card, biggest cache_read first, then an ALL row. all is returned for the summary.
func tokenRows(turns []turn, cards map[string]card) (rows [][]string, all totals) {
	type key struct{ room, id string }
	per := map[key]*totals{}
	var order []key
	for _, t := range turns {
		k := key{t.Room, t.TaskID}
		if per[k] == nil {
			per[k] = &totals{}
			order = append(order, k)
		}
		per[k].add(t)
		all.add(t)
	}
	sort.SliceStable(order, func(i, j int) bool { return per[order[i]].cacheRead > per[order[j]].cacheRead })
	for _, k := range order {
		rows = append(rows, per[k].row(k.room, cardTitle(cards, k.room, k.id)))
	}
	return append(rows, all.row("ALL", "ALL")), all
}

var cardsHeader = []string{"card", "room", "status", "launcher", "model", "started", "ended", "turns",
	"input", "cache_write", "cache_read", "output", "total", "landed"}

// cardRows is one row per card created at or after since, oldest first. landed maps a card's title to the sha it
// landed as.
func cardRows(cards map[string]card, turns []turn, since time.Time, landed map[string]string) [][]string {
	var cs []card
	for _, c := range cards {
		if parseTime(c.CreatedAt).Before(since) {
			continue
		}
		cs = append(cs, c)
	}
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].CreatedAt != cs[j].CreatedAt {
			return cs[i].CreatedAt < cs[j].CreatedAt
		}
		return cs[i].Room+cs[i].ID < cs[j].Room+cs[j].ID
	})
	var rows [][]string
	for _, c := range cs {
		var a totals
		var models []string
		ended := ""
		for _, t := range turns {
			if t.Room != c.Room || t.TaskID != c.ID {
				continue
			}
			a.add(t)
			seen := false
			for _, m := range models {
				seen = seen || m == t.Model
			}
			if !seen {
				models = append(models, t.Model)
			}
			if t.EndedAt > ended {
				ended = t.EndedAt
			}
		}
		l := landed[c.Title]
		if l == "" {
			l = "none"
		}
		rows = append(rows, []string{cardTitle(cards, c.Room, c.ID), c.Room, c.Status, c.SpawnedBy,
			strings.Join(models, " "), c.CreatedAt, ended, n(a.turns), n(a.input), n(a.cacheWrite), n(a.cacheRead),
			n(a.output), n(a.input + a.cacheWrite + a.cacheRead + a.output), l})
	}
	return rows
}

func writeCSV(path string, header []string, rows [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	_ = w.Write(header)
	_ = w.WriteAll(rows)
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func summary(since, until time.Time, saysN, turnSays, sessions, cardsN int, all totals) string {
	end := "now"
	if !until.IsZero() {
		end = until.Format(time.RFC3339)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Usage %s to %s\n\n", since.Format(time.RFC3339), end)
	fmt.Fprintf(&b, "- says: %d (turn-starting %d)\n- sessions: %d\n- cards: %d\n\n", saysN, turnSays, sessions, cardsN)
	b.WriteString("| turns | say turns | input | cache write | cache read | output |\n| --- | --- | --- | --- | --- | --- |\n")
	fmt.Fprintf(&b, "| %d | %d | %d | %d | %d | %d |\n", all.turns, all.sayTurns, all.input, all.cacheWrite,
		all.cacheRead, all.output)
	return b.String()
}

// newest returns the last file by name matching dir/pattern.
func newest(dir, pattern string) (string, error) {
	m, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil || len(m) == 0 {
		return "", fmt.Errorf("no %s in %s", pattern, dir)
	}
	sort.Strings(m)
	return m[len(m)-1], nil
}

func readTurns(path, room string) ([]turn, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []turn
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" {
			continue
		}
		var t turn
		if err := json.Unmarshal([]byte(l), &t); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		t.Room = room
		out = append(out, t)
	}
	return out, sc.Err()
}

func readCards(path, room string, into map[string]card) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var d struct {
		Tasks []card `json:"tasks"`
	}
	if err := json.Unmarshal(b, &d); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	for _, c := range d.Tasks {
		c.Room = room
		into[room+"~"+c.ID] = c
	}
	return nil
}

// fetchSays pages /v1/says from one room, newest page first, following `before` while the room says there is more.
func fetchSays(hub, room string, since time.Time) ([]say, error) {
	var all []say
	before := ""
	for {
		q := url.Values{"since": {since.Format(time.RFC3339)}}
		if before != "" {
			q.Set("before", before)
		}
		var p struct {
			Says []say `json:"says"`
			More bool  `json:"more"`
		}
		if err := hubCall(hub, http.MethodGet, "/v1/says?"+q.Encode(), room, nil, &p); err != nil {
			return nil, err
		}
		for _, s := range p.Says {
			s.Room = room
			all = append(all, s)
		}
		if len(p.Says) == 0 || !p.More {
			return all, nil
		}
		before = p.Says[len(p.Says)-1].ID
	}
}

func cmdUsage(args []string, out io.Writer) error {
	_, flags := splitArgs(args, "since", "until", "src", "out", "rooms", "hub", "landed", "name")
	fs := flag.NewFlagSet("usage", flag.ContinueOnError)
	sinceS := fs.String("since", "", "window start, RFC3339")
	untilS := fs.String("until", "", "window end, RFC3339, default now")
	src := fs.String("src", ".", "directory with usage-<room>-*.jsonl and tasks-<room>-*.json")
	dir := fs.String("out", ".", "directory to write the CSVs and summary to")
	rooms := fs.String("rooms", "claude-sg4,m1mini,sg3,sg4-control,sgg", "comma separated rooms")
	hub := fs.String("hub", defaultHub, "hub base URL")
	landedF := fs.String("landed", "", "JSON file mapping card title to the sha it landed as")
	name := fs.String("name", "", "suffix for the output file names, default the since date")
	if err := fs.Parse(flags); err != nil {
		return err
	}
	since := parseTime(*sinceS)
	if since.IsZero() {
		return fmt.Errorf("--since needs an RFC3339 time")
	}
	var until time.Time
	if *untilS != "" {
		if until = parseTime(*untilS); until.IsZero() {
			return fmt.Errorf("--until needs an RFC3339 time")
		}
	}
	suffix := *name
	if suffix == "" {
		suffix = since.Format("2006-01-02")
	}
	landed := map[string]string{}
	if *landedF != "" {
		b, err := os.ReadFile(*landedF)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(b, &landed); err != nil {
			return fmt.Errorf("%s: %w", *landedF, err)
		}
	}
	var turns []turn
	var says []say
	cards := map[string]card{}
	for _, r := range strings.Split(*rooms, ",") {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		up, err := newest(*src, "usage-"+r+"-*.jsonl")
		if err != nil {
			return err
		}
		ts, err := readTurns(up, r)
		if err != nil {
			return err
		}
		turns = append(turns, ts...)
		tp, err := newest(*src, "tasks-"+r+"-*.json")
		if err != nil {
			return err
		}
		if err := readCards(tp, r, cards); err != nil {
			return err
		}
		ss, err := fetchSays(*hub, r, since)
		if err != nil {
			return err
		}
		says = append(says, ss...)
		step(out, "read %s: %d turns, %d says", r, len(ts), len(ss))
	}
	turns = inWindow(turns, since, until)
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return err
	}
	sayRows := matchSays(says, turns)
	tokRows, all := tokenRows(turns, cards)
	cRows := cardRows(cards, turns, since, landed)
	for _, w := range []struct {
		file   string
		header []string
		rows   [][]string
	}{
		{"says-" + suffix + ".csv", saysHeader, sayRows},
		{"tokens-" + suffix + ".csv", tokensHeader, tokRows},
		{"cards-" + suffix + ".csv", cardsHeader, cRows},
	} {
		if err := writeCSV(filepath.Join(*dir, w.file), w.header, w.rows); err != nil {
			return err
		}
		step(out, "wrote %s (%d rows)", w.file, len(w.rows))
	}
	started := 0
	for _, r := range sayRows {
		if r[9] == "true" {
			started++
		}
	}
	md := summary(since, until, len(sayRows), started, len(tokRows)-1, len(cRows), all)
	if err := os.WriteFile(filepath.Join(*dir, "summary-"+suffix+".md"), []byte(md), 0o644); err != nil {
		return err
	}
	step(out, "wrote summary-%s.md", suffix)
	return nil
}
