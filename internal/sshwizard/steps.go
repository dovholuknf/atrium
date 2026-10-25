package sshwizard

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/base/remoteaccess"
	"github.com/dovholuknf/atrium/internal/base/sshcfg"
	"github.com/dovholuknf/atrium/internal/base/sshkey"
)

// Status is what one step found. A Check only reads, so none of these says a thing was changed.
type Status string

const (
	// OK means there is nothing to do.
	OK Status = "ok"
	// Todo means the step is not done and the wizard can do it itself.
	Todo Status = "todo"
	// NeedsHuman means a person must act, and Result.Command is the exact thing to run.
	NeedsHuman Status = "needs-human"
	// Failed means something is wrong that no command here names, and Detail says what.
	Failed Status = "failed"
	// Skipped means a step this one waits on is not done, or the thing it reads is not there to ask yet.
	Skipped Status = "skipped"
)

// Result is one step's answer.
type Result struct {
	N      int    `json:"n"`
	Key    string `json:"key"`
	Name   string `json:"name"`
	Status Status `json:"status"`
	// Found is the one line of what the step saw, in words a person reads.
	Found string `json:"found"`
	// Command is what a person must run when Status is needs-human, exactly as typed.
	Command string `json:"command,omitempty"`
	// Detail is more lines, one fact each.
	Detail []string `json:"detail,omitempty"`
}

// Options are what the person chose, with the defaults filled in from the machine.
type Options struct {
	// Port is sshd's port. Zero reads the switch, then 22.
	Port int
	// User is the login other rooms use. Empty reads the room's saved choice, then claude.
	User string
	// From is who may open the room's sshd. Empty reads the switch.
	From []string
	// SelfTest asks step 8 to log in to this room through the hub. It is on unless turned off.
	SelfTest bool
}

// Step is one line of the wizard. Needs names the steps whose failure makes this one meaningless.
type Step struct {
	N     int
	Key   string
	Name  string
	Needs []int
	Check func(ctx context.Context, r *Run) Result
}

// Run is what the steps share while one pass goes.
type Run struct {
	M    Machine
	Opt  Options
	GOOS string

	Joined  Joined
	Account Account
	State   remoteaccess.State
	// Finding is step 3's scan, and User the login step 4 settled on.
	Finding Finding
	User    string
	// PubKey is the room's public key line, and PubFP its fingerprint, once step 5 read them.
	PubKey string
	PubFP  string
	// AuthFile is where step 7 would write.
	AuthFile string
	results  map[int]Result
}

// Report is a whole pass.
type Report struct {
	Room    string   `json:"room,omitempty"`
	OS      string   `json:"os"`
	Results []Result `json:"steps"`
	// Ready is true when every step is ok.
	Ready   bool   `json:"ready"`
	Summary string `json:"summary"`
}

// Bound is how long a whole pass may take. A step that has not answered by then is reported as failed.
const Bound = 10 * time.Second

// Steps is the eleven steps in order. The order is the wizard's.
func Steps() []Step {
	return []Step{
		{1, "where", "Where am I", nil, checkWhere},
		{2, "sshd", "Is there an sshd", []int{1}, checkSSHD},
		{3, "which", "Which sshd answers, and is it this machine's", []int{1, 2}, checkWhich},
		{4, "user", "The login user", []int{1}, checkUser},
		{5, "key", "This room's key", []int{1}, checkKey},
		{6, "publish", "Publish", []int{1, 4, 5}, checkPublish},
		{7, "install", "Install the others' keys", []int{1, 4, 5}, checkInstall},
		{8, "roundtrip", "Prove a round trip", []int{1, 3, 5, 7}, checkRoundTrip},
		{9, "reach", "Reach the others", []int{1, 5}, checkReach},
		{10, "config", "Write ssh config here", []int{1}, checkConfig},
		{11, "on", "Turn it on", []int{1, 3, 4}, checkOn},
	}
}

// Check runs every step's check, in order, bounded at Bound, and changes nothing.
func Check(ctx context.Context, m Machine, opt Options) Report {
	_, rep := pass(ctx, m, opt, nil)
	return rep
}

