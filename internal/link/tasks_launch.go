package link

import "strconv"

type taskBlock struct {
	BacklogID string   `json:"backlog_id"`
	TaskID    string   `json:"task_id"`
	Attempt   int      `json:"attempt"`
	Kind      string   `json:"kind"`
	Title     string   `json:"title"`
	Brief     string   `json:"brief"`
	Outputs   []string `json:"outputs"`
	ItemTitle string   `json:"item_title"`
	Context   string   `json:"context,omitempty"`
	Learnings []string `json:"learnings,omitempty"`
}

// dir is the launch's stand-in for a directory in the launch key: a task has none until the room makes it.
func (b *taskBlock) dir() string {
	return "task:" + b.BacklogID + ":" + b.TaskID + ":" + strconv.Itoa(b.Attempt)
}
