package sshwizard

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/remoteaccess"
	"github.com/dovholuknf/atrium/internal/sshcfg"
)

type ranAnswer struct {
	out string
	err error
}

// fakeMachine is a Machine for the steps. Run answers by a substring of the command line, so a test names what it
// cares about.
type fakeMachine struct {
	*fakeEnv
	runs      map[string]ranAnswer
	exists    map[string]bool
	have      map[string]bool // commands on the PATH
	home      string
	joined    Joined
	account   Account
	users     map[string]bool
	sw        remoteaccess.State
	swErr     error
	shown     sshcfg.Shown
	published *Published
	pubErr    error
	peers     []PeerKey
	peersErr  error
	ran       []string
	ranMu     sync.Mutex
	// onRun lets a test make a command do what it would: ssh-keygen leaves files.
	onRun     func(m *fakeMachine, line string)
	userHomes map[string]string
	sent      []PublishRecord
	pubSend   error
	savedUser string
	mkdirs    []string
	modes     map[string]os.FileMode
}

const getService = "$s = Get-Service"

func (m *fakeMachine) Run(_ context.Context, name string, args ...string) (string, error) {
	line := name + " " + strings.Join(args, " ")
	m.ranMu.Lock()
	m.ran = append(m.ran, line)
	m.ranMu.Unlock()
	if m.onRun != nil {
		m.onRun(m, line)
	}
	// The longest key that matches wins, so a test can override one command and leave the rest.
	best, found := "", false
	for k := range m.runs {
		if strings.Contains(line, k) && len(k) >= len(best) {
			best, found = k, true
		}
	}
	if found {
		a := m.runs[best]
		return a.out, a.err
	}
	return "", errors.New("exit status 1")
}
func (m *fakeMachine) LookPath(n string) bool              { return m.have[n] }
func (m *fakeMachine) Exists(p string) bool                { return m.exists[p] || m.fakeEnv.files[p] != "" }
func (m *fakeMachine) Home() string                        { return m.home }
func (m *fakeMachine) Joined() Joined                      { return m.joined }
func (m *fakeMachine) Account() Account                    { return m.account }
func (m *fakeMachine) UserExists(n string) bool            { return m.users[n] }
func (m *fakeMachine) Switch() (remoteaccess.State, error) { return m.sw, m.swErr }
func (m *fakeMachine) SSHConfig() (sshcfg.Shown, error)    { return m.shown, nil }
func (m *fakeMachine) Published(context.Context) (*Published, error) {
	return m.published, m.pubErr
}
func (m *fakeMachine) UserHome(n string) string {
	if h, ok := m.userHomes[n]; ok {
		return h
	}
	return m.home
}
func (m *fakeMachine) MkdirAll(p string, _ os.FileMode) error {
	m.mkdirs = append(m.mkdirs, p)
	return nil
}
func (m *fakeMachine) WriteFile(p string, b []byte, mode os.FileMode) error {
	m.fakeEnv.files[p] = string(b)
	if m.modes == nil {
		m.modes = map[string]os.FileMode{}
	}
	m.modes[p] = mode
	return nil
}
func (m *fakeMachine) Publish(_ context.Context, r PublishRecord) error {
	if m.pubSend != nil {
		return m.pubSend
	}
	m.sent = append(m.sent, r)
	return nil
}
func (m *fakeMachine) SaveUser(u string) error                  { m.savedUser = u; m.sw.User = u; return nil }
func (m *fakeMachine) Peers(context.Context) ([]PeerKey, error) { return m.peers, m.peersErr }

// winMachine is sg3 at the start: joined and proven, a standard user, Windows sshd running, wslrelay on loopback,
// nothing set up for ssh.
func winMachine() *fakeMachine {
	return &fakeMachine{
		fakeEnv: wslMachine(),
		runs: map[string]ranAnswer{
			getService:             {out: "Running|Automatic\n"},
			"Get-LocalGroupMember": {out: "SG3\\Administrator\n"},
		},
		exists:  map[string]bool{},
		have:    map[string]bool{"ssh-keygen": true},
		home:    `C:\Users\claude`,
		joined:  Joined{Room: "sg3", Proven: true},
		account: Account{Name: "claude"},
		users:   map[string]bool{"claude": true},
		sw:      remoteaccess.State{Version: 1, Port: 22},
		shown:   sshcfg.Shown{},
		pubErr:  ErrUnavailable, peersErr: ErrUnavailable,
	}
}

