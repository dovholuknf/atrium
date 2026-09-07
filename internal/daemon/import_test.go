package daemon

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// Importing is the direction that can lose something.
//
// An export that is missing a field costs a rebuild. An import that overwrites
// one costs whatever it overwrote, and the operator finds out later, which is
// why every test here is about what an import DID NOT do.

// A dry run is the default, and it has to write nothing at all. It is the
// question somebody restoring a machine actually asks, and an answer that
// arrives by doing it is not an answer.
func TestADryRunChangesNothing(t *testing.T) {
	d := testDaemon(t)
	in := &Export{
		Version:  ExportVersion,
		Settings: map[string]string{store.SettingSweepDead: "999"},
	}
	res, err := d.ApplyImport(in, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied {
		t.Fatal("a dry run reported itself as applied")
	}
	if len(res.Changes) == 0 {
		t.Fatal("a dry run said nothing would change, so it is not answering the question")
	}
	got, err := d.st.Setting(store.SettingSweepDead)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("a dry run wrote %q", got)
	}
}

// THE ONE THAT MATTERS. Restoring onto a machine that is already configured
// must not quietly replace what is there.
func TestAnImportKeepsWhatIsAlreadySet(t *testing.T) {
	d := testDaemon(t)
	if err := d.st.SetSetting(store.SettingSweepDead, "mine"); err != nil {
		t.Fatal(err)
	}
	in := &Export{
		Version:  ExportVersion,
		Settings: map[string]string{store.SettingSweepDead: "theirs"},
	}
	res, err := d.ApplyImport(in, true, false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.st.Setting(store.SettingSweepDead)
	if err != nil {
		t.Fatal(err)
	}
	if got != "mine" {
		t.Fatalf("an import overwrote a setting that was already there: %q", got)
	}
	if res.Kept == 0 {
		t.Fatal("what was kept was not reported, so nobody would know it was skipped")
	}
	// And it says how to do it anyway, or the operator is stuck.
	var said bool
	for _, c := range res.Changes {
		if c.Action == "keep" && strings.Contains(c.Why, "force") {
			said = true
		}
	}
	if !said {
		t.Fatalf("nothing said how to overwrite it: %+v", res.Changes)
	}
}

// Forcing does overwrite, or the escape hatch is not one.
func TestForceOverwrites(t *testing.T) {
	d := testDaemon(t)
	if err := d.st.SetSetting(store.SettingSweepDead, "mine"); err != nil {
		t.Fatal(err)
	}
	in := &Export{
		Version:  ExportVersion,
		Settings: map[string]string{store.SettingSweepDead: "theirs"},
	}
	if _, err := d.ApplyImport(in, true, true); err != nil {
		t.Fatal(err)
	}
	got, err := d.st.Setting(store.SettingSweepDead)
	if err != nil {
		t.Fatal(err)
	}
	if got != "theirs" {
		t.Fatalf("force did not overwrite: %q", got)
	}
}

// THE WORST THING AN IMPORT COULD DO, and it is not obvious.
//
// The exported overlay shape has no account token in it, because the export
// refuses to carry one. Writing that shape over the stored config would erase
// the token with a value that was deliberately absent, and the machine would
// look configured and refuse to share. So the overlay is MERGED.
func TestAnImportDoesNotEraseTheZrokToken(t *testing.T) {
	d := testDaemon(t)
	cfg := d.zrokConfig()
	cfg.ShareToken = "a-token-that-must-survive"
	cfg.Name = "atrium-reservedname"
	cfg.Private, cfg.Public = true, false
	if err := d.saveOverlayConfig(SettingOverlayZrok, cfg); err != nil {
		t.Fatal(err)
	}

	in := &Export{Version: ExportVersion}
	in.Overlays.Zrok.Public = true
	in.Overlays.Zrok.ApiEndpoint = "https://zrok.example"
	if _, err := d.ApplyImport(in, true, true); err != nil {
		t.Fatal(err)
	}

	after := d.zrokConfig()
	if after.ShareToken != "a-token-that-must-survive" {
		t.Fatalf("the share token was erased by an import: %q", after.ShareToken)
	}
	if after.Name != "atrium-reservedname" {
		t.Fatalf("the reserved address was erased by an import: %q", after.Name)
	}
	// And the parts the file DID carry were applied, or the merge is a no-op.
	if !after.Public {
		t.Fatal("the imported sharing option was not applied")
	}
	if after.ApiEndpoint != "https://zrok.example" {
		t.Fatalf("the imported instance was not applied: %q", after.ApiEndpoint)
	}
}

// The same, for the ziti identity. An identity path points at a private key.
func TestAnImportDoesNotEraseTheZitiIdentity(t *testing.T) {
	d := testDaemon(t)
	cfg := d.zitiConfig()
	cfg.Identity = "C:/keys/atrium.json"
	cfg.Service = "old-service"
	if err := d.saveOverlayConfig(SettingOverlayZiti, cfg); err != nil {
		t.Fatal(err)
	}

	in := &Export{Version: ExportVersion}
	in.Overlays.Ziti.Service = "new-service"
	if _, err := d.ApplyImport(in, true, true); err != nil {
		t.Fatal(err)
	}

	after := d.zitiConfig()
	if after.Identity != "C:/keys/atrium.json" {
		t.Fatalf("the identity was erased by an import: %q", after.Identity)
	}
	if after.Service != "new-service" {
		t.Fatalf("the service name was not applied: %q", after.Service)
	}
}

// A hand-edited file does not get to name a setting the export itself refuses
// to carry. The import list and the export list are the same list.
func TestAnImportRefusesASettingTheExportWouldNotCarry(t *testing.T) {
	d := testDaemon(t)
	in := &Export{
		Version: ExportVersion,
		Settings: map[string]string{
			store.SettingGlobalAuto: "on",
			SettingOverlayZrok:      `{"share_token":"sneaky"}`,
		},
	}
	if _, err := d.ApplyImport(in, true, true); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{store.SettingGlobalAuto, SettingOverlayZrok} {
		got, err := d.st.Setting(key)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(got, "on") && key == store.SettingGlobalAuto {
			t.Fatal("a hand-edited file turned on approving everything")
		}
		if strings.Contains(got, "sneaky") {
			t.Fatalf("a hand-edited file wrote the overlay blob: %q", got)
		}
	}
}

// A version this daemon does not understand is refused whole. Half a
// configuration is worse than none, because it looks like it worked.
func TestAnUnknownVersionIsRefusedWhole(t *testing.T) {
	d := testDaemon(t)
	in := &Export{
		Version:  ExportVersion + 41,
		Settings: map[string]string{store.SettingSweepDead: "999"},
	}
	if _, err := d.ApplyImport(in, true, true); err == nil {
		t.Fatal("a configuration from the future was applied")
	}
	got, err := d.st.Setting(store.SettingSweepDead)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("a refused import still wrote something: %q", got)
	}
}

