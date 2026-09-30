package daemon

import (
	"log"
	"os"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// setPriorityClass is a variable so a test can make the raise fail.
var setPriorityClass = windows.SetPriorityClass

var raiseFailedOnce sync.Once

// ptyRaise raises an interactive runner and the pseudo console host behind its pty to
// ABOVE_NORMAL_PRIORITY_CLASS. Make it with beginPTYRaise BEFORE pty.New and call apply after Start.
//
// A keystroke crosses the runner and its console host on the way to the screen and back, and the
// runner redraws in between. On a busy Windows machine a Normal process that only sleeps 1ms
// overslept by 15 to 149ms in bursts: 571 stalls in 120s, while the same loop at AboveNormal, run at
// the same moment, logged none. The room and the hub already raise themselves, but the runner and
// its console host stayed Normal and competed on equal terms with go test, headless chrome and pwsh,
// so a keystroke waited at the runner's redraw. See docs/terminal/input-lag-logging.md.
//
// THE CHILDREN OF THE RUNNER ARE LEFT ALONE, ON PURPOSE. Windows hands a priority class down to a
// child only for Idle and BelowNormal, so the go test and chrome an agent starts stay Normal and keep
// the machine as busy as clint wants it, below the key path. Do not "fix" that by walking the tree.
//
// Never fails a spawn. A runner always starts, even when the raise is refused.
type ptyRaise struct {
	on     bool
	before map[uint32]bool
}

func (d *Daemon) beginPTYRaise() *ptyRaise {
	r := &ptyRaise{on: d.st.RunnerPriorityRaised()}
	if r.on {
		r.before = consoleHosts()
	}
	return r
}

func (r *ptyRaise) apply(pid int) {
	if r == nil || !r.on {
		return
	}
	// CreatePseudoConsole starts the host as OUR child, and there is no handle to it in go-pty, so the
	// hosts that were not there before pty.New are this pty's. Another spawn racing in gets raised
	// too, which is also an interactive pty and harmless.
	targets := []uint32{uint32(pid)}
	for host := range consoleHosts() {
		if !r.before[host] {
			targets = append(targets, host)
		}
	}
	for _, t := range targets {
		if err := raiseProcess(t); err != nil {
			raiseFailedOnce.Do(func() {
				log.Printf("[atrium] could not raise a runner's priority, keystrokes may lag under load: %v", err)
			})
		}
	}
}

func raiseProcess(pid uint32) error {
	h, err := windows.OpenProcess(windows.PROCESS_SET_INFORMATION, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return setPriorityClass(h, windows.ABOVE_NORMAL_PRIORITY_CLASS)
}

// consoleHosts is every conhost.exe or OpenConsole.exe that is a child of this process.
func consoleHosts() map[uint32]bool {
	out := map[uint32]bool{}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	me := uint32(os.Getpid())
	for err := windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if e.ParentProcessID != me {
			continue
		}
		name := strings.ToLower(windows.UTF16ToString(e.ExeFile[:]))
		if name == "conhost.exe" || name == "openconsole.exe" {
			out[e.ProcessID] = true
		}
	}
	return out
}