// pass is Check, and the Run it left behind with what the steps found, for the fix half to act on. only, when not nil,
// names the steps to check, and every other step is reported skipped.
func pass(ctx context.Context, m Machine, opt Options, only map[int]bool) (*Run, Report) {
	ctx, cancel := context.WithTimeout(ctx, Bound)
	defer cancel()
	r := &Run{M: m, Opt: opt, GOOS: m.GOOS(), results: map[int]Result{}}
	r.Joined, r.Account = m.Joined(), m.Account()
	st, serr := m.Switch()
	r.State = st
	if serr != nil {
		r.State = remoteaccess.State{Version: remoteaccess.Version, Port: remoteaccess.DefaultPort}
	}
	rep := Report{Room: r.Joined.Room, OS: r.GOOS}
	for _, s := range Steps() {
		res := Result{N: s.N, Key: s.Key, Name: s.Name, Status: Skipped, Found: "not checked by this pass"}
		if only == nil || only[s.N] {
			res = r.run(ctx, s)
		}
		r.results[s.N] = res
		rep.Results = append(rep.Results, res)
	}
	rep.Ready = true
	for _, res := range rep.Results {
		if res.Status != OK {
			rep.Ready = false
		}
	}
	rep.Summary = summarize(rep)
	return r, rep
}

func (r *Run) run(ctx context.Context, s Step) Result {
	res := Result{N: s.N, Key: s.Key, Name: s.Name}
	for _, n := range s.Needs {
		switch r.results[n].Status {
		case Failed, NeedsHuman, Skipped:
			res.Status = Skipped
			res.Found = fmt.Sprintf("waits for step %d, %s", n, strings.ToLower(r.results[n].Name))
			return res
		}
	}
	if ctx.Err() != nil {
		res.Status, res.Found = Failed, "the check ran out of time before it got here"
		return res
	}
	out := s.Check(ctx, r)
	out.N, out.Key, out.Name = s.N, s.Key, s.Name
	if ctx.Err() != nil && out.Status == OK {
		out.Status, out.Found = Failed, "the check ran out of time: "+out.Found
	}
	return out
}

func summarize(rep Report) string {
	var todo, human, failed, skipped int
	for _, r := range rep.Results {
		switch r.Status {
		case Todo:
			todo++
		case NeedsHuman:
			human++
		case Failed:
			failed++
		case Skipped:
			skipped++
		}
	}
	switch {
	case rep.Ready:
		return "every step is done"
	case failed > 0:
		return fmt.Sprintf("%d failed, %d need a person, %d to do, %d waiting", failed, human, todo, skipped)
	case human > 0:
		return fmt.Sprintf("%d need a person, %d to do, %d waiting", human, todo, skipped)
	}
	return fmt.Sprintf("%d to do, %d waiting", todo, skipped)
}

func (r *Run) port() int {
	switch {
	case r.Opt.Port != 0:
		return r.Opt.Port
	case r.State.Port != 0:
		return r.State.Port
	}
	return remoteaccess.DefaultPort
}

// 1. Where am I.
func checkWhere(_ context.Context, r *Run) Result {
	j := r.Joined
	switch {
	case j.Err != nil:
		return Result{Status: Failed, Found: j.Err.Error()}
	case !j.Proven:
		return Result{Status: Failed, Found: "this room has no certificate that proves its name", Detail: []string{j.Rejoin}}
	}
	el := "not elevated"
	if r.Account.Elevated {
		el = "elevated"
	}
	return Result{Status: OK, Found: fmt.Sprintf("room %s on %s, running as %s (%s)", j.Room, osLabel(r.GOOS), r.Account.Name, el)}
}

func osLabel(goos string) string {
	switch goos {
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	}
	return goos
}

