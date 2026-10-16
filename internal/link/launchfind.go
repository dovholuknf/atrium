package link

import "strings"

// LaunchRow is the part of a card the launch recovery reads, so the hub's launch and the stdio
// control server's launch share one rule without sharing a card type.
type LaunchRow struct {
	ID, Worktree, Created string
}

// PickLaunched is the card a launch made in `cwd`, or "" when there is none. A card is the launch's when its
// id was NOT in `known`, the ids listed just before the post, and its worktree is `cwd`.
//
// NO CLOCK IS COMPARED. The hub proxies to rooms on other machines, and created_at is the room's clock.
// Of several new cards the latest created wins, which compares two stamps from the same room.
func PickLaunched(known map[string]bool, cwd string, rows []LaunchRow) string {
	norm := func(p string) string {
		return strings.ToLower(strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(p), "\\", "/"), "/"))
	}
	want := norm(cwd)
	best, bestAt := "", ""
	for _, r := range rows {
		if known[r.ID] || norm(r.Worktree) != want {
			continue
		}
		if best == "" || r.Created > bestAt {
			best, bestAt = r.ID, r.Created
		}
	}
	return best
}

// IDSet is the ids of rows, for PickLaunched's snapshot.
func IDSet(rows []LaunchRow) map[string]bool {
	m := make(map[string]bool, len(rows))
	for _, r := range rows {
		m[r.ID] = true
	}
	return m
}
