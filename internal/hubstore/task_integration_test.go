//go:build integration

package hubstore

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

func taskStore(t *testing.T) *Store {
	t.Helper()
	s := open(t)
	if _, _, err := s.PutItem(onWork("a1", "sg4", "c1", "f-x", 1)); err != nil {
		t.Fatal(err)
	}
	return s
}

func addTask(t *testing.T, s *Store, title string, after ...string) Task {
	t.Helper()
	got, err := s.TaskAdd(TaskInput{BacklogID: "f-x", Title: title, Brief: "do " + title, After: after, Outputs: []string{"out.txt"}})
	if err != nil {
		t.Fatalf("add %q: %v", title, err)
	}
	return got
}

func TestTaskAddNumbersTheTasksOfAnItem(t *testing.T) {
	s := taskStore(t)
	for i, want := range []string{"t1", "t2", "t3"} {
		got := addTask(t, s, "task "+want)
		if got.TaskID != want || got.State != TaskPending || got.Kind != TaskScratch || got.Attempt != 0 {
			t.Fatalf("add %d: %+v", i, got)
		}
	}
	list, _ := s.TaskList("f-x")
	if len(list) != 3 || list[0].TaskID != "t1" || list[2].TaskID != "t3" {
		t.Fatalf("list: %+v", list)
	}
	if _, err := s.TaskAdd(TaskInput{BacklogID: "f-none", Title: "x", Brief: "y"}); !errors.Is(err, ErrNoWork) {
		t.Fatalf("an item with no work row: %v", err)
	}
}

func TestTaskAddRefusesAnAfterNamingAnUnknownTaskOrItself(t *testing.T) {
	s := taskStore(t)
	addTask(t, s, "first")
	for _, after := range [][]string{{"t9"}, {"t2"}} {
		_, err := s.TaskAdd(TaskInput{BacklogID: "f-x", Title: "next", Brief: "b", After: after})
		if err == nil || !IsRefusal(err) || !strings.Contains(err.Error(), after[0]) {
			t.Fatalf("after %v: %v", after, err)
		}
	}
	if got := addTask(t, s, "second", "t1"); got.TaskID != "t2" || len(got.After) != 1 {
		t.Fatalf("after a known task: %+v", got)
	}
}