// 2. Is there an sshd.
func checkSSHD(ctx context.Context, r *Run) Result {
	m := r.M
	switch r.GOOS {
	case "windows":
		const exe = `C:\Windows\System32\OpenSSH\sshd.exe`
		out, err := m.Run(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
			`$s = Get-Service sshd -ErrorAction Stop; "$($s.Status)|$($s.StartType)"`)
		if err != nil {
			if !m.Exists(exe) {
				return Result{Status: NeedsHuman, Found: "the Windows OpenSSH server is not installed",
					Command: "Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0; Start-Service sshd; " +
						"Set-Service sshd -StartupType Automatic"}
			}
			return Result{Status: NeedsHuman, Found: "sshd.exe is there and no sshd service is registered",
				Command: `& "C:\Windows\System32\OpenSSH\install-sshd.ps1"; Start-Service sshd`}
		}
		status, start, _ := strings.Cut(strings.TrimSpace(out), "|")
		switch {
		case !strings.EqualFold(status, "Running"):
			return Result{Status: NeedsHuman, Found: "the sshd service is " + strings.ToLower(status),
				Command: "Start-Service sshd; Set-Service sshd -StartupType Automatic"}
		case !strings.EqualFold(start, "Automatic"):
			return Result{Status: NeedsHuman, Found: "sshd is running and its start type is " + start + ", so it will not come back after a reboot",
				Command: "Set-Service sshd -StartupType Automatic"}
		}
		return Result{Status: OK, Found: "Windows sshd, running, automatic start"}
	case "darwin":
		if _, err := m.Run(ctx, "launchctl", "print", "system/com.openssh.sshd"); err != nil {
			return Result{Status: NeedsHuman, Found: "Remote Login is off", Command: "sudo systemsetup -setremotelogin on"}
		}
		return Result{Status: OK, Found: "Remote Login is on"}
	}
	for _, unit := range []string{"ssh", "sshd"} {
		if out, _ := m.Run(ctx, "systemctl", "is-active", unit); strings.TrimSpace(out) == "active" {
			return Result{Status: OK, Found: "the " + unit + " service is active"}
		}
	}
	return Result{Status: NeedsHuman, Found: "no ssh or sshd service is active",
		Command: "sudo apt install openssh-server   (or the distro's package), then: sudo systemctl enable --now ssh"}
}

// 3. Which sshd answers, and is it this machine's.
func checkWhich(ctx context.Context, r *Run) Result {
	f := Which(ctx, r.M, r.port())
	r.Finding = f
	res := Result{Detail: f.Notes}
	anyOther := false
	for _, p := range f.Probes {
		if p.Verdict(f.Local) == Other {
			anyOther = true
		}
	}
	switch {
	case f.Chosen == "" && !anyOther:
		res.Status = NeedsHuman
		res.Found = fmt.Sprintf("nothing answers ssh on port %d", r.port())
		res.Command = "start sshd, then run this check again"
		return res
	case f.Chosen == "":
		res.Status = Failed
		res.Found = "no address answers with this machine's own sshd host key"
		return res
	}
	pin := ""
	if f.Pinned {
		pin = ", pinned to an IP address that can move with DHCP because no name matched"
	}
	saved := r.State.Host()
	switch {
	case saved == f.Chosen:
		res.Status = OK
		res.Found = fmt.Sprintf("%s:%d is this machine's sshd%s", f.Chosen, f.Port, pin)
	case r.State.On && !containsHost(f, saved):
		// What the room dials now is not this machine's sshd. Streams would open onto the wrong machine.
		res.Status = Failed
		res.Found = fmt.Sprintf("ssh: wrong sshd. the room dials %s:%d, which is not this machine's sshd. %s:%d is", saved, f.Port, f.Chosen, f.Port)
	default:
		res.Status = Todo
		res.Found = fmt.Sprintf("%s:%d is this machine's sshd%s. the room dials %s now", f.Chosen, f.Port, pin, saved)
	}
	return res
}

// containsHost says whether the saved address is one of the addresses that were scanned and answered as this
// machine's own.
func containsHost(f Finding, addr string) bool {
	for _, p := range f.Probes {
		if p.Addr == addr && p.Verdict(f.Local) == Mine {
			return true
		}
	}
	return false
}

// 4. The login user.
func checkUser(_ context.Context, r *Run) Result {
	user := r.Opt.User
	src := "chosen"
	if user == "" {
		user, src = r.State.User, "this room's setting"
	}
	if user == "" {
		user, src = remoteaccess.DefaultUser, "the default"
	}
	r.User = user
	if !remoteaccess.ValidUser(user) {
		return Result{Status: Failed, Found: fmt.Sprintf("%q is not a login name", user)}
	}
	if !r.M.UserExists(user) {
		return Result{Status: Failed, Found: fmt.Sprintf("there is no account %s on this machine", user),
			Detail: []string{"name the account other rooms log in as with --user, and the room keeps it"}}
	}
	return Result{Status: OK, Found: fmt.Sprintf("other rooms log in as %s (%s)", user, src)}
}

