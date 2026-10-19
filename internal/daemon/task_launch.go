package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/cardroot"
	"github.com/dovholuknf/atrium/internal/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

// A TASK OF AN ITEM, launched by the hub.

// TaskTagPrefix starts the tag that says a card runs a task of an item: `atrium:task:<tid>`.
const TaskTagPrefix = "atrium:task:"

// SettingCardRoot is the room's card root, where every home and task folder goes.
const SettingCardRoot = cardroot.Setting

var taskIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,30}$`)

type taskSpec struct {
	BacklogID string   `json:"backlog_id"`
	TaskID    string   `json:"task_id"`
	Attempt   int      `json:"attempt"`
	Kind      string   `json:"kind"`
	Title     string   `json:"title"`
	Brief     string   `json:"brief"`
	Outputs   []string `json:"outputs"`
	ItemTitle string   `json:"item_title"`
	Context   string   `json:"context"`
	Learnings []string `json:"learnings"`
}

type taskLaunchBody struct {
	LaunchRequest
	Task *taskSpec `json:"task"`
}

// taskOfTags is the task id a card's tags name, or "".
func taskOfTags(tags []string) string {
	for _, t := range tags {
		if id, ok := strings.CutPrefix(strings.TrimSpace(t), TaskTagPrefix); ok && taskIDRe.MatchString(id) {
			return id
		}
	}
	return ""
}

func isTaskCard(t *store.Task) bool { return t != nil && taskOfTags(t.Tags) != "" }

func (d *Daemon) taskLaunchFromJSON(body []byte) (*store.Task, error) {
	var in taskLaunchBody
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, err
	}
	spec := in.Task
	switch {
	case spec == nil:
		return nil, errors.New("a task launch carries the task")
	case !workIDRe.MatchString(spec.BacklogID):
		return nil, fmt.Errorf("item %q is not a backlog id", spec.BacklogID)
	case !taskIDRe.MatchString(spec.TaskID):
		return nil, fmt.Errorf("task %q is not a task id", spec.TaskID)
	case spec.Attempt < 1:
		return nil, errors.New("a task launch carries its attempt")
	}
	req := in.LaunchRequest
	req.Interactive = true
	req.SpawnedBy = launcherFor(req)
	if err := d.launchWhileStopping(); err != nil {
		return nil, err
	}
	if err := d.launchHeld(req); err != nil {
		return nil, err
	}
	if err := d.launchNeedsLogin(req); err != nil {
		return nil, err
	}
	folder, err := d.taskFolder(spec)
	if err != nil {
		return nil, err
	}
	for _, o := range spec.Outputs {
		if _, err := safepath.Contained(folder, o); err != nil {
			return nil, fmt.Errorf("output %q is not a path inside the task folder", o)
		}
	}
	lean := true
	req.Cwd, req.Scratch, req.Item, req.Isolated, req.Lean = folder, true, true, true, &lean
	req.WorkItem = spec.BacklogID
	if taskOfTags(req.Tags) == "" {
		req.Tags = append(append([]string{}, req.Tags...), TaskTagPrefix+spec.TaskID)
	}
	req.Brief = taskBrief(spec, folder)
	task, err := d.Launch(req)
	if err != nil {
		if rows, _ := d.st.CardHomesOf(spec.BacklogID, spec.TaskID); len(rows) > 0 {
			if last := rows[len(rows)-1]; last.Card == "" && last.Attempt == spec.Attempt && last.State == store.CardHomeLive {
				_ = os.Remove(filepath.FromSlash(last.Path))
				_ = d.st.CardHomeSet(last.Path, store.CardHomeRemoved, "the launch was refused", "")
			}
		}
		return nil, err
	}
	if err := d.st.CardHomeCard(filepath.ToSlash(folder), task.ID); err != nil {
		return nil, err
	}
	return task, nil
}

// taskFolder is the folder this attempt runs in: the one the ledger already has for it (a replayed launch), else a new one.
func (d *Daemon) taskFolder(spec *taskSpec) (string, error) {
	rows, err := d.st.CardHomesOf(spec.BacklogID, spec.TaskID)
	if err != nil {
		return "", err
	}
	for _, r := range rows {
		if r.Attempt == spec.Attempt && r.State == store.CardHomeLive {
			if _, err := os.Stat(filepath.FromSlash(r.Path)); err == nil {
				return filepath.FromSlash(r.Path), nil
			}
		}
	}
	root, err := d.cardRoot()
	if err != nil {
		return "", err
	}
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	folder := filepath.Join(root, spec.BacklogID+"-"+spec.TaskID+"-a"+strconv.Itoa(spec.Attempt)+"-"+hex.EncodeToString(b))
	if err := d.st.CardHomeAdd(filepath.ToSlash(folder), spec.BacklogID, spec.TaskID, spec.Attempt, spec.Outputs); err != nil {
		return "", err
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		_ = d.st.CardHomeSet(filepath.ToSlash(folder), store.CardHomeRemoved, "the folder could not be made", "")
		return "", fmt.Errorf("could not make the task folder: %v", err)
	}
	return folder, nil
}

// taskBrief is BRIEF.md for a task: what to do, what the item is, where to work and what to leave, and how to end.
func taskBrief(s *taskSpec, folder string) string {
	var b strings.Builder
	b.WriteString("# Task " + s.TaskID + " of " + s.BacklogID + ": " + s.Title + "\n\n")
	if s.ItemTitle != "" {
		b.WriteString("Item: " + s.ItemTitle + "\n\n")
	}
	if c := strings.TrimSpace(s.Context); c != "" {
		b.WriteString("## About the item\n\n" + c + "\n\n")
	}
	b.WriteString("## The task\n\n")
	if t := strings.TrimSpace(s.Brief); t != "" {
		b.WriteString(t + "\n\n")
	} else {
		b.WriteString(s.Title + "\n\n")
	}
	b.WriteString("## Where you work\n\nThis folder is yours and throwaway: " + filepath.ToSlash(folder) + "\n" +
		"It is not a git checkout. Nothing here is kept unless you name it below. When you end, atrium keeps the files " +
		"named here and removes the folder.\n\n")
	if len(s.Outputs) > 0 {
		b.WriteString("## What to leave\n\nWrite these, as paths inside this folder (a folder means every file in it):\n\n")
		for _, o := range s.Outputs {
			b.WriteString("- " + o + "\n")
		}
		b.WriteString("\n")
	}
	if len(s.Learnings) > 0 {
		b.WriteString("## What earlier tasks of this item learned\n\n")
		for _, l := range s.Learnings {
			b.WriteString("- " + strings.Join(strings.Fields(l), " ") + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("## How to end\n\n" +
		"1. Write RECAP.md in this folder: what you found or made, in full.\n" +
		"2. Call atrium_done with artifact `RECAP.md`, `line` (one line, up to 20 words), `why` (up to 1000 bytes: why you " +
		"did it the way you did) and `learned` (one paragraph the next task of this item should know).\n" +
		"3. Stop after the call. If you cannot go on, call atrium_blocked with what you need.\n")
	return b.String()
}
