package daemon

import (
	"reflect"
	"testing"
)

// The runner's command and each of its arguments have to become separate argv
// entries. Joining them into one string makes Windows Terminal look for an
// executable literally named `cmd.exe /c echo hi`, which fails with
// "the system cannot find the file specified".
func TestExpandTemplateKeepsArgumentsSeparate(t *testing.T) {
	tmpl := []string{"wt.exe", "-w", "atrium", "new-tab", "--title", "{title}", "-d", "{cwd}", "{cmd}"}
	got := expandTemplate(tmpl, `D:\work`, "smoke", "cmd.exe", []string{"/c", "echo", "hello there"})
	want := []string{
		"wt.exe", "-w", "atrium", "new-tab", "--title", "smoke", "-d", `D:\work`,
		"cmd.exe", "/c", "echo", "hello there",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv wrong.\n got: %#v\nwant: %#v", got, want)
	}
}

func TestExpandTemplateWithNoArguments(t *testing.T) {
	tmpl := []string{"wt.exe", "-d", "{cwd}", "{cmd}"}
	got := expandTemplate(tmpl, `D:\work`, "t", "claude", nil)
	want := []string{"wt.exe", "-d", `D:\work`, "claude"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv wrong.\n got: %#v\nwant: %#v", got, want)
	}
}

// A template that embeds {cmd} inside a longer string wants the joined form,
// because that is a shell being handed a command line.
func TestExpandTemplateJoinsWhenEmbedded(t *testing.T) {
	tmpl := []string{"bash", "-lc", "cd {cwd} && {cmd}"}
	got := expandTemplate(tmpl, "/d/work", "t", "claude", []string{"--resume", "abc def"})
	want := []string{"bash", "-lc", `cd /d/work && claude --resume "abc def"`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv wrong.\n got: %#v\nwant: %#v", got, want)
	}
}

func TestShellJoinQuotesWhatNeedsIt(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"claude"}, "claude"},
		{[]string{"claude", "--resume", "abc"}, "claude --resume abc"},
		{[]string{"C:/Program Files/x.exe", "-a"}, `"C:/Program Files/x.exe" -a`},
		{[]string{"echo", `say "hi"`}, `echo "say \"hi\""`},
	}
	for _, c := range cases {
		if got := shellJoin(c.in); got != c.want {
			t.Errorf("shellJoin(%#v) = %q, want %q", c.in, got, c.want)
		}
	}
}