func (m *fakeMachine) withKey() *fakeMachine {
	m.fakeEnv.files[`C:\Users\claude\.ssh\atrium_ed25519.pub`] = keyLine("ssh-ed25519", "mine") + " atrium\n"
	m.fakeEnv.files[`C:\Users\claude\.ssh\atrium_ed25519`] = "private"
	return m
}

func (m *fakeMachine) step(t *testing.T, n int, opt Options) Result {
	t.Helper()
	rep := Check(context.Background(), m, opt)
	if len(rep.Results) != 11 {
		t.Fatalf("%d steps, want eleven", len(rep.Results))
	}
	return rep.Results[n-1]
}

func want(t *testing.T, r Result, s Status, sub string) {
	t.Helper()
	if r.Status != s || !strings.Contains(r.Found+"\n"+strings.Join(r.Detail, "\n"), sub) {
		t.Fatalf("step %d: %s %q (%v), want %s containing %q", r.N, r.Status, r.Found, r.Detail, s, sub)
	}
}

func TestTheElevenStepsAreInOrderAndNamed(t *testing.T) {
	got := Steps()
	if len(got) != 11 {
		t.Fatal(len(got))
	}
	for i, s := range got {
		if s.N != i+1 || s.Key == "" || s.Name == "" || s.Check == nil {
			t.Fatalf("step %d is not filled in: %+v", i+1, s)
		}
		for _, n := range s.Needs {
			if n >= s.N {
				t.Fatalf("step %d waits for step %d, which does not come before it", s.N, n)
			}
		}
	}
}

func TestStep1WhereAmI(t *testing.T) {
	m := winMachine()
	want(t, m.step(t, 1, Options{}), OK, "room sg3 on Windows, running as claude (not elevated)")
	m.joined = Joined{Room: "sg3", Rejoin: "run `atrium rooms token`"}
	r := m.step(t, 1, Options{})
	want(t, r, Failed, "no certificate")
	want(t, r, Failed, "atrium rooms token")
	m.joined = Joined{Err: errors.New("this machine has not joined a hub yet")}
	want(t, m.step(t, 1, Options{}), Failed, "not joined")
}

func TestAFailedFirstStepSkipsTheRest(t *testing.T) {
	m := winMachine()
	m.joined = Joined{Err: errors.New("no hub")}
	rep := Check(context.Background(), m, Options{})
	for _, r := range rep.Results[1:] {
		if r.Status != Skipped {
			t.Fatalf("step %d was %s after step 1 failed", r.N, r.Status)
		}
	}
	if rep.Ready {
		t.Fatal("a failed pass is ready")
	}
	if len(m.fakeEnv.scanCalls) != 0 {
		t.Fatalf("a skipped step still scanned: %v", m.fakeEnv.scanCalls)
	}
}

func TestStep2SSHD(t *testing.T) {
	m := winMachine()
	want(t, m.step(t, 2, Options{}), OK, "Windows sshd, running, automatic start")

	m.runs[getService] = ranAnswer{err: errors.New("no service")}
	r := m.step(t, 2, Options{})
	want(t, r, NeedsHuman, "not installed")
	if !strings.Contains(r.Command, "Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0") ||
		!strings.Contains(r.Command, "Start-Service sshd") || !strings.Contains(r.Command, "Set-Service sshd -StartupType Automatic") {
		t.Fatalf("the install line: %q", r.Command)
	}
	m.exists[`C:\Windows\System32\OpenSSH\sshd.exe`] = true
	want(t, m.step(t, 2, Options{}), NeedsHuman, "no sshd service is registered")

	m.runs[getService] = ranAnswer{out: "Stopped|Automatic"}
	r = m.step(t, 2, Options{})
	want(t, r, NeedsHuman, "stopped")
	if !strings.Contains(r.Command, "Start-Service sshd") {
		t.Fatal(r.Command)
	}
	m.runs[getService] = ranAnswer{out: "Running|Manual"}
	r = m.step(t, 2, Options{})
	want(t, r, NeedsHuman, "Manual")
	if r.Command != "Set-Service sshd -StartupType Automatic" {
		t.Fatal(r.Command)
	}
}

