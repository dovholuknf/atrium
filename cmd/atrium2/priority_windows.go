package main

import (
	"log"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// priorityEnv turns the raise off. Set to "normal" to keep the default class.
const priorityEnv = "ATRIUM_PRIORITY"

// raisePriority moves the hub or the room to ABOVE_NORMAL_PRIORITY_CLASS.
//
// A keystroke crosses both processes on its way to the pty and back, and each
// one only copies a few bytes. On a busy Windows machine that is not enough to
// get scheduled promptly: a Normal-priority process that does nothing but sleep
// 1ms overslept by 15 to 200ms in bursts, while the same loop at AboveNormal,
// run at the same moment, never overslept once. Those bursts are the spiky echo
// the input-lag logging showed, with the hub looking slow because it is the
// process in the middle.
//
// ABOVE NORMAL, NOT HIGH. Both processes sit idle on sockets almost all the
// time, so the raise costs the rest of the machine nothing until a byte
// arrives. The runners they start are not raised: Windows gives a child the
// Normal class unless it is asked otherwise, so agents compete as before.
func raisePriority() {
	if strings.EqualFold(strings.TrimSpace(os.Getenv(priorityEnv)), "normal") {
		return
	}
	if err := windows.SetPriorityClass(windows.CurrentProcess(), windows.ABOVE_NORMAL_PRIORITY_CLASS); err != nil {
		log.Printf("[atrium] could not raise the process priority, keystrokes may lag under load: %v", err)
	}
}
