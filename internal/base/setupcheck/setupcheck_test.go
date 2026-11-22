package setupcheck

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func fakeEnv(goos string, answers map[string]Ran, onPath ...string) Env {
	return Env{
		GOOS: goos,
		Getenv: func(k string) string {
			return map[string]string{"COMPUTERNAME": "ROOM-D", "TEMP": `C:\Users\c\AppData\Local\Temp`}[k]
		},
		Run: func(_ time.Duration, name string, args ...string) Ran {
			if r, ok := answers[strings.Join(append([]string{name}, args...), " ")]; ok {
				return r
			}
			return Ran{Code: -1, Err: errors.New("not faked")}
		},
		LookPath: func(n string) (string, error) {
			for _, p := range onPath {
				if p == n {
					return "/usr/bin/" + n, nil
				}
			}
			return "", errors.New("not found")
		},
		Exe: func() (string, error) { return "/usr/local/bin/atrium", nil },
	}
}

func byID(r Report, id string) Result {
	for _, c := range r.Checks {
		if c.ID == id {
			return c
		}
	}
	return Result{}
}

func TestInspectOnLinuxReadsTheAccountToolsAndBinary(t *testing.T) {
	env := fakeEnv("linux", map[string]Ran{
		"id -un": {Out: "agent\n"}, "id -u": {Out: "1001\n"}, "id -Gn": {Out: "agent users\n"}, "uname -n": {Out: "box\n"},
		"sudo -n -l": {Code: 1, Out: "sudo: a password is required"},
	}, "sudo", "go", "make")
	rep := Inspect(env)
	if c := byID(rep, "account.standard"); c.State != OK || !strings.HasPrefix(c.Detail, "agent: ") {
		t.Errorf("account: %+v", c)
	}
	if c := byID(rep, "tool.go"); c.State != OK || c.Detail != "/usr/bin/go" {
		t.Errorf("go: %+v", c)
	}
	if c := byID(rep, "tool.git"); c.State != Warn || c.Fix != FixExplain {
		t.Errorf("a missing git is a warn that is explained: %+v", c)
	}
	if c := byID(rep, "tool.node"); c.State != NA {
		t.Errorf("a missing node is n/a: %+v", c)
	}
	if c := byID(rep, "binary.installed"); c.State != OK {
		t.Errorf("binary: %+v", c)
	}
	if rep.Warning != 1 || rep.Failing != 0 {
		t.Errorf("counts: %d warn %d fail", rep.Warning, rep.Failing)
	}
}

func TestSudoThatTimesOutIsAWarn(t *testing.T) {
	env := fakeEnv("linux", map[string]Ran{
		"id -un": {Out: "agent"}, "id -u": {Out: "1001"}, "id -Gn": {Out: "agent"}, "uname -n": {Out: "box"},
		"sudo -n -l": {Code: -1, TimedOut: true},
	}, "sudo")
	if c := byID(Inspect(env), "account.standard"); c.State != Warn || !strings.Contains(c.Detail, "did not answer in 5 seconds") {
		t.Errorf("%+v", c)
	}
}

func TestPasswordlessSudoIsFound(t *testing.T) {
	env := fakeEnv("linux", map[string]Ran{
		"id -un": {Out: "agent"}, "id -u": {Out: "1001"}, "id -Gn": {Out: "agent"}, "uname -n": {Out: "box"},
		"sudo -n -l": {Out: "User agent may run the following commands:\n    (ALL) NOPASSWD: ALL\n"},
	}, "sudo")
	if c := byID(Inspect(env), "account.standard"); c.State != Warn || !strings.Contains(c.Detail, "NOPASSWD: ALL") {
		t.Errorf("%+v", c)
	}
}

func TestInspectOnWindowsReadsWhoami(t *testing.T) {
	groups := `"Everyone","Well-known group","S-1-1-0","x"` + "\n" +
		`"BUILTIN\Administrators","Alias","S-1-5-32-544","Group used for deny only"` + "\n" +
		`"Mandatory Label\Medium Mandatory Level","Label","S-1-16-8192",""`
	env := fakeEnv("windows", map[string]Ran{"whoami": {Out: "ROOM-D\\claude\r\n"}, "whoami /groups /fo csv /nh": {Out: groups}})
	c := byID(Inspect(env), "account.standard")
	if c.State != Warn || !strings.Contains(c.Detail, `ROOM-D\claude is in Administrators (S-1-5-32-544) with a filtered token`) {
		t.Errorf("%+v", c)
	}
	env = fakeEnv("windows", map[string]Ran{"whoami": {Out: "ROOM-D\\claude"}, "whoami /groups /fo csv /nh": {Code: -1, Err: errors.New("boom")}})
	if c := byID(Inspect(env), "account.standard"); c.State != Warn || !strings.Contains(c.Detail, "could not tell") {
		t.Errorf("a probe that fails is a warn: %+v", c)
	}
}

func TestOperatorAccountIsReadFromTheEnvironment(t *testing.T) {
	env := fakeEnv("linux", map[string]Ran{
		"id -un": {Out: "owner"}, "id -u": {Out: "1000"}, "id -Gn": {Out: "owner"}, "uname -n": {Out: "box"}, "sudo -n -l": {Code: 1},
	}, "sudo")
	env.Getenv = func(k string) string {
		if k == "ATRIUM_OPERATOR_ACCOUNT" {
			return "owner"
		}
		return ""
	}
	if c := byID(Inspect(env), "account.dedicated"); c.State != Warn {
		t.Errorf("%+v", c)
	}
}

func TestBinaryInABuildOrTempFolderFails(t *testing.T) {
	for _, exe := range []string{"/work/atrium/build.claude/atrium", "/home/c/Downloads/atrium", "C:/Users/c/AppData/Local/Temp/x/atrium.exe"} {
		env := fakeEnv("linux", nil)
		env.Exe = func() (string, error) { return exe, nil }
		if c := binaryRow(env); c.State != Fail || c.Fix != FixExplain {
			t.Errorf("%s: %+v", exe, c)
		}
	}
}