// 5. This room's key.
func checkKey(ctx context.Context, r *Run) Result {
	home := r.M.Home()
	priv := keyPathFor(r.GOOS, home)
	pub := priv + ".pub"
	b, err := r.M.ReadFile(pub)
	if err != nil {
		if !r.M.LookPath("ssh-keygen") {
			return Result{Status: NeedsHuman, Found: "there is no atrium key and no ssh-keygen to make one", Command: installClient(r.GOOS)}
		}
		return Result{Status: Todo, Found: "no key at " + pub + ". ssh-keygen will make an ed25519 key with no passphrase",
			Detail: []string{"an agent cannot type a passphrase, so the key has none. that is a decision, and the wizard says so"}}
	}
	line := strings.TrimSpace(string(b))
	k, ok := parseKeyLine(line)
	if !ok || k.Type != "ssh-ed25519" {
		return Result{Status: Failed, Found: pub + " is not an ssh-ed25519 public key"}
	}
	if !r.M.Exists(priv) {
		return Result{Status: Failed, Found: pub + " is there and its private half " + priv + " is not"}
	}
	r.PubKey, r.PubFP = keyBody(line), k.Fingerprint
	return Result{Status: OK, Found: "ed25519 key " + k.Fingerprint + " at " + priv}
}

func installClient(goos string) string {
	if goos == "windows" {
		return "Add-WindowsCapability -Online -Name OpenSSH.Client~~~~0.0.1.0"
	}
	return "install the OpenSSH client with this machine's package manager"
}

// keyBody is `type body`, the comment dropped, which is what two copies of a key are compared on.
func keyBody(line string) string {
	f := strings.Fields(line)
	if len(f) < 2 {
		return ""
	}
	return f[0] + " " + f[1]
}

// 6. Publish.
func checkPublish(ctx context.Context, r *Run) Result {
	p, err := r.M.Published(ctx)
	switch {
	case errors.Is(err, ErrUnavailable):
		return Result{Status: Skipped, Found: "the hub has no ssh record to read yet"}
	case err != nil:
		return Result{Status: Failed, Found: "the hub could not be asked: " + err.Error()}
	case p == nil || !p.Enabled:
		return Result{Status: Todo, Found: "the hub holds no key for this room"}
	case r.PubFP == "":
		return Result{Status: Todo, Found: "the hub holds a key, and this room has none yet to compare"}
	case p.Fingerprint != r.PubFP || p.User != r.User || p.Port != r.port() || p.On != r.State.On:
		return Result{Status: Todo, Found: fmt.Sprintf("the hub holds %s as %s on port %d, which is not what this room has now", p.Fingerprint, p.User, p.Port)}
	}
	return Result{Status: OK, Found: fmt.Sprintf("the hub holds this room's key, login %s, port %d", p.User, p.Port)}
}

// AuthorizedFile is where the login user's authorized keys live, and why. An administrator account on Windows reads
// administrators_authorized_keys, found the way sshd finds it, in sshd_config.
func AuthorizedFile(ctx context.Context, m Machine, user string) (path string, admin bool) {
	home := m.UserHome(user)
	if m.GOOS() != "windows" {
		return joinFor(m.GOOS(), home, ".ssh", "authorized_keys"), false
	}
	if !isWindowsAdmin(ctx, m, user) {
		return joinFor(m.GOOS(), home, ".ssh", "authorized_keys"), false
	}
	pd := m.Getenv("ProgramData")
	if pd == "" {
		pd = `C:\ProgramData`
	}
	conf, err := m.ReadFile(joinFor(m.GOOS(), pd, "ssh", "sshd_config"))
	file := joinFor(m.GOOS(), pd, "ssh", "administrators_authorized_keys")
	if err != nil {
		return file, true
	}
	inMatch := false
	for _, l := range strings.Split(string(conf), "\n") {
		f := strings.Fields(strings.TrimSpace(l))
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch {
		case strings.EqualFold(f[0], "Match"):
			inMatch = len(f) >= 3 && strings.EqualFold(f[1], "Group") && strings.EqualFold(f[2], "administrators")
		case inMatch && strings.EqualFold(f[0], "AuthorizedKeysFile") && len(f) > 1:
			p := strings.ReplaceAll(f[1], "__PROGRAMDATA__", pd)
			p = strings.ReplaceAll(p, "%PROGRAMDATA%", pd)
			return p, true
		}
	}
	return file, true
}

