package link

import "strconv"

// dir is the launch's stand-in for a directory in the launch key: a task has none until the room makes it.
func taskDir(b *taskBlock) string {
	return "task:" + b.BacklogID + ":" + b.TaskID + ":" + strconv.Itoa(b.Attempt)
}
