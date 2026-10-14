//go:build windows

package detach

import "testing"

func TestOutsideJobCommandLine(t *testing.T) {
	got := outsideJobCommandLine(`C:\a b\atrium.ptyhost.exe`, []string{"ptyhost", "--state-dir", `C:\s d`}, `C:\s d\ptyhost.log`)
	want := `cmd.exe /c ""C:\a b\atrium.ptyhost.exe" "ptyhost" "--state-dir" "C:\s d" >> "C:\s d\ptyhost.log" 2>&1"`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if got := outsideJobCommandLine("x.exe", nil, ""); got != `cmd.exe /c ""x.exe" >NUL 2>&1"` {
		t.Fatalf("no log: %s", got)
	}
}