// Round trip. Export a configured machine, import onto an empty one, and the
// second should end up saying the same things as the first.
func TestExportThenImportCarriesTheConfiguration(t *testing.T) {
	from := testDaemon(t)
	if err := from.st.SetSetting(store.SettingSweepDead, "7200"); err != nil {
		t.Fatal(err)
	}
	if _, err := from.st.SaveSource(store.Source{
		ID: "issues", Label: "my issues", Enabled: true,
		Cmd: "gh", Args: []string{"issue", "list"},
		IntervalSecs: store.MinSourceInterval,
	}); err != nil {
		t.Fatal(err)
	}
	out, err := from.BuildExport()
	if err != nil {
		t.Fatal(err)
	}

	to := testDaemon(t)
	if _, err := to.ApplyImport(out, true, false); err != nil {
		t.Fatal(err)
	}
	got, err := to.st.Setting(store.SettingSweepDead)
	if err != nil {
		t.Fatal(err)
	}
	if got != "7200" {
		t.Fatalf("the setting did not survive the round trip: %q", got)
	}
	src, err := to.st.SourceByID("issues")
	if err != nil || src == nil {
		t.Fatalf("the source did not survive the round trip: %v", err)
	}
	if src.Label != "my issues" {
		t.Fatalf("the source came back as %q", src.Label)
	}
}

// A BROUGHT THEME IS CONFIGURATION. A machine rebuilt from a checkout that
// restores every fixture and then draws them all in atrium's own blue has
// restored the work and lost the thing the operator notices first.
func TestABroughtThemeSurvivesTheRoundTrip(t *testing.T) {
	from := testDaemon(t)
	mine := store.TermTheme{Name: "mine", Palette: store.Palette{
		Background: "#0c0c0c", Foreground: "#cccccc",
		Black: "#0c0c0c", Red: "#c50f1f", Green: "#13a10e", Yellow: "#c19c00",
		Blue: "#0037da", Magenta: "#881798", Cyan: "#3a96dd", White: "#cccccc",
		BrightBlack: "#767676", BrightRed: "#e74856", BrightGreen: "#16c60c",
		BrightYellow: "#f9f1a5", BrightBlue: "#3b78ff", BrightMagenta: "#b4009e",
		BrightCyan: "#61d6d6", BrightWhite: "#f2f2f2",
	}}
	if _, err := from.st.SaveTermTheme(mine); err != nil {
		t.Fatal(err)
	}
	out, err := from.BuildExport()
	if err != nil {
		// The export refuses itself when anything in it looks like a credential,
		// and a theme is twenty one hex strings and a short name. If this ever
		// fires, the name rule and the secret scanner have drifted into each
		// other: see `themeName`.
		t.Fatalf("exporting a machine with a brought theme on it failed: %v", err)
	}

	to := testDaemon(t)
	if _, err := to.ApplyImport(out, true, false); err != nil {
		t.Fatal(err)
	}
	got, err := to.st.TermThemeByName("mine")
	if err != nil || got == nil {
		t.Fatalf("the theme did not survive the round trip: %v", err)
	}
	if got.Palette.Magenta != "#881798" {
		t.Fatalf("the palette came back as %+v", got.Palette)
	}
}

// An imported configuration goes through the same validator the editor does, so
// a file edited by hand cannot put something that is not a colour into the
// table by arriving as configuration instead of through the editor.
func TestAnImportedThemeIsStillValidated(t *testing.T) {
	d := testDaemon(t)
	in := &Export{
		Version: ExportVersion,
		Themes: []store.TermTheme{{Name: "bad", Palette: store.Palette{
			Background: "#000000", Foreground: "url(javascript:alert(1))",
		}}},
	}
	if _, err := d.ApplyImport(in, true, false); err == nil {
		t.Fatal("a configuration file put a value that is not a colour into the theme table")
	}
	got, err := d.st.TermThemeByName("bad")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatal("the refused theme was written anyway")
	}
}