func TestStep2SSHDOnMacAndLinux(t *testing.T) {
	m := winMachine()
	m.fakeEnv.goos = "darwin"
	m.runs = map[string]ranAnswer{"launchctl print system/com.openssh.sshd": {out: "ok"}}
	want(t, m.step(t, 2, Options{}), OK, "Remote Login is on")
	m.runs = nil
	r := m.step(t, 2, Options{})
	want(t, r, NeedsHuman, "Remote Login is off")
	if r.Command != "sudo systemsetup -setremotelogin on" {
		t.Fatal(r.Command)
	}

	m.fakeEnv.goos = "linux"
	m.runs = map[string]ranAnswer{"systemctl is-active sshd": {out: "active\n"}}
	want(t, m.step(t, 2, Options{}), OK, "sshd service is active")
	m.runs = nil
	r = m.step(t, 2, Options{})
	want(t, r, NeedsHuman, "no ssh or sshd service")
	if !strings.Contains(r.Command, "sudo apt install openssh-server") {
		t.Fatal(r.Command)
	}
}

func TestStep3WhichSSHD(t *testing.T) {
	m := winMachine()
	// Saved addr is the default and loopback is wslrelay's: the wizard chooses the machine's name.
	r := m.step(t, 3, Options{})
	want(t, r, Todo, "sg3:22 is this machine's sshd")
	want(t, r, Todo, "127.0.0.1:22 is WSL's sshd (wslrelay), not Windows'")

	// Already pointed at the right one.
	m.sw.Addr = "sg3"
	want(t, m.step(t, 3, Options{}), OK, "sg3:22 is this machine's sshd")

	// ssh is on and dials the wslrelay: loud, not silent.
	m.sw = remoteaccess.State{Version: 1, On: true, Port: 22, From: []string{"hub"}}
	want(t, m.step(t, 3, Options{}), Failed, "ssh: wrong sshd")

	// Nothing answers anywhere.
	m2 := winMachine()
	m2.fakeEnv.scans = nil
	want(t, m2.step(t, 3, Options{}), NeedsHuman, "nothing answers ssh on port 22")

	// Something answers, and none of it is this machine's.
	m3 := winMachine()
	delete(m3.fakeEnv.scans, "sg3:22")
	delete(m3.fakeEnv.scans, "192.168.1.147:22")
	want(t, m3.step(t, 3, Options{}), Failed, "no address answers with this machine's own sshd host key")
}

func TestStep4LoginUser(t *testing.T) {
	m := winMachine()
	want(t, m.step(t, 4, Options{}), OK, "log in as claude (the default)")
	m.sw.User = "localai"
	want(t, m.step(t, 4, Options{}), Failed, "no account localai")
	m.users["localai"] = true
	want(t, m.step(t, 4, Options{}), OK, "log in as localai (this room's setting)")
	m.sw.User = ""
	delete(m.users, "localai")
	want(t, m.step(t, 4, Options{User: "localai"}), Failed, "no account localai")
	m.users["localai"] = true
	want(t, m.step(t, 4, Options{User: "localai"}), OK, "log in as localai (chosen)")
	delete(m.users, "claude")
	want(t, m.step(t, 4, Options{}), Failed, "no account claude")
}

func TestStep5Key(t *testing.T) {
	m := winMachine()
	want(t, m.step(t, 5, Options{}), Todo, "no key at")
	m.have = nil
	r := m.step(t, 5, Options{})
	want(t, r, NeedsHuman, "no ssh-keygen")
	if !strings.Contains(r.Command, "OpenSSH.Client") {
		t.Fatal(r.Command)
	}
	m.withKey()
	want(t, m.step(t, 5, Options{}), OK, fp("mine"))
	delete(m.fakeEnv.files, `C:\Users\claude\.ssh\atrium_ed25519`)
	want(t, m.step(t, 5, Options{}), Failed, "private half")
	m.fakeEnv.files[`C:\Users\claude\.ssh\atrium_ed25519.pub`] = keyLine("ssh-rsa", "old")
	want(t, m.step(t, 5, Options{}), Failed, "not an ssh-ed25519")
}

