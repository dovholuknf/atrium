package cli

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
	"golang.org/x/term"
)

// serviceStopGrace bounds the room's wind-down after the SCM asks it to stop.
const serviceStopGrace = 60 * time.Second

// runAsService runs the command line as a service when the SCM started this process. Anything else, a console
// included, answers false and runs as it always did.
func runAsService(args []string) (int, bool) {
	if is, err := svc.IsWindowsService(); err != nil || !is {
		return 0, false
	}
	toRoomLog(args)
	h := &roomService{args: args}
	serviceRestart = func() { h.restart.Store(true) }
	if err := svc.Run("atrium", h); err != nil {
		log.Printf("[service] %v", err)
		return 1, true
	}
	return h.code, true
}

// toRoomLog points stdout, stderr and the log at room.log beside the room's --dir, because a service has no console.
func toRoomLog(args []string) {
	dir := roomDir()
	for i, a := range args {
		if a == "--dir" && i+1 < len(args) {
			dir = args[i+1]
		} else if v, ok := strings.CutPrefix(a, "--dir="); ok {
			dir = v
		}
	}
	f, err := os.OpenFile(filepath.Join(dir, roomLogName), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	os.Stdout, os.Stderr = f, f
	log.SetOutput(f)
}

type roomService struct {
	args    []string
	code    int
	restart atomic.Bool
}

// Execute is the SCM's view of the room. Stop and shutdown are the room's ctrl-c, which leaves the pty host's runners up.
func (s *roomService) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	done := make(chan int, 1)
	go func() { done <- run(s.args) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case code := <-done:
			code = serviceExitCode(code, s.restart.Load())
			s.code = code
			return code != 0, uint32(code)
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				log.Printf("[service] the SCM asked the room to stop")
				askToStop()
				tick, deadline := time.NewTicker(5*time.Second), time.After(serviceStopGrace)
				for n := uint32(1); ; n++ {
					status <- svc.Status{State: svc.StopPending, CheckPoint: n, WaitHint: 15000}
					select {
					case <-done:
						return false, 0
					case <-deadline:
						return false, 0
					case <-tick.C:
					}
				}
			}
		}
	}
}

