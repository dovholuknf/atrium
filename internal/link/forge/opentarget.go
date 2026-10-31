package forge

import (
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/rules"
)

// OpenTargetOf is openTarget for a link and the request's `repo` already in hand.
func (p *Service) OpenTargetOf(link, pickedRepo string) (key, repo string) {
	var in struct{ URL, Repo string }
	in.URL, in.Repo = link, pickedRepo
	f := p.ForgeSide()
	if f == nil {
		return "", ""
	}
	rows, err := HubRecogniserRows(f.st, f.seedRows())
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