func TestStep6Publish(t *testing.T) {
	m := winMachine().withKey()
	want(t, m.step(t, 6, Options{}), Skipped, "no ssh record")
	m.pubErr = errors.New("hub down")
	want(t, m.step(t, 6, Options{}), Failed, "hub could not be asked")
	m.pubErr = nil
	want(t, m.step(t, 6, Options{}), Todo, "holds no key")
	m.published = &Published{User: "claude", Port: 22, Fingerprint: fp("someone else"), Enabled: true}
	want(t, m.step(t, 6, Options{}), Todo, "not what this room has now")
	m.published.Fingerprint = fp("mine")
	m.sw.On = true
	want(t, m.step(t, 6, Options{}), Todo, "not what this room has now")
	m.published.On = true
	want(t, m.step(t, 6, Options{}), OK, "login claude, port 22")
}

func TestStep7InstallTheOthersKeys(t *testing.T) {
	const auth = `C:\Users\claude\.ssh\authorized_keys`
	m := winMachine().withKey()
	opt := Options{SelfTest: true}

	// The hub cannot say yet and there is no block.
	want(t, m.step(t, 7, opt), Todo, "no atrium block")

	block := "ssh-rsa AAAA user@laptop\n# BEGIN atrium (managed)\n" + keyLine("ssh-ed25519", "mine") + " atrium:sg3\n# END atrium\n"
	m.fakeEnv.files[auth] = block
	want(t, m.step(t, 7, opt), OK, "not compared with the hub's set")

	// With the hub's set: sg4 is allowed by from, m1mini is not, a room outside the from list is not, and a room's own switch is not read.
	m.peersErr = nil
	m.sw.From = []string{"hub", "sg4"}
	m.peers = []PeerKey{
		{Room: "sg4", Key: keyLine("ssh-ed25519", "sg4"), SSHOn: true},
		{Room: "m1mini", Key: keyLine("ssh-ed25519", "m1mini"), SSHOn: true},
		{Room: "off", Key: keyLine("ssh-ed25519", "off"), SSHOn: false},
	}
	want(t, m.step(t, 7, opt), Todo, "needs 1 key(s) added and 0 removed")
	m.fakeEnv.files[auth] = block[:strings.Index(block, "# END")] + keyLine("ssh-ed25519", "sg4") + " atrium:sg4\n# END atrium\n"
	want(t, m.step(t, 7, opt), OK, "1 room key(s) and this room's own")

	// A removed room's key still in the block is a removal to do. A key outside the block is never read.
	m.fakeEnv.files[auth] = strings.Replace(m.fakeEnv.files[auth], "# END atrium", keyLine("ssh-ed25519", "gone")+" atrium:gone\n# END atrium", 1) +
		keyLine("ssh-ed25519", "outside") + " me\n"
	want(t, m.step(t, 7, opt), Todo, "1 removed")

	// --no-self-test leaves this room's own key out of what is wanted.
	m.fakeEnv.files[auth] = "# BEGIN atrium\n" + keyLine("ssh-ed25519", "sg4") + "\n# END atrium\n"
	want(t, m.step(t, 7, Options{SelfTest: false}), OK, "1 room key(s)")
}

func TestStep7TheWindowsAdministratorsFile(t *testing.T) {
	m := winMachine().withKey()
	m.runs["Get-LocalGroupMember"] = ranAnswer{out: "SG3\\Administrator\nSG3\\claude\n"}
	m.fakeEnv.files[`C:\ProgramData\ssh\sshd_config`] = "Port 22\nMatch Group administrators\n       AuthorizedKeysFile __PROGRAMDATA__/ssh/administrators_authorized_keys\n"
	r := m.step(t, 7, Options{SelfTest: true})
	want(t, r, NeedsHuman, "administrators_authorized_keys")
	if !strings.Contains(r.Command, "icacls") || !strings.Contains(r.Command, "Administrators:F") || !strings.Contains(r.Command, "SYSTEM:F") {
		t.Fatalf("the icacls line: %q", r.Command)
	}
	// Elevated, the wizard does it itself.
	m.account.Elevated = true
	want(t, m.step(t, 7, Options{SelfTest: true}), Todo, "no atrium block")
	// A standard user who is not an administrator keeps the home file and needs no one.
	m.account = Account{Name: "claude"}
	m.runs["Get-LocalGroupMember"] = ranAnswer{out: "SG3\\Administrator\n"}
	r = m.step(t, 7, Options{SelfTest: true})
	want(t, r, Todo, ".ssh")
}