func installService(o serviceOpts) error {
	if o.exe == "" {
		self, err := os.Executable()
		if err != nil {
			return err
		}
		o.exe = self
	}
	o.exe, _ = filepath.Abs(o.exe)
	if o.user == "" {
		u, err := user.Current()
		if err != nil {
			return err
		}
		o.user = u.Username
	}
	interactive := term.IsTerminal(int(os.Stdin.Fd()))
	start, err := chooseStart(o.start, interactive)
	if err != nil {
		return err
	}
	pw, err := servicePassword(o.passwordFile, o.user, interactive)
	if err != nil {
		return err
	}
	if pw == "" {
		return fmt.Errorf("%s has a blank password, and Windows refuses to start a service as an account with one "+
			"(error 1069, a logon failure). give the account a password first, as an administrator: "+
			"net user %s <password>. then run this again with that password", o.user, strings.TrimPrefix(o.user, `.\`))
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("could not open the service manager: %w. installing a service needs an administrator", err)
	}
	defer m.Disconnect()

	if err := grantServiceLogon(o.user); err != nil {
		return fmt.Errorf("could not give %s the right to log on as a service: %w. grant it in Local Security Policy, "+
			"Local Policies, User Rights Assignment, Log on as a service, and run this again", o.user, err)
	}
	args := append([]string{"room"}, o.roomArgs...)
	s, err := m.CreateService(o.name, o.exe, mgr.Config{
		DisplayName:      o.name,
		Description:      "atrium room",
		StartType:        start,
		ServiceStartName: o.user,
		Password:         pw,
	}, args...)
	if err != nil {
		return fmt.Errorf("could not create service %s: %w", o.name, err)
	}
	defer s.Close()
	// A crash restarts the room. A clean exit does not, so `net stop` stays stopped.
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{{Type: mgr.ServiceRestart, Delay: 10 * time.Second}}, 86400)
	_ = s.SetRecoveryActionsOnNonCrashFailures(true)

	fmt.Printf("  installed service %s, running as %s\n", o.name, o.user)
	if start == mgr.StartAutomatic {
		fmt.Printf("  it starts at boot. to make it start only by hand: sc config %s start= demand\n", o.name)
	} else {
		fmt.Printf("  it starts only when you run: net start %s\n  to start it at boot instead: sc config %s start= auto\n", o.name, o.name)
	}
	fmt.Printf("  start it now: net start %s\n  stop it: net stop %s\n", o.name, o.name)
	fmt.Printf("  if the account's password changes, run: atrium service password --name %s\n", o.name)
	return nil
}

// chooseStart is the start type: --start, else asked in plain words on a terminal, else automatic.
func chooseStart(flag string, interactive bool) (uint32, error) {
	switch strings.ToLower(strings.TrimSpace(flag)) {
	case "auto", "automatic":
		return mgr.StartAutomatic, nil
	case "manual", "demand":
		return mgr.StartManual, nil
	case "":
	default:
		return 0, fmt.Errorf("--start is auto or manual, not %q", flag)
	}
	if !interactive {
		return mgr.StartAutomatic, nil
	}
	fmt.Println("When should this room start?")
	fmt.Println("  auto    when the machine boots, before anybody logs in. Right for a room that should always be there.")
	fmt.Println("  manual  only when you run `net start <name>`. Right while you are trying it out.")
	fmt.Println("You can change it later with `sc config <name> start= auto` or `start= demand`.")
	fmt.Print("auto or manual [auto]: ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.TrimSpace(line) == "" {
		return mgr.StartAutomatic, nil
	}
	return chooseStart(line, false)
}

func servicePassword(file, account string, interactive bool) (string, error) {
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		return strings.TrimRight(string(b), "\r\n"), nil
	}
	if v := os.Getenv("ATRIUM_SERVICE_PASSWORD"); v != "" {
		return v, nil
	}
	if !interactive {
		return "", errors.New("no password for the account. give --password-file or set ATRIUM_SERVICE_PASSWORD")
	}
	fmt.Printf("password for %s: ", account)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return string(b), err
}

func passwordService(name, file string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("could not open the service manager: %w. this needs an administrator", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("no service named %s: %w", name, err)
	}
	defer s.Close()
	pw, err := servicePassword(file, "the service's account", term.IsTerminal(int(os.Stdin.Fd())))
	if err != nil {
		return err
	}
	if pw == "" {
		return errors.New("a blank password cannot start a service. give the account a password first")
	}
	p, err := syscall.UTF16PtrFromString(pw)
	if err != nil {
		return err
	}
	const noChange = windows.SERVICE_NO_CHANGE
	if err := windows.ChangeServiceConfig(s.Handle, noChange, noChange, noChange, nil, nil, nil, nil, nil, p, nil); err != nil {
		return err
	}
	fmt.Printf("  stored the new password for service %s. start it with: net start %s\n", name, name)
	return nil
}

func uninstallService(name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("could not open the service manager: %w. removing a service needs an administrator", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		fmt.Printf("  no service named %s. nothing to remove\n", name)
		return nil
	}
	defer s.Close()
	if st, err := s.Query(); err == nil && st.State != svc.Stopped {
		_, _ = s.Control(svc.Stop)
		for end := time.Now().Add(serviceStopGrace + 10*time.Second); time.Now().Before(end); time.Sleep(500 * time.Millisecond) {
			if st, err := s.Query(); err != nil || st.State == svc.Stopped {
				break
			}
		}
	}
	if err := s.Delete(); err != nil {
		return err
	}
	fmt.Printf("  removed service %s\n", name)
	return nil
}

func statusService(name string) error {
	h, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return err
	}
	defer windows.CloseServiceHandle(h)
	n, _ := syscall.UTF16PtrFromString(name)
	sh, err := windows.OpenService(h, n, windows.SERVICE_QUERY_STATUS|windows.SERVICE_QUERY_CONFIG)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		fmt.Printf("  no service named %s\n", name)
		return nil
	}
	if err != nil {
		return err
	}
	s := &mgr.Service{Name: name, Handle: sh}
	defer s.Close()
	st, err := s.Query()
	if err != nil {
		return err
	}
	cfg, err := s.Config()
	if err != nil {
		return err
	}
	states := map[svc.State]string{svc.Stopped: "stopped", svc.StartPending: "starting", svc.StopPending: "stopping",
		svc.Running: "running", svc.ContinuePending: "continuing", svc.PausePending: "pausing", svc.Paused: "paused"}
	start := map[uint32]string{mgr.StartAutomatic: "auto (at boot)", mgr.StartManual: "manual", mgr.StartDisabled: "disabled"}[cfg.StartType]
	fmt.Printf("  service  %s\n  state    %s\n  start    %s\n  account  %s\n  runs     %s\n",
		name, states[st.State], start, cfg.ServiceStartName, cfg.BinaryPathName)
	return nil
}

// grantServiceLogon gives account SeServiceLogonRight, which CreateService does not.
func grantServiceLogon(account string) error {
	sid, _, _, err := windows.LookupSID("", account)
	if err != nil {
		return err
	}
	adv := windows.NewLazySystemDLL("advapi32.dll")
	var attrs [6]uintptr
	attrs[0] = unsafe.Sizeof(attrs)
	var policy windows.Handle
	const policyCreateAccount, policyLookupNames = 0x10, 0x800
	r, _, _ := adv.NewProc("LsaOpenPolicy").Call(0, uintptr(unsafe.Pointer(&attrs)), policyCreateAccount|policyLookupNames, uintptr(unsafe.Pointer(&policy)))
	if r != 0 {
		return lsaErr(adv, r)
	}
	defer adv.NewProc("LsaClose").Call(uintptr(policy))
	right, err := windows.NewNTUnicodeString("SeServiceLogonRight")
	if err != nil {
		return err
	}
	r, _, _ = adv.NewProc("LsaAddAccountRights").Call(uintptr(policy), uintptr(unsafe.Pointer(sid)), uintptr(unsafe.Pointer(right)), 1)
	if r != 0 {
		return lsaErr(adv, r)
	}
	return nil
}

func lsaErr(adv *windows.LazyDLL, status uintptr) error {
	code, _, _ := adv.NewProc("LsaNtStatusToWinError").Call(status)
	return syscall.Errno(code)
}
