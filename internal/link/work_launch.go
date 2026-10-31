package link

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/ctl"
)

// EVERY LAUNCH NAMES AN ITEM. Item lifecycle L1: work is named in the backlog, and a card launched on it is attached to
// it, so nothing runs without one. This file is the part every launcher shares (the hub's atrium_launch, a room's
// stdio one, and `atrium launch`): what the item argument may say, the refusal that names it, and the lookup that makes
// sure it exists. See docs/rnd/item-lifecycle-design.md, section 8.

// WorkTagPrefix starts the tag that attaches a card to an item, `atrium:item:<backlog id>`. NOT ItemTag, which is
// whole-string and means isolated. The same prefix as daemon.WorkTagPrefix.
const WorkTagPrefix = "atrium:item:"

// StepTagPrefix starts the tag that says which step of its item a card is for, `atrium:step:<step>`. The same prefix as
// daemon.StepTagPrefix. The room carries it on the card's first push and the hub files it on the attachment.
const StepTagPrefix = "atrium:step:"

// WithStepTag puts the step tag on a launch's tags, replacing one that is there. A step that is not one of the eight is
// refused, naming them, before a card exists.
func WithStepTag(tags []string, step string) ([]string, error) {
	step = strings.ToLower(strings.TrimSpace(step))
	if !hubstore.ValidStep(step) {
		return nil, &ctl.RefusedError{Msg: fmt.Sprintf("--step %q is not a step: %s", step, strings.Join(hubstore.Steps, ", "))}
	}
	out := make([]string, 0, len(tags)+1)
	for _, t := range tags {
		if !strings.HasPrefix(strings.TrimSpace(t), StepTagPrefix) {
			out = append(out, t)
		}
	}
	return append(out, StepTagPrefix+step), nil
}

// NewItemPrefix is how a launch files the backlog item it runs on: `new:a title`.
const NewItemPrefix = "new:"

// ItemRequired is the refusal for a launch that names no item. It names the flag, in the words of each door.
const ItemRequired = "a launch names the item it works on. pass --item <backlog id> (the `item` argument of atrium_launch), " +
	"or --item new:\"a title\" to file one as you launch. work is named in the backlog, so find the id with atrium_backlog list"

// WorkOfTags is the backlog id a launch's tags attach it to, or "".
func WorkOfTags(tags []string) string {
	for _, t := range tags {
		if id, ok := strings.CutPrefix(strings.TrimSpace(t), WorkTagPrefix); ok && backlogIDRe.MatchString(id) {
			return id
		}
	}
	return ""
}

// WithWorkTag puts the attachment tag on a launch's tags, unless one is there.
func WithWorkTag(tags []string, id string) []string {
	if id == "" || WorkOfTags(tags) != "" {
		return tags
	}
	return append(append([]string{}, tags...), WorkTagPrefix+id)
}

// ItemSpec is what an item argument says: an id of a backlog item, or a title to file one under.
type ItemSpec struct {
	ID    string
	Title string
}

// ParseItemSpec reads an item argument. Empty is refused with ItemRequired, and an id that is not one is refused with
// what an id looks like.
func ParseItemSpec(spec string) (ItemSpec, error) {
	spec = strings.TrimSpace(spec)
	switch {
	case spec == "":
		return ItemSpec{}, &ctl.RefusedError{Msg: ItemRequired}
	case strings.HasPrefix(strings.ToLower(spec), NewItemPrefix):
		title := strings.TrimSpace(spec[len(NewItemPrefix):])
		if title == "" {
			return ItemSpec{}, &ctl.RefusedError{Msg: "--item new: needs a title: --item new:\"what the work is\""}
		}
		return ItemSpec{Title: title}, nil
	case !backlogIDRe.MatchString(spec):
		return ItemSpec{}, &ctl.RefusedError{Msg: fmt.Sprintf("--item %q is not a backlog id: lower case letters, digits, dot, dash "+
			"and underscore, like f-new-thing or r-037. to file a new one say --item new:\"a title\"", spec)}
	}
	return ItemSpec{ID: spec}, nil
}

// HubCall is one call to the hub's own endpoints, however the caller reaches them.
type HubCall func(ctx context.Context, method, path string, body, out any) error

// statusErr is an error that carries the HTTP status the hub answered with.
type statusErr interface {
	HTTPStatus() int
	Bare() bool
}

// DeptOfTags is the department a card's tags file it under, or "".
func DeptOfTags(tags []string) string {
	for _, t := range tags {
		if t = strings.TrimSpace(t); len(t) > len(DeptTagPrefix) && strings.EqualFold(t[:len(DeptTagPrefix)], DeptTagPrefix) {
			return strings.ToLower(strings.TrimSpace(t[len(DeptTagPrefix):]))
		}
	}
	return ""
}

// ResolveItem turns an item argument into the backlog id a card is attached to.
//
//   - An id must be a backlog item the hub holds. A hub that answers "no such item" refuses the launch. A hub that does
//     not answer does NOT: a launch is not refused for a hub being down, and the attach waits in the room's outbox.
//   - `new:<title>` files the backlog item first, under dept (the launcher's), and attaches to it. Filing needs the hub,
//     so a hub that is not answering refuses it.
func ResolveItem(ctx context.Context, call HubCall, spec, dept, by, room string) (id string, filed bool, err error) {
	s, err := ParseItemSpec(spec)
	if err != nil {
		return "", false, err
	}
	if s.ID != "" {
		var b hubstore.BacklogItem
		err := call(ctx, http.MethodGet, "/_hub/backlog/"+url.PathEscape(s.ID), nil, &b)
		var se statusErr
		if err != nil && errors.As(err, &se) && se.HTTPStatus() == http.StatusNotFound && !se.Bare() {
			return "", false, &ctl.RefusedError{Msg: fmt.Sprintf("there is no backlog item %q. file it first with atrium_backlog, "+
				"or say --item new:\"a title\" to file one as you launch", s.ID)}
		}
		return s.ID, false, nil
	}
	if strings.TrimSpace(dept) == "" {
		return "", false, &ctl.RefusedError{Msg: "--item new: files the item under your department, and there is none to read: " +
			"launch from a card tagged dept:<name>, or file it with atrium_backlog first and pass its id"}
	}
	var b hubstore.BacklogItem
	if err := call(ctx, http.MethodPost, "/_hub/backlog", map[string]any{
		"dept": dept, "title": s.Title, "by": by, "room": room}, &b); err != nil {
		return "", false, &ctl.RefusedError{Msg: "could not file the backlog item for --item new: " + err.Error()}
	}
	return b.ID, true, nil
}