func TestManagedKeysReadsOnlyInsideTheBlock(t *testing.T) {
	keys, found := ManagedKeys("ssh-ed25519 AAAA outside\n# BEGIN atrium x\nssh-ed25519 BBBB c\n\n# note\n# END atrium\nssh-ed25519 CCCC outside2\n")
	if !found || len(keys) != 1 || keys[0] != "ssh-ed25519 BBBB" {
		t.Fatalf("%v %v", keys, found)
	}
	if _, found := ManagedKeys("ssh-ed25519 AAAA only\n"); found {
		t.Fatal("found a block that is not there")
	}
}

func TestStep8RoundTrip(t *testing.T) {
	const auth = `C:\Users\claude\.ssh\authorized_keys`
	m := winMachine().withKey()
	m.sw = remoteaccess.State{Version: 1, On: true, Addr: "sg3", Port: 22, From: []string{"hub"}}
	m.fakeEnv.files[auth] = "# BEGIN atrium\n" + keyLine("ssh-ed25519", "mine") + "\n# END atrium\n"
	opt := Options{SelfTest: true}

	// Off: there is no path to prove.
	m.sw.On = false
	want(t, m.step(t, 8, opt), Skipped, "off")
	m.sw.On = true

	for out, sub := range map[string]string{
		"claude@sg3: Permission denied (publickey).": "step 5 or 7",
		"Connection closed by UNKNOWN port 65535":    "before a banner",
		"SSH-2.0-OpenSSH_8.9p1 Ubuntu mismatch":      "wrong sshd",
		"Host key verification failed.":              "host key mismatch",
		"ssh: Could not resolve hostname sg3":        "atrium_config",
	} {
		m.runs["ssh -o BatchMode=yes"] = ranAnswer{out: out, err: errors.New("exit status 255")}
		want(t, m.step(t, 8, opt), Failed, sub)
	}
	m.runs["ssh -o BatchMode=yes"] = ranAnswer{out: "atrium-ok\n"}
	want(t, m.step(t, 8, opt), OK, "atrium-ok")
	last := m.ran[len(m.ran)-1]
	if !strings.Contains(last, "BatchMode=yes") || !strings.Contains(last, "sg3 echo atrium-ok") {
		t.Fatalf("the ssh that ran: %q", last)
	}

	want(t, m.step(t, 8, Options{SelfTest: false}), Skipped, "turned off")
}

func TestStep8WaitsForThePathToBeBuilt(t *testing.T) {
	m := winMachine().withKey()
	m.sw = remoteaccess.State{Version: 1, On: true, Port: 22, From: []string{"hub"}}
	// Step 7 has no block yet for a non elevated admin: nothing may be run as a login test.
	m.runs["Get-LocalGroupMember"] = ranAnswer{out: "SG3\\claude\n"}
	r := m.step(t, 8, Options{SelfTest: true})
	want(t, r, Skipped, "waits for step 3")
	for _, l := range m.ran {
		if strings.HasPrefix(l, "ssh ") {
			t.Fatalf("an ssh ran before the path was ready: %q", l)
		}
	}
}

