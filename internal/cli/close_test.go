package cli

import (
	"strings"
	"testing"
)

func TestCloseAnswersMatchTheFlagsToTheAskedWorktrees(t *testing.T) {
	asks := []closeAskOut{{Seq: 1, Path: "D:/wt/o/r/a"}, {Seq: 5, Path: "D:/wt/o/r/b"}}

	got, missing, err := closeAnswers(asks, []string{"1"}, []string{`D:\wt\o\r\b\`}, nil)
	if err != nil || got["1"] != "keep" || got["5"] != "stash" || len(missing) != 0 {
		t.Fatalf("%v %v %v", got, missing, err)
	}
	got, missing, err = closeAnswers(asks, nil, nil, []string{"all"})
	if err != nil || got["1"] != "delete" || got["5"] != "delete" || len(missing) != 0 {
		t.Fatalf("all: %v %v %v", got, missing, err)
	}
	_, missing, err = closeAnswers(asks, []string{"5"}, nil, nil)
	if err != nil || strings.Join(missing, ",") != "D:/wt/o/r/a" {
		t.Fatalf("missing: %v %v", missing, err)
	}
	if _, _, err := closeAnswers(asks, []string{"9"}, nil, nil); err == nil {
		t.Error("a flag naming no asked worktree was taken")
	}
	if _, _, err := closeAnswers(asks, []string{"1"}, nil, []string{"all"}); err == nil {
		t.Error("a worktree told keep and delete was taken")
	}
}
