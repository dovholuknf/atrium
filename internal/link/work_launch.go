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

// NoItemNote is what a launch reply says when no item was attached.
const NoItemNote = "no backlog item was attached. "

// DefaultDept is the department a `new:` item is filed under when the launcher has none.
const DefaultDept = "general"

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

// WithItemTags is WithWorkTag after taking out each tag that is the bare id of a backlog item the hub holds, so an item
// is named by its work tag alone. A bare id the hub does not answer for stays as given.
func WithItemTags(ctx context.Context, call HubCall, tags []string, id string) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		bare := strings.TrimSpace(t)
		if backlogIDRe.MatchString(bare) && (bare == id ||
			call(ctx, http.MethodGet, "/_hub/backlog/"+url.PathEscape(bare), nil, &hubstore.BacklogItem{}) == nil) {
			continue
		}
		out = append(out, t)
	}
	return WithWorkTag(out, id)
}

// ItemSpec is what an item argument says: an id of a backlog item, or a title to file one under.
type ItemSpec struct {
	ID    string
	Title string
	// Rerun is `rerun:<id>`: a launch on an item that already has a result, said on purpose.
	Rerun bool
}

// RerunPrefix is how a launch says it runs an item again that already has a result: `rerun:r-037`.
const RerunPrefix = "rerun:"

// RerunTag rides a rerun's tags, so the hub that re-reads a relayed launch's item from its tags knows it was said.
const RerunTag = "atrium:rerun"

// ParseItemSpec reads an item argument. Empty is no item, and an id that is not one is refused with what an id looks
// like.
func ParseItemSpec(spec string) (ItemSpec, error) {
	spec = strings.TrimSpace(spec)
	switch {
	case strings.HasPrefix(strings.ToLower(spec), RerunPrefix):
		s, err := ParseItemSpec(spec[len(RerunPrefix):])
		if err == nil && s.ID == "" {
			err = &ctl.RefusedError{Msg: "--item rerun: names the id of a backlog item that has a result: --item rerun:r-037"}
		}
		s.Rerun = err == nil
		return s, err
	case spec == "":
		return ItemSpec{}, nil
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
//   - An id whose item already has a result is refused unless it is said as `rerun:<id>`.
//   - An empty spec is no item: it returns "" and no error.
//   - `new:<title>` files the backlog item first, under dept (the launcher's, else DefaultDept), and attaches to it. Filing needs the hub,
//     so a hub that is not answering refuses it.
func ResolveItem(ctx context.Context, call HubCall, spec, dept, by, room string) (id string, filed bool, err error) {
	s, err := ParseItemSpec(spec)
	if err != nil {
		return "", false, err
	}
	if s.ID == "" && s.Title == "" {
		return "", false, nil
	}
	if s.ID != "" {
		var b hubstore.BacklogItem
		err := call(ctx, http.MethodGet, "/_hub/backlog/"+url.PathEscape(s.ID), nil, &b)
		var se statusErr
		if err != nil && errors.As(err, &se) && se.HTTPStatus() == http.StatusNotFound && !se.Bare() {
			return "", false, &ctl.RefusedError{Msg: fmt.Sprintf("there is no backlog item %q. file it first with atrium_backlog, "+
				"or say --item new:\"a title\" to file one as you launch", s.ID)}
		}
		if err == nil && b.Result != "" && !s.Rerun {
			return "", false, &ctl.RefusedError{Msg: fmt.Sprintf("backlog item %s already has a result, from %s at %s: %s. "+
				"read it with atrium_backlog get %s. to launch on it again on purpose say --item %s%s",
				s.ID, b.ResultCard, b.ResultAt, b.Result, s.ID, RerunPrefix, s.ID)}
		}
		return s.ID, false, nil
	}
	if strings.TrimSpace(dept) == "" {
		dept = DefaultDept
	}
	var b hubstore.BacklogItem
	if err := call(ctx, http.MethodPost, "/_hub/backlog", map[string]any{
		"dept": dept, "title": s.Title, "by": by, "room": room}, &b); err != nil {
		return "", false, &ctl.RefusedError{Msg: "could not file the backlog item for --item new: " + err.Error()}
	}
	return b.ID, true, nil
}