func TestTaskAddRefusesWhatIsNotATask(t *testing.T) {
	s := taskStore(t)
	for name, in := range map[string]TaskInput{
		"no title":      {Title: " ", Brief: "b"},
		"two lines":     {Title: "a\nb", Brief: "b"},
		"no brief":      {Title: "a", Brief: " "},
		"a long brief":  {Title: "a", Brief: strings.Repeat("x", TaskBriefMax+1)},
		"a kind":        {Title: "a", Brief: "b", Kind: "shell"},
		"an absolute":   {Title: "a", Brief: "b", Outputs: []string{"/etc/passwd"}},
		"a drive":       {Title: "a", Brief: "b", Outputs: []string{"c:x"}},
		"going up":      {Title: "a", Brief: "b", Outputs: []string{"out/../../x"}},
		"many outputs":  {Title: "a", Brief: "b", Outputs: strings.Split(strings.Repeat("a,", TaskOutputsMax+1), ",")},
		"empty outputs": {Title: "a", Brief: "b", Outputs: []string{" "}},
	} {
		in.BacklogID = "f-x"
		if _, err := s.TaskAdd(in); err == nil || !IsRefusal(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if list, _ := s.TaskList("f-x"); len(list) != 0 {
		t.Fatalf("a refused add left a row: %+v", list)
	}
}

func TestTheTaskStateIsChecked(t *testing.T) {
	s := taskStore(t)
	addTask(t, s, "one")
	if _, err := s.db.Exec(`UPDATE work_task SET state = 'done' WHERE task_id = 't1'`); err == nil {
		t.Fatal("a state that is not one was written")
	}
	if _, err := s.db.Exec(`UPDATE work_task SET kind = 'shell' WHERE task_id = 't1'`); err == nil {
		t.Fatal("a kind that is not one was written")
	}
	for _, st := range []string{TaskPending, TaskHeld, TaskRunning, TaskCapturing, TaskCaptured, TaskCaptureFailed,
		TaskFailed, TaskUnconfirmed, TaskCancelled} {
		if _, err := s.db.Exec(`UPDATE work_task SET state = ? WHERE task_id = 't1'`, st); err != nil {
			t.Fatalf("%s: %v", st, err)
		}
	}
}

func TestOnlyAPendingTaskBeginsAndAHeldOneDoesNot(t *testing.T) {
	s := taskStore(t)
	addTask(t, s, "one")
	if moved, _ := s.TaskSetHeld("f-x", "t1", true); !moved {
		t.Fatal("hold")
	}
	if _, began, _ := s.TaskBegin("f-x", "t1", "sg4"); began {
		t.Fatal("a held task began")
	}
	if items, _ := s.TaskItemsWithPending(); len(items) != 0 {
		t.Fatalf("a held task is pending: %v", items)
	}
	s.TaskSetHeld("f-x", "t1", false)
	got, began, err := s.TaskBegin("f-x", "t1", "sg4")
	if err != nil || !began || got.State != TaskRunning || got.Attempt != 1 || got.Room != "sg4" {
		t.Fatalf("begin: %+v %v %v", got, began, err)
	}
	if busy, _ := s.TaskBusy("f-x"); !busy {
		t.Fatal("an item with a running task is not busy")
	}
	if _, began, _ := s.TaskBegin("f-x", "t1", "sg4"); began {
		t.Fatal("a running task began again")
	}
}

func TestAnUnconfirmedLaunchIsResolvedByTheCardThatReportsIn(t *testing.T) {
	s := taskStore(t)
	addTask(t, s, "one")
	begun, _, _ := s.TaskBegin("f-x", "t1", "sg4")
	if err := s.TaskLaunched("f-x", "t1", "", true, "lost"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.TaskGet("f-x", "t1"); got.State != TaskUnconfirmed {
		t.Fatalf("state: %+v", got)
	}
	if busy, _ := s.TaskBusy("f-x"); !busy {
		t.Fatal("an unconfirmed task leaves the item free for another launch")
	}
	if moved, _ := s.TaskCardSeen("f-x", "sg5", "c9", begun.StartedAt); moved {
		t.Fatal("a card on another room took the task")
	}
	if moved, _ := s.TaskCardSeen("f-x", "sg4", "c9", begun.StartedAt); !moved {
		t.Fatal("the card that reported in did not take the task")
	}
	if got, _ := s.TaskGet("f-x", "t1"); got.State != TaskRunning || got.Card != "sg4~c9" {
		t.Fatalf("after: %+v", got)
	}
}

func captured(attempt int) TaskEvent {
	return TaskEvent{BacklogID: "f-x", TaskID: "t1", Attempt: attempt, Card: "sg4~c1", Line: "wrote it", Why: "asked",
		Learned: "use the cache", Recap: "the recap", Files: []TaskFile{{Path: "out.txt", Name: "f-x-t1-out.txt", Size: 3}}}
}

func TestACapturedResultIsWrittenOnceAndAReplayIsANoOp(t *testing.T) {
	s := taskStore(t)
	addTask(t, s, "one")
	s.TaskBegin("f-x", "t1", "sg4")
	s.TaskLaunched("f-x", "t1", "sg4~c1", false, "")
	if moved, _ := s.TaskCapturingNow(captured(1)); !moved {
		t.Fatal("capturing")
	}
	got, applied, err := s.TaskCapturedAs(captured(1))
	if err != nil || !applied || got.State != TaskCaptured || got.Why != "asked" || len(got.Files) != 1 || got.Folder != "" {
		t.Fatalf("captured: %+v %v %v", got, applied, err)
	}
	if _, applied, _ := s.TaskCapturedAs(captured(1)); applied {
		t.Fatal("a replay was applied")
	}
	if wrote, _ := s.TaskReapedAs(captured(1)); !wrote {
		t.Fatal("reaped")
	}
	if wrote, _ := s.TaskReapedAs(captured(1)); wrote {
		t.Fatal("a replayed reap was written")
	}
	kinds := map[string]int{}
	facts, _ := s.WorkFacts("f-x")
	for _, f := range facts {
		kinds[f.Kind]++
	}
	for _, k := range []string{FactTaskStarted, FactTaskDone, FactTaskCaptured, FactTaskReaped, FactLearning} {
		if kinds[k] != 1 {
			t.Errorf("%d %s facts: %v", kinds[k], k, kinds)
		}
	}
}

func TestAFailedTaskIsRetriedAsANewAttemptAndTheOldAttemptIsIgnored(t *testing.T) {
	s := taskStore(t)
	addTask(t, s, "one")
	s.TaskBegin("f-x", "t1", "sg4")
	s.TaskLaunched("f-x", "t1", "sg4~c1", false, "")
	got, applied, _ := s.TaskFailedAs(TaskEvent{BacklogID: "f-x", TaskID: "t1", Attempt: 1, Folder: "/cards/a1", Reason: "exit 1"})
	if !applied || got.State != TaskFailed || got.Folder != "/cards/a1" {
		t.Fatalf("failed: %+v", got)
	}
	if moved, _ := s.TaskRetry("f-x", "t1"); !moved {
		t.Fatal("retry")
	}
	if got, began, _ := s.TaskBegin("f-x", "t1", "sg4"); !began || got.Attempt != 2 {
		t.Fatalf("attempt 2: %+v", got)
	}
	if _, applied, _ := s.TaskCapturedAs(captured(1)); applied {
		t.Fatal("attempt 1's capture was taken for attempt 2")
	}
	if moved, _ := s.TaskRetry("f-x", "t1"); moved {
		t.Fatal("a running task was retried")
	}
}

func TestLearningsAreCappedOnTheItemAndTheBriefTakesTheNewestWithinItsBytes(t *testing.T) {
	s := taskStore(t)
	addTask(t, s, "one")
	entry := strings.Repeat("l", 300)
	for i := 0; i < LearningEntries+2; i++ {
		tx, err := s.db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if err := addLearning(tx, ts(s.now()), "f-x", "t1", i+1, entry+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := s.Learnings("f-x", 0)
	brief, _ := s.Learnings("f-x", LearningBriefBytes)
	if len(all) == 0 || len(all) > LearningEntries {
		t.Fatalf("%d learnings on the item", len(all))
	}
	used := 0
	for _, l := range brief {
		used += len(l.Text)
	}
	if used > LearningBriefBytes || len(brief) == 0 || brief[0].Text != all[0].Text {
		t.Fatalf("the brief's learnings: %d bytes, %d entries", used, len(brief))
	}
	capped := 0
	facts, _ := s.WorkFacts("f-x")
	for _, f := range facts {
		if f.Kind == FactLearningCapped {
			capped++
		}
	}
	if capped == 0 {
		t.Fatal("the cap was reached with no fact saying so")
	}
}
