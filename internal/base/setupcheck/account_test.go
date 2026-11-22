package setupcheck

import (
	"reflect"
	"strings"
	"testing"
)

const std = "S-1-1-0;S-1-5-32-545;S-1-5-14;S-1-5-4;S-1-2-1;S-1-5-11;S-1-5-15;S-1-5-113;S-1-2-0;S-1-5-64-10;S-1-16-8192"

func win(user, sids string) Facts {
	return Facts{"user": user, "host": "SG3", "elevated": "False", "sids": sids, "docker": "0"}
}

func unx(user, uid, groups, rc string) Facts {
	return Facts{"user": user, "uid": uid, "groups": groups, "host": "m1mini", "sudo.rc": rc, "sudo.nopasswd": "0", "sudo.all": "0"}
}

func reasons(f Facts, kind string, ops ...string) []string { return Judge(f, kind, ops).Reasons }

func TestWindowsGroupsAreMatchedBySID(t *testing.T) {
	admin := "is in Administrators (S-1-5-32-544)"
	cases := []struct {
		name  string
		facts Facts
		want  []string
	}{
		{"standard user is clean", win(`SG3\claude`, std), nil},
		{"filtered admin", win(`SG3\claude`, std+";S-1-5-32-544"), []string{admin + " with a filtered token"}},
		{"domain admins by RID", win(`CORP\c`, std+";S-1-5-21-111-222-333-512"), []string{"is in Domain Admins (RID 512)"}},
		{"enterprise admins by RID", win(`CORP\c`, std+";S-1-5-21-111-222-333-519"), []string{"is in Enterprise Admins (RID 519)"}},
		{"a RID that only ends in 512", win(`SG3\c`, std+";S-1-5-21-1-2-3-5120;S-1-5-21-1-2-3-1512"), nil},
		{"a group only named Administrators", win(`SG3\c`, std+";S-1-5-21-1-2-3-1105"), nil},
		{"backup operators", win(`SG3\c`, std+";S-1-5-32-551"), []string{"is in Backup Operators (S-1-5-32-551, can read every file)"}},
		{"hyper-v administrators", win(`SG3\c`, std+";S-1-5-32-578"), []string{"is in Hyper-V Administrators (S-1-5-32-578, can mount any disk)"}},
		{"look-alike SIDs", win(`SG3\c`, std+";S-1-5-32-5510;S-1-5-21-1-2-3-578"), nil},
	}
	for _, c := range cases {
		if got := reasons(c.facts, "windows"); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	f := win(`SG3\claude`, std+";S-1-5-32-544")
	f["elevated"] = "True"
	if got, want := reasons(f, "windows"), []string{admin, "the token is elevated"}; !reflect.DeepEqual(got, want) {
		t.Errorf("elevated admin: got %q, want %q", got, want)
	}
	f = win(`SG3\claude`, std)
	f["docker"] = "1"
	if got := reasons(f, "windows"); len(got) != 1 || !strings.Contains(got[0], "docker-users") {
		t.Errorf("docker-users: got %q", got)
	}
}

func TestNoUserOrNoGroupsIsUnknownNeverClean(t *testing.T) {
	if Judge(Facts{}, "windows", nil).Known {
		t.Error("no user must be unknown")
	}
	if Judge(win(`SG3\claude`, ""), "windows", nil).Known {
		t.Error("a user with no group list must be unknown")
	}
}

func TestUnixGroupsAndSudo(t *testing.T) {
	cases := []struct {
		name string
		f    Facts
		kind string
		want []string
	}{
		{"mac claude user", unx("claude", "501", "staff,everyone,localaccounts", "1"), "mac", nil},
		{"mac root", unx("root", "0", "wheel,admin", "1"), "mac", []string{"is root (uid 0)", "is in the group wheel", "is in the group admin"}},
		{"linux sudo and docker", unx("dev", "1000", "dev,sudo,docker", "1"), "linux", []string{"is in the group sudo", "is in the group docker (root on this host)"}},
		{"mac docker is not special", unx("c", "501", "staff,docker,libvirt", "1"), "mac", nil},
		{"whole names only", unx("dev", "1000", "dev,sudoers,dockers,wheel2", "1"), "linux", nil},
		{"no sudo installed", unx("dev", "1000", "dev", "none"), "linux", nil},
		{"sudo needs a password", unx("dev", "1000", "dev", "1"), "linux", nil},
		{"sudo timed out", unx("dev", "1000", "dev", "timeout"), "linux", []string{"may have sudo, since sudo -n -l did not answer in 5 seconds"}},
	}
	for _, c := range cases {
		if got := reasons(c.f, c.kind); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	f := unx("dev", "1000", "dev", "0")
	f["sudo.nopasswd"], f["sudo.all"] = "1", "1"
	if got := reasons(f, "linux"); len(got) != 1 || !strings.Contains(got[0], "NOPASSWD: ALL") {
		t.Errorf("nopasswd: %q", got)
	}
	f["sudo.nopasswd"] = "0"
	if got := reasons(f, "linux"); len(got) != 1 || !strings.Contains(got[0], "lists ALL") {
		t.Errorf("all: %q", got)
	}
	f["sudo.all"] = "0"
	if got := reasons(f, "linux"); len(got) != 1 || !strings.Contains(got[0], "some command") {
		t.Errorf("narrow: %q", got)
	}
}

func TestOperatorList(t *testing.T) {
	op := func(f Facts, kind string, ops ...string) bool { return Judge(f, kind, ops).Operator }
	m := unx("owner", "501", "staff", "1")
	if !op(m, "mac", "owner") || !op(unx("Owner", "501", "staff", "1"), "mac", "owner") {
		t.Error("a listed name matches, case aside")
	}
	if op(unx("owner2", "501", "staff", "1"), "mac", "owner") {
		t.Error("a listed name does not match a longer one")
	}
	if !op(m, "mac", "owner@m1mini") || op(m, "mac", "owner@other") {
		t.Error("name@host matches that host only")
	}
	if !op(win(`CORP\clint`, std), "windows", `CORP\clint`) || op(win(`OTHER\clint`, std), "windows", `CORP\clint`) {
		t.Error(`DOMAIN\name needs the domain too`)
	}
	if got := OperatorNames("a, b", "c;d,,e"); !reflect.DeepEqual(got, []string{"a", "b", "c", "d", "e"}) {
		t.Errorf("operator list: %q", got)
	}
}

func TestWhoamiSIDsAreTheThirdColumn(t *testing.T) {
	csv := strings.Join([]string{
		`"Everyone","Well-known group","S-1-1-0","Mandatory group, Enabled by default, Enabled group"`,
		`"BUILTIN\Administrators","Alias","S-1-5-32-544","Group used for deny only"`,
		`"CORP\Sales, EMEA","Group","S-1-5-21-111-222-333-1105","Mandatory group"`,
		`"PC\Docker-Users","Alias","S-1-5-21-1-2-3-1002","x"`,
		`"PC\docker-users2","Alias","S-1-5-21-1-2-3-1003","x"`,
		`"Mandatory Label\High Mandatory Level","Label","S-1-16-12288",""`,
	}, "\n")
	sids, docker := WhoamiGroups(csv)
	want := []string{"S-1-1-0", "S-1-5-32-544", "S-1-5-21-111-222-333-1105", "S-1-5-21-1-2-3-1002", "S-1-5-21-1-2-3-1003", "S-1-16-12288"}
	if !reflect.DeepEqual(sids, want) || docker != 1 {
		t.Errorf("got %q docker %d", sids, docker)
	}
	if !Elevated(sids) || Elevated([]string{"S-1-16-8192"}) {
		t.Error("the High integrity label is an elevated token and Medium is not")
	}
}

func TestParseFactsTakesTheFirstAndFlagsAConflict(t *testing.T) {
	f, bad := ParseFacts([]string{"user=svc", "uid=1", "groups=svc", "groups=svc", "x=1", "x=2"})
	if bad != "" || f["user"] != "svc" {
		t.Errorf("same value twice or an unused key is fine: %q %v", bad, f)
	}
	if _, bad := ParseFacts([]string{"user=svc", "user=root"}); bad == "" {
		t.Error("one fact with two values is not an answer")
	}
}

func TestAccountRowsSayWhatWasSeen(t *testing.T) {
	std1, ded := AccountRows(unx("claude", "501", "staff,everyone", "1"), "", "mac", nil)
	if std1.State != OK || std1.Detail != "claude: not elevated, not in an admin group, no passwordless sudo" {
		t.Errorf("clean: %+v", std1)
	}
	if ded.State != NA {
		t.Errorf("no operator named is n/a: %+v", ded)
	}
	f := win(`SG3\claude`, std+";S-1-5-32-544")
	f["elevated"] = "True"
	r, _ := AccountRows(f, "", "windows", nil)
	if r.State != Warn || r.Fix != FixAdmin || !strings.HasPrefix(r.Detail, `SG3\claude is in Administrators (S-1-5-32-544) and the token is elevated. `) ||
		!strings.Contains(r.Detail, "docs/room-accounts.md") {
		t.Errorf("elevated admin: %+v", r)
	}
	r, _ = AccountRows(Facts{}, "the probe said nothing", "linux", nil)
	if r.State != Warn || !strings.Contains(r.Detail, "could not tell who the room runs as (the probe said nothing)") {
		t.Errorf("unknown is a warn: %+v", r)
	}
	_, ded = AccountRows(unx("owner", "501", "staff", "1"), "", "mac", []string{"owner"})
	if ded.State != Warn || ded.Fix != FixAdmin {
		t.Errorf("the operator's own account: %+v", ded)
	}
	_, ded = AccountRows(unx("agent", "501", "staff", "1"), "", "mac", []string{"owner"})
	if ded.State != OK {
		t.Errorf("another account is ok: %+v", ded)
	}
}