func isWindowsAdmin(ctx context.Context, m Machine, user string) bool {
	out, err := m.Run(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
		"(Get-LocalGroupMember -Group Administrators -ErrorAction SilentlyContinue).Name")
	if err != nil {
		return false
	}
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if i := strings.LastIndex(l, `\`); i >= 0 {
			l = l[i+1:]
		}
		if l != "" && strings.EqualFold(l, user) {
			return true
		}
	}
	return false
}

const (
	beginMark = "# BEGIN atrium"
	endMark   = "# END atrium"
)

// ManagedKeys reads the keys inside the managed block of an authorized keys file, as `type body`. A line outside the
// block is never read.
func ManagedKeys(text string) (keys []string, found bool) {
	in := false
	for _, l := range strings.Split(text, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, beginMark):
			in, found = true, true
		case strings.HasPrefix(t, endMark):
			in = false
		case in && t != "" && !strings.HasPrefix(t, "#"):
			if b := keyBody(t); b != "" {
				keys = append(keys, b)
			}
		}
	}
	return keys, found
}

// wantKey is one key the managed block should hold, and the room it belongs to.
type wantKey struct{ Body, Room string }

// wantedKeys is every key the managed block should hold: this room's own, for the self test, and each room the hub
// publishes that this room's from list allows. ok is false when the hub could not say.
//
// A ROOM'S OWN SWITCH IS NOT READ HERE. A room that only logs in to others has ssh off and still needs its key trusted
// by every room it logs in to. The from list is the gate on who may open this room's sshd, and sshd's own file is the
// gate on who may log in.
func (r *Run) wantedKeys(ctx context.Context) (keys []wantKey, ok bool) {
	if r.PubKey != "" && r.Opt.SelfTest {
		keys = append(keys, wantKey{r.PubKey, r.Joined.Room})
	}
	ps, err := r.M.Peers(ctx)
	if err != nil {
		return keys, false
	}
	from := r.Opt.From
	if len(from) == 0 {
		from = r.State.From
	}
	for _, p := range ps {
		if p.Room == r.Joined.Room || !fromAllows(from, p.Room) {
			continue
		}
		// THE HUB'S SET IS NOT TRUSTED TO BE CLEAN. A key with options in front, another key type or a room name that
		// is not a name never reaches authorized_keys, whatever the hub sent.
		if err := sshcfg.ValidRoom(p.Room); err != nil {
			log.Printf("[ssh] dropped a relayed key: %v", err)
			continue
		}
		b, err := sshkey.Clean(p.Key)
		if err != nil {
			log.Printf("[ssh] dropped the key relayed for room %s: %v", p.Room, err)
			continue
		}
		if !hasWant(keys, b) {
			keys = append(keys, wantKey{b, p.Room})
		}
	}
	return keys, true
}

func hasWant(l []wantKey, body string) bool {
	for _, k := range l {
		if k.Body == body {
			return true
		}
	}
	return false
}

// wanted is wantedKeys as bodies, and how many of them are another room's.
func (r *Run) wanted(ctx context.Context) (keys []string, peers int, ok bool) {
	ws, ok := r.wantedKeys(ctx)
	for _, w := range ws {
		keys = append(keys, w.Body)
		if w.Room != r.Joined.Room {
			peers++
		}
	}
	return keys, peers, ok
}

// fromAllows is the room's own from list applied to another room's name. `hub` names the hub and no room.
func fromAllows(from []string, room string) bool {
	for _, f := range from {
		if f == "*" || strings.EqualFold(f, room) {
			return true
		}
	}
	return false
}

// 7. Install the others' keys.
func checkInstall(ctx context.Context, r *Run) Result {
	path, admin := AuthorizedFile(ctx, r.M, r.User)
	r.AuthFile = path
	res := Result{}
	if admin {
		res.Detail = append(res.Detail, "an administrator account on Windows reads "+path+", and sshd ignores it unless only Administrators and SYSTEM can write it")
	}
	text := ""
	if b, err := r.M.ReadFile(path); err == nil {
		text = string(b)
	}
	have, found := ManagedKeys(text)
	want, peers, known := r.wanted(ctx)
	if !known {
		// The hub cannot say what the set is, so only what is certain is compared: this room's own key.
		if found && contains(have, r.PubKey) {
			res.Status, res.Found = OK, fmt.Sprintf("%s holds atrium's block with %d key(s). not compared with the hub's set, which cannot be read yet", path, len(have))
			return res
		}
		res.Status, res.Found = Todo, "no atrium block in "+path+". the hub's set cannot be read yet, so only this room's own key would go in"
		return res.withAdminGate(r, admin)
	}
	missing := 0
	for _, k := range want {
		if !contains(have, k) {
			missing++
		}
	}
	extra := 0
	for _, k := range have {
		if !contains(want, k) {
			extra++
		}
	}
	if found && missing == 0 && extra == 0 {
		res.Status, res.Found = OK, fmt.Sprintf("%s holds atrium's block: %d room key(s) and this room's own", path, peers)
		return res
	}
	res.Status = Todo
	res.Found = fmt.Sprintf("%s needs %d key(s) added and %d removed in atrium's block", path, missing, extra)
	if !found {
		res.Found = fmt.Sprintf("no atrium block in %s. %d key(s) would go in", path, len(want))
	}
	return res.withAdminGate(r, admin)
}

// withAdminGate turns a todo into needs-human when the file is the administrators one and the room is not elevated:
// writing it takes elevation, and the wizard does everything else.
func (res Result) withAdminGate(r *Run, admin bool) Result {
	if admin && !r.Account.Elevated {
		res.Status = NeedsHuman
		res.Command = fmt.Sprintf(`icacls %q /inheritance:r /grant "Administrators:F" /grant "SYSTEM:F"`, r.AuthFile)
		res.Detail = append(res.Detail, "run from an elevated PowerShell. the room is not elevated, so it cannot write this file")
	}
	return res
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// sshTest is the ssh a round trip runs. BatchMode so nothing prompts, and a bound so nothing hangs.
func sshTest(ctx context.Context, r *Run, host string) (string, error) {
	return r.M.Run(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", host, "echo", "atrium-ok")
}

// Diagnose maps what ssh said to the layer that failed, as the design lists them.
func Diagnose(out string) string {
	o := strings.ToLower(out)
	switch {
	case strings.Contains(o, "permission denied"):
		return "permission denied (publickey): the key or the authorized keys file is wrong (step 5 or 7)"
	case strings.Contains(o, "host key verification failed") || strings.Contains(o, "remote host identification has changed"):
		return "host key mismatch: the tunnel landed on a different sshd than the one published (step 3)"
	case strings.Contains(o, "could not resolve hostname"):
		return "ssh does not know this room's name: its atrium_config block is missing (step 10)"
	case strings.Contains(o, "connection closed") || strings.Contains(o, "connection reset") || strings.Contains(o, "kex_exchange_identification"):
		return "the connection closed before a banner: the hub carry or the room's switch refused it"
	case strings.Contains(o, "ubuntu"):
		return "a banner naming another system answered: it is the wrong sshd (step 3)"
	}
	return "ssh said: " + strings.TrimSpace(out)
}

// 8. Prove a round trip.
func checkRoundTrip(ctx context.Context, r *Run) Result {
	if !r.State.On {
		return Result{Status: Skipped, Found: "ssh is off in this room, so there is no path to prove yet"}
	}
	if r.Joined.Room == "" {
		return Result{Status: Skipped, Found: "no room name to log in to"}
	}
	if !r.Opt.SelfTest {
		return Result{Status: Skipped, Found: "the self test is turned off"}
	}
	out, err := sshTest(ctx, r, r.Joined.Room)
	if err == nil && strings.Contains(out, "atrium-ok") {
		return Result{Status: OK, Found: "this room logged in to itself through the hub and got atrium-ok"}
	}
	return Result{Status: Failed, Found: Diagnose(out)}
}

// 9. Reach the others.
func checkReach(ctx context.Context, r *Run) Result {
	ps, err := r.M.Peers(ctx)
	if errors.Is(err, ErrUnavailable) {
		return Result{Status: Skipped, Found: "the hub has no ssh record of other rooms to read yet"}
	}
	if err != nil {
		return Result{Status: Failed, Found: "the hub could not be asked: " + err.Error()}
	}
	var live []PeerKey
	for _, p := range ps {
		if p.Room != r.Joined.Room && p.Online && p.SSHOn {
			live = append(live, p)
		}
	}
	if len(live) == 0 {
		return Result{Status: OK, Found: "no other room is online with ssh on, so there is nobody to reach"}
	}
	// One ssh per room, together, so a slow room does not eat the others' time.
	type answer struct {
		line string
		ok   bool
	}
	ans := make([]answer, len(live))
	var wg sync.WaitGroup
	for i, p := range live {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := sshTest(ctx, r, p.Room)
			if err == nil && strings.Contains(out, "atrium-ok") {
				ans[i] = answer{p.Room + ": reached", true}
				return
			}
			why := Diagnose(out)
			if strings.Contains(why, "permission denied") {
				why += ". it may not have this room's key yet: it picks it up within a minute, or run its wizard"
			}
			ans[i] = answer{p.Room + ": " + why, false}
		}()
	}
	wg.Wait()
	res := Result{Status: OK}
	bad := 0
	for _, a := range ans {
		res.Detail = append(res.Detail, a.line)
		if !a.ok {
			bad++
		}
	}
	res.Found = fmt.Sprintf("reached %d of %d room(s)", len(live)-bad, len(live))
	if bad > 0 {
		res.Status = Failed
	}
	return res
}

// 10. Write ssh config here.
func checkConfig(ctx context.Context, r *Run) Result {
	sh, err := r.M.SSHConfig()
	if err != nil {
		return Result{Status: Failed, Found: "~/.ssh could not be read: " + err.Error()}
	}
	ps, perr := r.M.Peers(ctx)
	if perr != nil {
		ps = nil
	}
	var want []string
	for _, p := range ps {
		if p.Room != r.Joined.Room && p.SSHOn {
			want = append(want, p.Room)
		}
	}
	missing := 0
	for _, w := range want {
		if !contains(sh.Rooms, w) {
			missing++
		}
	}
	res := Result{}
	switch {
	case !sh.Included:
		res.Status = Todo
		res.Found = "~/.ssh/config does not include atrium_config. the wizard asks before it adds that one line"
	case missing > 0:
		res.Status = Todo
		res.Found = fmt.Sprintf("atrium_config has no block for %d room(s) that have ssh on", missing)
	default:
		res.Status = OK
		res.Found = fmt.Sprintf("atrium_config is included and holds %d block(s)", len(sh.Rooms))
	}
	if errors.Is(perr, ErrUnavailable) {
		res.Detail = append(res.Detail, "the hub's list of rooms cannot be read yet, so the blocks were not compared with it")
	}
	return res
}

// 11. Turn it on.
func checkOn(_ context.Context, r *Run) Result {
	st := r.State
	want := r.Finding.Chosen
	switch {
	case !st.On:
		return Result{Status: Todo, Found: "ssh is off in this room. this step turns it on, for " + strings.Join(r.fromList(), ", ")}
	case want != "" && st.Host() != want:
		return Result{Status: Todo, Found: fmt.Sprintf("ssh is on and dials %s, and this machine's sshd is at %s", st.Host(), want)}
	}
	return Result{Status: OK, Found: "ssh is on: " + remoteaccess.Status{State: st}.Describe()}
}

func (r *Run) fromList() []string {
	if len(r.Opt.From) > 0 {
		return r.Opt.From
	}
	if len(r.State.From) > 0 {
		return r.State.From
	}
	return []string{"hub"}
}
