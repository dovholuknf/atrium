package gitsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The rules of a push, design 3.2. A push is refused whole when any updated ref breaks one.

// adoptedSentence is the refusal of a push to what a git_repos mirror the operator took into the store keeps in step
// with its checkout: its integration branch, main, a tag or a ref outside refs/heads. NOBODY PUSHES THOSE, the
// operator included: the mirror pass force-fetches the checkout over them. A work branch lands (mirrorRefuses).
func adoptedSentence(name string) string {
	return name + " is a mirror the hub keeps in step with a checkout, so its integration branch, main and tags cannot be pushed. push your work under another branch name"
}

// mirrorRefuses says why one ref of a push to an adopted mirror cannot land, or "". A CARD'S BRANCH LANDS: the
// mirror pass force-fetches the integration branch and nothing else, and a room's sync fetches only that branch, so
// a work branch beside it is never written over and never fetched by accident. What the pass owns (the integration
// branch, and main and claude/main whatever the entry names), a tag and anything outside refs/heads are refused, the
// operator included. `branch` is the git_repos entry's branch, "" when the entry is no longer configured.
func mirrorRefuses(name, ref, branch string) string {
	if branch == "" {
		branch = IntegrationBranch
	}
	if !strings.HasPrefix(ref, headsPrefix) || isMainRef(ref) || ref == headsPrefix+branch {
		return adoptedSentence(name)
	}
	return ""
}

func trimHead(ref string) string { return strings.TrimPrefix(ref, headsPrefix) }

// release is a branch whose owner is let go as the push is taken.
type release struct {
	ref, by string
}

// decision is what the rules made of a push.
type decision struct {
	// bad is the sentence for each ref that broke a rule, and order the refs in the order they were found.
	bad      map[string]string
	order    []string
	releases []release
}

func (d *decision) fail(ref, why string) {
	if _, ok := d.bad[ref]; !ok {
		d.bad[ref] = why
		d.order = append(d.order, ref)
	}
}

// sentence is the line the user sees after `remote: atrium: `.
func (d *decision) sentence() string {
	var parts []string
	for _, ref := range d.order {
		parts = append(parts, d.bad[ref])
	}
	if len(parts) == 1 {
		return parts[0] + ". nothing was pushed"
	}
	return strings.Join(parts, ". ") + ". nothing was pushed"
}

// ownerVerdict is what an owner's room said about the owner's card.
type ownerVerdict struct {
	released bool
	by       string
	err      error
}

// ownerKey names an owner in the map of questions already put.
func ownerKey(room, card string) string { return room + "\x00" + card }

// inherits says whether the pusher is the owner or its successor: the same room, and the owner is the
// pusher's card or one of its `moved_to` predecessors. THE CHAIN IS ONLY HONOURED AGAINST AN OWNER ON THE
// PUSHER'S OWN ROOM, whose name came from the link certificate. A chain is what the room's forwarder said,
// and the hub trusts a room for its own cards and no others.
func inherits(p Pusher, room, card string) bool {
	if p.Operator || p.Room != room {
		return false
	}
	for _, c := range p.Chain {
		if c == card {
			return true
		}
	}
	return false
}

// What one push may ask of the link, in all: how many owners' rooms, and for how long. Past either, the owners
// not asked are treated as unreachable, which keeps their branches owned and says so. Variables for the tests.
var (
	maxOwnerAsks  = 8
	ownerAskTotal = 10 * time.Second
)

// askOwners puts the one question a push may need answered, BEFORE the lock: for each branch the push
// updates that is owned by another card, is that card finished? One push asks at most maxOwnerAsks rooms
// and for ownerAskTotal, so it cannot hold a link connection for long.
func (rc *Receiver) askOwners(ctx context.Context, repo string, p Pusher, req pushRequest) map[string]ownerVerdict {
	asked := map[string]ownerVerdict{}
	if p.Operator {
		return asked
	}
	ctx, cancel := context.WithTimeout(ctx, ownerAskTotal)
	defer cancel()
	count := 0
	for _, u := range req.Updates {
		if !strings.HasPrefix(u.Ref, headsPrefix) || isMainRef(u.Ref) || checkRefName(u.Ref) != "" {
			continue
		}
		room, card, ok, err := rc.h.PushLog.Owner(ctx, repo, u.Ref)
		if err != nil || !ok || room == "" || inherits(p, room, card) || (p.Room == room && p.Card == card) {
			continue
		}
		key := ownerKey(room, card)
		if _, done := asked[key]; done {
			continue
		}
		if count >= maxOwnerAsks || ctx.Err() != nil {
			asked[key] = ownerVerdict{err: ErrRoomUnreachable}
			continue
		}
		count++
		asked[key] = rc.askCard(ctx, room, card)
	}
	return asked
}

