package daemon

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/store"
)

// A CLOSE'S HUB BRANCHES: a claude/* branch a card pushed (GitPush) is an inventory row, and closing the card deletes it
// on the hub with a push from the card's worktree. The hub takes that delete only for a branch its push log says this
// card pushed (gitsync/receive_rules.go). The push is made with a token minted for the close, as a stash's is, so a card
// whose session has ended can still be closed. The close itself is internal/api/close.go.

// recordHubBranch writes the inventory row for a branch a card pushed to the hub. Best effort: a push that landed is
// not undone by a row that did not write. Only a claude/* branch other than claude/main is a row.
func (d *Daemon) recordHubBranch(card, branch, hubRepo string) {
	if !strings.HasPrefix(branch, "claude/") || branch == "claude/main" {
		return
	}
	rows, err := d.st.Resources(card)
	if err == nil {
		for _, r := range rows {
			if r.Live() && r.Kind == store.ResHubBranch && r.Ref == branch && r.Detail == hubRepo {
				return
			}
		}
	}
	if _, err := d.st.AddResource(card, store.ResHubBranch, branch, hubRepo); err != nil {
		log.Printf("[atrium daemon] the hub branch %s of %s was not recorded: %v", branch, card, err)
	}
}

// HubDeleteBranch is api.Server.HubDelete.
func (d *Daemon) HubDeleteBranch(ctx context.Context, card, dir, hubRepo, branch string) error {
	if d.st.GitPush() != store.GitPushHub {
		return fmt.Errorf("this room does not let cards push to the hub (git.push is none)")
	}
	base := d.HubRemoteBase()
	if base == "" || d.HubGit.Cards == nil {
		return fmt.Errorf("this room has no hub remote to delete from")
	}
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("the card has no directory to push the delete from")
	}
	d.HubGit.Stashing.Store(card, true)
	defer d.HubGit.Stashing.Delete(card)
	tok, err := d.HubGit.Cards.Mint(card)
	if err != nil {
		return err
	}
	defer d.RevokeHubGit(card, time.Time{})
	report, err := gitsync.DeleteFromHub(ctx, gitsync.Default, filepath.FromSlash(dir), base, tok, branch, hubRepo)
	if err != nil {
		if strings.TrimSpace(report) != "" {
			return fmt.Errorf("the hub did not delete it:\n%s", report)
		}
		return err
	}
	return nil
}