func TestStep9ReachTheOthers(t *testing.T) {
	m := winMachine().withKey()
	opt := Options{SelfTest: true}
	want(t, m.step(t, 9, opt), Skipped, "no ssh record")
	m.peersErr = nil
	want(t, m.step(t, 9, opt), OK, "nobody to reach")
	m.peers = []PeerKey{
		{Room: "sg4", Online: true, SSHOn: true},
		{Room: "m1mini", Online: true, SSHOn: true},
		{Room: "asleep", Online: false, SSHOn: true},
		{Room: "sg3", Online: true, SSHOn: true},
	}
	m.runs["ConnectTimeout=10 sg4"] = ranAnswer{out: "atrium-ok"}
	m.runs["ConnectTimeout=10 m1mini"] = ranAnswer{out: "Permission denied (publickey).", err: errors.New("exit status 255")}
	r := m.step(t, 9, opt)
	want(t, r, Failed, "reached 1 of 2")
	want(t, r, Failed, "sg4: reached")
	want(t, r, Failed, "it picks it up within a minute")
	for _, l := range r.Detail {
		if strings.HasPrefix(l, "asleep") || strings.HasPrefix(l, "sg3") {
			t.Fatalf("asked a room that is offline or is this one: %v", r.Detail)
		}
	}
	m.runs["ConnectTimeout=10 m1mini"] = ranAnswer{out: "atrium-ok"}
	want(t, m.step(t, 9, opt), OK, "reached 2 of 2")
}

func TestStep10Config(t *testing.T) {
	m := winMachine()
	opt := Options{}
	want(t, m.step(t, 10, opt), Todo, "does not include atrium_config")
	m.shown = sshcfg.Shown{Included: true, ConfigExists: true}
	r := m.step(t, 10, opt)
	want(t, r, OK, "holds 0 block(s)")
	want(t, r, OK, "cannot be read yet")
	m.peersErr = nil
	m.peers = []PeerKey{{Room: "sg4", SSHOn: true}, {Room: "m1mini", SSHOn: true}, {Room: "off"}}
	m.shown.Rooms = []string{"sg4"}
	want(t, m.step(t, 10, opt), Todo, "no block for 1 room(s)")
	m.shown.Rooms = []string{"sg4", "m1mini"}
	want(t, m.step(t, 10, opt), OK, "holds 2 block(s)")
}

func TestStep11On(t *testing.T) {
	m := winMachine()
	want(t, m.step(t, 11, Options{From: []string{"sg4"}}), Todo, "ssh is off in this room. this step turns it on, for sg4")
	// On and dialling the chosen address.
	m.sw = remoteaccess.State{Version: 1, On: true, Addr: "sg3", Port: 22, From: []string{"hub"}}
	want(t, m.step(t, 11, Options{}), OK, "ssh is on: on, sg3:22, from hub")
	// On, but at loopback, which is wslrelay's: the wrong sshd is loud, and what waits on it waits.
	m.sw.Addr = ""
	want(t, m.step(t, 11, Options{}), Skipped, "waits for step 3")
	// On at an address that is mine but not the one chosen: a todo.
	m.fakeEnv.scans["127.0.0.1:22"] = scanOut("OpenSSH_for_Windows_9.5", "ssh-ed25519", "win")
	m.sw.Addr = "192.168.1.147"
	want(t, m.step(t, 11, Options{}), Todo, "dials 192.168.1.147")
}

func TestACheckChangesNothing(t *testing.T) {
	m := winMachine().withKey()
	m.sw = remoteaccess.State{Version: 1, On: true, Port: 22, From: []string{"hub"}}
	Check(context.Background(), m, Options{SelfTest: true})
	for _, l := range m.ran {
		for _, w := range []string{"Add-WindowsCapability", "Set-Service", "Start-Service", "icacls", "ssh-keygen", "systemsetup"} {
			if strings.Contains(l, w) {
				t.Fatalf("a check ran %q", l)
			}
		}
	}
}

// slowMachine never answers until its context ends.
type slowMachine struct{ *fakeMachine }

func (s *slowMachine) Keyscan(ctx context.Context, host string, port int) (Scan, error) {
	<-ctx.Done()
	return Scan{}, ctx.Err()
}
func (s *slowMachine) Run(ctx context.Context, name string, args ...string) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

func TestASlowMachineIsBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	rep := Check(ctx, &slowMachine{winMachine()}, Options{})
	if time.Since(start) > 5*time.Second {
		t.Fatalf("the pass took %s", time.Since(start))
	}
	if rep.Ready {
		t.Fatal("a pass that ran out of time is ready")
	}
}