// inNameCheck, when set, runs before each `git check-ref-format`. Tests use it to see whether the repository's
// lock is held at that moment.
var inNameCheck func()

// checkNames is the verdict on each ref's NAME, made before the lock: the name does not depend on anything the lock
// holds, and `git check-ref-format` is a process per ref. A ref that is fine has no entry.
func (rc *Receiver) checkNames(ctx context.Context, req pushRequest) map[string]string {
	bad := map[string]string{}
	seen := map[string]bool{}
	for _, u := range req.Updates {
		if seen[u.Ref] {
			continue
		}
		seen[u.Ref] = true
		label := u.Ref
		if strings.HasPrefix(u.Ref, headsPrefix) {
			label = trimHead(u.Ref)
		}
		if why := checkRefName(u.Ref); why != "" {
			bad[u.Ref] = why
			continue
		}
		if inNameCheck != nil {
			inNameCheck()
		}
		if _, err := rc.h.Store().git(ctx, "", "check-ref-format", u.Ref); err != nil {
			bad[u.Ref] = "git does not take " + label + " as a ref name"
		}
	}
	return bad
}

func (rc *Receiver) askCard(ctx context.Context, room, card string) ownerVerdict {
	if rc.h.Cards == nil {
		return ownerVerdict{err: ErrRoomUnreachable}
	}
	ctx, cancel := context.WithTimeout(ctx, cardAsk)
	defer cancel()
	st, err := rc.h.Cards(ctx, room, card)
	if rel, by := released(st, err); rel {
		return ownerVerdict{released: true, by: by}
	}
	if err != nil && !errors.Is(err, ErrCardGone) && !errors.Is(err, ErrRoomUnreachable) {
		err = ErrRoomUnreachable
	}
	return ownerVerdict{err: err}
}

// decide applies the rules to every update of a push, with the repository's lock held. `cur` is the
// repository's branches and tags as they are now, and nothing in it is trusted from the client.
func (rc *Receiver) decide(ctx context.Context, repo string, p Pusher, req pushRequest, cur map[string]string,
	names map[string]string, asked map[string]ownerVerdict) decision {

	d := decision{bad: map[string]string{}}
	inPush := map[string]string{}
	for _, u := range req.Updates {
		ref := u.Ref
		label := ref
		if strings.HasPrefix(ref, headsPrefix) {
			label = trimHead(ref)
		}
		if why := names[ref]; why != "" {
			d.fail(ref, why)
			continue
		}
		if _, dup := inPush[foldedRef(ref)]; dup {
			d.fail(ref, label+" is named twice in this push")
			continue
		}
		inPush[foldedRef(ref)] = ref

		have := cur[ref]
		switch {
		case u.New == zeroSHA:
			d.fail(ref, "deleting "+label+" is not done by a push")
			continue
		case strings.HasPrefix(ref, tagsPrefix):
			switch {
			case !p.Operator:
				d.fail(ref, "a card cannot push a tag. "+label+" is a tag")
			case have != "":
				d.fail(ref, "the tag "+strings.TrimPrefix(ref, tagsPrefix)+" is already there, and a tag is not moved")
			}
			if _, bad := d.bad[ref]; bad {
				continue
			}
		case isMainRef(ref) && !p.Operator:
			d.fail(ref, "only the operator moves "+label+". push your work under another branch name")
			continue
		}
		// What the client thinks the ref is has to be what it is.
		if u.Old != zeroSHA && have != u.Old || u.Old == zeroSHA && have != "" {
			d.fail(ref, label+" is not where your push expected it. someone moved it, or it is not there. fetch and push again")
			continue
		}
		if !strings.HasPrefix(ref, headsPrefix) || isMainRef(ref) || p.Operator {
			continue
		}
		rc.checkOwner(ctx, &d, repo, p, ref, label, asked)
	}

	// A NEW REF MAY NOT BE A CASE VARIANT OF, OR A DIRECTORY/FILE CLASH WITH, ANOTHER: `Fix/x` and `fix/x` are
	// one file on sg4's NTFS. Against the repository and against the rest of this push.
	for _, u := range req.Updates {
		if _, bad := d.bad[u.Ref]; bad || cur[u.Ref] != "" {
			continue
		}
		f := foldedRef(u.Ref)
		clash := func(other string) bool {
			of := foldedRef(other)
			return other != u.Ref && (of == f || dirFileConflict(of, f))
		}
		for have := range cur {
			if clash(have) {
				d.fail(u.Ref, collisionSentence(u.Ref, have))
				break
			}
		}
		if _, bad := d.bad[u.Ref]; bad {
			continue
		}
		for _, v := range req.Updates {
			if v.Ref != u.Ref && clash(v.Ref) && cur[v.Ref] == "" {
				d.fail(u.Ref, collisionSentence(u.Ref, v.Ref))
				break
			}
		}
	}
	return d
}

