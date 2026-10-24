package sshwizard

import (
	"context"
	"strings"
	"testing"
)

// The machine of f-ssh-wslrelay-loopback: wslrelay answers on loopback with an Ubuntu sshd, the Windows sshd answers
// on the machine's own address.
func wslMachine() *fakeEnv {
	return &fakeEnv{
		goos: "windows", env: map[string]string{"ProgramData": `C:\ProgramData`},
		host: "sg3", addrs: []string{"127.0.0.1", "::1", "192.168.1.147", "fe80::1"},
		files: map[string]string{
			`C:\ProgramData\ssh\ssh_host_ed25519_key.pub`: keyLine("ssh-ed25519", "win") + " root@sg3\n",
		},
		scans: map[string]string{
			"127.0.0.1:22":     scanOut("OpenSSH_8.9p1 Ubuntu-3ubuntu0.1", "ssh-ed25519", "wsl"),
			"::1:22":           scanOut("OpenSSH_8.9p1 Ubuntu-3ubuntu0.1", "ssh-ed25519", "wsl"),
			"sg3:22":           scanOut("OpenSSH_for_Windows_9.5", "ssh-ed25519", "win"),
			"192.168.1.147:22": scanOut("OpenSSH_for_Windows_9.5", "ssh-ed25519", "win"),
		},
		owners: map[string]string{"127.0.0.1:22": "wslrelay"},
	}
}

func TestHostKeysReadThePubFilesAndSkipWhatIsNotAKey(t *testing.T) {
	env := wslMachine()
	env.files[`C:\ProgramData\ssh\ssh_host_rsa_key.pub`] = "not a key\n"
	got := HostKeys(env)
	if len(got) != 1 || got[0].Fingerprint != fp("win") {
		t.Fatalf("%+v", got)
	}
	lin := &fakeEnv{goos: "linux", files: map[string]string{"/etc/ssh/ssh_host_ed25519_key.pub": keyLine("ssh-ed25519", "x")}}
	if got := HostKeys(lin); len(got) != 1 {
		t.Fatalf("linux: %+v", got)
	}
}

func TestAMatchingKeyIsThisMachinesSSHD(t *testing.T) {
	env := wslMachine()
	p := ProbeAddr(context.Background(), env, "192.168.1.147", 22)
	if v := p.Verdict(HostKeys(env)); v != Mine {
		t.Fatalf("%s %+v", v, p)
	}
}

func TestAnUbuntuBannerWithAnotherKeyIsTheWrongSSHD(t *testing.T) {
	env := wslMachine()
	p := ProbeAddr(context.Background(), env, "127.0.0.1", 22)
	if v := p.Verdict(HostKeys(env)); v != Other || !p.Foreign() {
		t.Fatalf("%s foreign=%v %+v", v, p.Foreign(), p)
	}
}

func TestNoSSHDIsNothingAndSaysWhy(t *testing.T) {
	env := wslMachine()
	delete(env.scans, "127.0.0.1:22")
	p := ProbeAddr(context.Background(), env, "127.0.0.1", 22)
	if v := p.Verdict(HostKeys(env)); v != Nothing || !strings.Contains(p.Err, "refused") {
		t.Fatalf("%s %+v", v, p)
	}
}

func TestWithoutHostKeyFilesAnAnswerIsUnknownNotMine(t *testing.T) {
	env := wslMachine()
	env.files = nil
	p := ProbeAddr(context.Background(), env, "192.168.1.147", 22)
	if v := p.Verdict(HostKeys(env)); v != Unknown {
		t.Fatalf("%s", v)
	}
}

func TestWhichRefusesWSLRelayAndChoosesTheMachinesName(t *testing.T) {
	f := Which(context.Background(), wslMachine(), 22)
	if f.Chosen != "sg3" || f.Pinned {
		t.Fatalf("chose %q pinned=%v: %v", f.Chosen, f.Pinned, f.Notes)
	}
	all := strings.Join(f.Notes, "\n")
	if !strings.Contains(all, "127.0.0.1:22 is WSL's sshd (wslrelay), not Windows'") ||
		!strings.Contains(all, "192.168.1.147:22 is this machine's sshd") {
		t.Fatalf("the sentences:\n%s", all)
	}
	for _, p := range f.Probes {
		if p.Addr == "fe80::1" {
			t.Fatal("a link local address was scanned")
		}
	}
}

func TestWhichPinsAnIPWhenNoNameMatches(t *testing.T) {
	env := wslMachine()
	delete(env.scans, "sg3:22")
	f := Which(context.Background(), env, 22)
	if f.Chosen != "192.168.1.147" || !f.Pinned {
		t.Fatalf("%+v", f)
	}
}

func TestWhichPrefersLoopbackWhenItIsThisMachines(t *testing.T) {
	env := wslMachine()
	env.scans["127.0.0.1:22"] = scanOut("OpenSSH_for_Windows_9.5", "ssh-ed25519", "win")
	f := Which(context.Background(), env, 22)
	if f.Chosen != "127.0.0.1" || f.Pinned {
		t.Fatalf("%+v", f)
	}
}

func TestWhichChoosesNothingWhenNoAddressIsThisMachines(t *testing.T) {
	env := wslMachine()
	env.scans = map[string]string{"127.0.0.1:22": scanOut("OpenSSH_8.9p1 Ubuntu", "ssh-ed25519", "wsl")}
	f := Which(context.Background(), env, 22)
	if f.Chosen != "" {
		t.Fatalf("%+v", f)
	}
}

func TestDescribeSaysWhichSSHDARoomDials(t *testing.T) {
	env := wslMachine()
	ctx := context.Background()
	if s := Describe(ctx, env, "192.168.1.147", 22); !strings.Contains(s, "OpenSSH_for_Windows_9.5") ||
		!strings.Contains(s, fp("win")) || !strings.Contains(s, "this machine's own sshd") {
		t.Fatal(s)
	}
	if s := Describe(ctx, env, "127.0.0.1", 22); !strings.Contains(s, "Ubuntu") || !strings.Contains(s, "NOT this machine's") {
		t.Fatal(s)
	}
	if s := Describe(ctx, env, "10.0.0.9", 22); !strings.Contains(s, "refused") {
		t.Fatal(s)
	}
}
