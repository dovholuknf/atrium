package link

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/rules"
)

// OPEN A LINK, ON THE HUB. Design: docs/rnd/card-lifecycle-design.md, section 3. Item r-open-verb-pr.
//
// `POST /v1/open {url}` is one path on a hub and on a room, so every door (the launch dialog, ctrl-alt-r, the pulls
// tab, `atrium open`) calls the same thing wherever it is pointed. The room does the work (internal/api/open.go). The
// hub only decides which room:
//
//   - a room the caller named, by header or query, is where it goes.
//   - otherwise the link is matched against the hub's own recogniser table, and a pull request some room already
//     claimed goes to that room, which answers its live card rather than making a second one. An issue, a branch and a
//     support link are claimed by their key the same way.
//   - otherwise the least busy room, as a paste on the pulls tab is placed. That room claims the key for itself.
//
// The answer comes back with `room` set and `card` and `pr` tagged, so the board can attach the card wherever it is.

// openRoomKey carries the room an open went to, for retagOpen.
type openRoomKey struct{}

// placeOpen is the hub's placement of `POST /v1/open`.
func (p *Proxy) placeOpen(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/open" {
		return r, true
	}
	if room, named := p.roomFor(r); named || room != "" {
		return r.WithContext(context.WithValue(r.Context(), openRoomKey{}, room)), true
	}
	key, repo := p.openTarget(r)
	if key != "" {
		if st := p.prClaims(); st != nil {
			if c, err := st.PRClaimOf(key); err == nil {
				r = p.placedOn(w, r, c.Room)
				return r.WithContext(context.WithValue(r.Context(), openRoomKey{}, c.Room)), true
			}
		}
	}
	// The least busy room, one that already holds the repo ahead of one that would have to clone it. One too old to
	// read the hub's rows hands it on to the next. See retryDeaf.
	room := p.placeRoomHolding(r.Context(), "", nil, repo)
	if room == "" {
		return r, true
	}
	r = p.placePaste(r, room)
	return r.WithContext(context.WithValue(r.Context(), openRoomKey{}, room)), true
}

// openKey is the claim key of a pasted link, read from the hub's recogniser table, or "" when the hub has no table,
// nothing matches, or the link is one no room opens. The body is put back for the room.
func (p *Proxy) openKey(r *http.Request) string {
	key, _ := p.openTarget(r)
	return key
}

// openTarget reads a pasted link against the hub's recogniser table: the claim key of a pull request, issue, branch or
// support link (empty when it is none of them), and the
// repo ("host/org/repo") the link opens in when it has one, so placement can prefer a room that holds it. A link that
// names no repo opens in the request's `repo`, else the row's default repo, and "none" is a scratch folder (no repo).
// Both are "" when the hub has no table or nothing matches. The body is put back for the room.
func (p *Proxy) openTarget(r *http.Request) (key, repo string) {
	if r.Body == nil {
		return "", ""
	}
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(raw), r.Body))
	var in struct {
		URL  string `json:"url"`
		Repo string `json:"repo"`
	}
	if json.Unmarshal(raw, &in) != nil || strings.TrimSpace(in.URL) == "" {
		return "", ""
	}
	return p.openTargetOf(in.URL, in.Repo)
}

// openTargetOf is openTarget for a link and the request's `repo` already in hand.
func (p *Proxy) openTargetOf(link, pickedRepo string) (key, repo string) {
	var in struct{ URL, Repo string }
	in.URL, in.Repo = link, pickedRepo
	f := p.forgeSide()
	if f == nil {
		return "", ""
	}
	rows, err := hubRecogniserRows(f.st, f.seedRows())
	if err != nil {
		return "", ""
	}
	row, vars, err := rules.MatchRecogniserIn(rows, in.URL)
	if err != nil {
		return "", ""
	}
	host, org, name := strings.ToLower(strings.TrimSpace(vars["host"])), strings.TrimSpace(vars["org"]), strings.TrimSpace(vars["repo"])
	if host != "" && org != "" && name != "" {
		repo = host + "/" + org + "/" + name
	} else if pick := strings.Trim(strings.TrimSpace(in.Repo), "/"); pick != "" {
		repo = pick
	} else {
		repo = strings.Trim(strings.TrimSpace(row.DefaultRepo), "/")
	}
	if strings.EqualFold(repo, "none") || !rules.RepoSlug(repo) {
		repo = ""
	}
	if !rules.IsPRRow(row.Tags) {
		// An issue, a branch or a support link is keyed as the room keys it, and the room claims it on the first open.
		// A link the room could not open has no key, and goes to the least busy room.
		return strings.ToLower(rules.ClassifyLink(row.Fill(vars)).Key(row.Kind)), repo
	}
	num, _ := strconv.Atoi(strings.TrimSpace(vars["num"]))
	if host == "" || org == "" || name == "" || num <= 0 {
		return "", repo
	}
	return rules.PRKey(vars["host"], vars["org"], vars["repo"], num), repo
}

// retagOpen puts the room on an open's answer, and on its card and review ids, in the merged view. A scoped board
// asked one room and wants that room's own ids.
func (p *Proxy) retagOpen(res *http.Response, room string) error {
	// A refusal because another room holds the link names that room's card, which only means something tagged.
	held := res.StatusCode == http.StatusConflict
	if res.Request == nil || (res.StatusCode >= 300 && !held) {
		return nil
	}
	if room == "" {
		return nil
	}
	if _, merged := p.merged(); !merged {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	res.Body.Close()
	if err != nil {
		return err
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		res.Body = io.NopCloser(bytes.NewReader(raw))
		return nil
	}
	// A link whose holder has a card live on it answers that card, which is on the holder's room, not the one asked.
	if live, _ := obj["live"].(bool); live {
		if owner, _ := obj["held_by"].(string); owner != "" {
			room = owner
		}
	}
	if held {
		if code, _ := obj["code"].(string); code != "held" {
			res.Body = io.NopCloser(bytes.NewReader(raw))
			return nil
		}
		if owner, _ := obj["held_by"].(string); owner != "" {
			room = owner
		}
		if id, ok := obj["card"].(string); ok && id != "" {
			obj["card"] = tagFor(room, id)
		}
		out, err := json.Marshal(obj)
		if err != nil {
			res.Body = io.NopCloser(bytes.NewReader(raw))
			return nil
		}
		res.Body = io.NopCloser(bytes.NewReader(out))
		res.ContentLength = int64(len(out))
		res.Header.Set("Content-Length", fmt.Sprint(len(out)))
		return nil
	}
	obj["room"] = room
	for _, field := range []string{"card", "pr"} {
		if id, ok := obj[field].(string); ok && id != "" {
			obj[field] = tagFor(room, id)
		}
	}
	out, err := json.Marshal(obj)
	if err != nil {
		res.Body = io.NopCloser(bytes.NewReader(raw))
		return nil
	}
	res.Body = io.NopCloser(bytes.NewReader(out))
	res.ContentLength = int64(len(out))
	res.Header.Set("Content-Length", fmt.Sprint(len(out)))
	return nil
}