func collisionSentence(ref, other string) string {
	label := func(r string) string {
		if strings.HasPrefix(r, headsPrefix) {
			return trimHead(r)
		}
		return strings.TrimPrefix(r, tagsPrefix)
	}
	if foldedRef(ref) == foldedRef(other) {
		return label(ref) + " differs from " + label(other) + " only in case, and a disk that ignores case would make them one branch"
	}
	return label(ref) + " clashes with " + label(other) + ", one would be a directory the other is a file in"
}

// checkOwner applies the ownership rule to one branch a card pushes.
func (rc *Receiver) checkOwner(ctx context.Context, d *decision, repo string, p Pusher, ref, label string,
	asked map[string]ownerVerdict) {

	room, card, ok, err := rc.h.PushLog.Owner(ctx, repo, ref)
	if err != nil {
		d.fail(ref, "the hub could not read who owns "+label)
		return
	}
	if !ok || (room == p.Room && card == p.Card) {
		return
	}
	if room == "" {
		d.fail(ref, label+" is owned by the operator, fetch it and push under another name")
		return
	}
	if inherits(p, room, card) {
		// The successor takes the branch over, so the log says the new owner and a later card is asked about
		// the successor and not the card that moved on.
		d.releases = append(d.releases, release{ref: ref, by: "moved to " + p.Card})
		return
	}
	owned := fmt.Sprintf("%s is owned by %s's %s, fetch it and push under another name", label, room, card)
	v, was := asked[ownerKey(room, card)]
	switch {
	case !was:
		d.fail(ref, owned+" (who owns it changed while this push arrived, try again)")
	case v.released:
		d.releases = append(d.releases, release{ref: ref, by: v.by})
	case v.err != nil && !errors.Is(v.err, ErrCardGone):
		d.fail(ref, owned+fmt.Sprintf(" (the hub cannot reach %s to ask whether that card is finished)", room))
	default:
		d.fail(ref, owned)
	}
}

// ── the operator lets a branch go ───────────────────────

// ReleaseBranch ends the ownership of a branch of a store repository, at the operator's word. It answers a
// sentence for the operator. The ref is refs/heads/<branch> and never anything else.
func (h *Hub) ReleaseBranch(ctx context.Context, repo, branch string) (string, error) {
	ref, err := ParseName(repo)
	if err != nil {
		return "", err
	}
	full := headsPrefix + strings.TrimPrefix(branch, headsPrefix)
	if why := checkRefName(full); why != "" || isMainRef(full) {
		return "", refuse("that is not a branch that can be released")
	}
	if h.PushLog == nil {
		return "", errors.New("this hub keeps no push log")
	}
	s := h.Store()
	if !s.Exists(ref.Name()) {
		return "", refuse("%s is not in the hub's store", ref.Name())
	}
	l := h.lock("store:" + strings.ToLower(ref.Name()))
	l.Lock()
	defer l.Unlock()
	room, card, ok, err := h.PushLog.Owner(ctx, ref.Name(), full)
	if err != nil {
		return "", err
	}
	if !ok {
		return trimHead(full) + " has no owner, so there is nothing to release", nil
	}
	if err := h.PushLog.Release(ctx, ref.Name(), full, "operator"); err != nil {
		return "", err
	}
	h.audit("", "git-hub-branch-released", fmt.Sprintf("%s %s by=operator", ref.Name(), full))
	who := "the operator"
	if room != "" {
		who = room + "'s " + card
	}
	return trimHead(full) + " was owned by " + who + " and is released. the next card to push a fast-forward owns it", nil
}

// removeCreated takes away a repository a push made and that took nothing, and the owner and host
// directories it left empty, which would make a later differently cased name collide with them.
func (rc *Receiver) removeCreated(dir string) {
	_ = os.RemoveAll(dir)
	gl := rc.h.lock(storeWideLock)
	gl.Lock()
	defer gl.Unlock()
	_ = os.Remove(filepath.Dir(dir))
	_ = os.Remove(filepath.Dir(filepath.Dir(dir)))
}
