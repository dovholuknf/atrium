package ctl

import "testing"

func TestChatClassSeesAtriumNeedAndNothingElse(t *testing.T) {
	for name := range WorkerTools {
		if got, want := InClass(name, ClassChat), name == "atrium_need"; got != want {
			t.Errorf("%s in chat class: %v, want %v", name, got, want)
		}
	}
	if InClass("atrium_launch", ClassChat|Elevated) || InClass("atrium_launch_worker", ClassChat) {
		t.Fatal("a chat card is shown a launch tool")
	}
}
