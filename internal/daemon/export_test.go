package daemon

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// What must never leave this machine.
//
// Every test here is about absence, which is the awkward kind to write and the
// only kind that matters: a secret in an export is still in the repository's
// history after somebody deletes it, so this gets one chance to be right on a
// machine nobody is watching.

// exportOf builds an export and hands back both the document and its bytes,
// because most of these assert on the SERIALISED form. What matters is what
// lands in the file, not what the struct held on the way there.
func exportOf(t *testing.T, d *Daemon) (*Export, string) {
	t.Helper()
	out, err := d.BuildExport()
	if err != nil {
		t.Fatalf("export refused: %v", err)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	return out, string(raw)
}

// A zrok account token is reusable on every machine and grants everything that
// account can do. It is the single worst thing that could end up in a commit.
func TestAZrokAccountTokenNeverLeaves(t *testing.T) {
	d := testDaemon(t)
	// Stored the way the daemon really stores it, so this cannot pass against
	// a shape the real code would never have written.
	cfg := d.zrokConfig()
	cfg.OwnEnvironment = true
	cfg.ApiEndpoint = "https://zrok.example"
	cfg.ShareToken = "sharetokensecret"
	cfg.Name = "atrium-abc123def456"
	if err := d.saveOverlayConfig(SettingOverlayZrok, cfg); err != nil {
		t.Fatal(err)
	}

	_, raw := exportOf(t, d)
	for _, secret := range []string{"sharetokensecret", "atrium-abc123def456"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("%q is in the export", secret)
		}
	}
	// And the parts that ARE configuration came through, or the export is safe
	// by being empty, which is not the same thing.
	if !strings.Contains(raw, "https://zrok.example") {
		t.Fatal("the instance is missing, so the export cannot rebuild a machine")
	}
}

// An identity is a private key. The configured value is a path, and exporting
// a path puts the location of a key in a repository and invites a second
// machine to be pointed at the same one.
func TestAZitiIdentityPathNeverLeaves(t *testing.T) {
	d := testDaemon(t)
	cfg := d.zitiConfig()
	cfg.Identity = "C:/Users/someone/.atrium/identities/atrium-secret.json"
	cfg.Service = "atrium-board"
	if err := d.saveOverlayConfig(SettingOverlayZiti, cfg); err != nil {
		t.Fatal(err)
	}

	_, raw := exportOf(t, d)
	if strings.Contains(raw, "identities") || strings.Contains(raw, "atrium-secret") {
		t.Fatalf("the identity path is in the export:\n%s", raw)
	}
	if !strings.Contains(raw, "atrium-board") {
		t.Fatal("the service name is missing, and that half is public and useful")
	}
}

// THE ONE THAT KEEPS WORKING IN A YEAR.
//
// Somebody adds a field to ZrokConfig. They do not read the export code,
// because why would they. This asserts that whatever they added is absent,
// without naming it, by comparing what the export declares against what the
// stored config holds.
//
// It fails when a field is added to the STORED type and the exporter is left
// alone, which is the safe direction and is meant to be a prompt rather than a
// problem: add the field to ExportZrok on purpose, or add it to the list below
// as one more thing that stays here.
func TestAFieldNobodyAllowlistedDoesNotAppear(t *testing.T) {
	// Every field on the stored config, and what it is.
	held := map[string]bool{} // name -> may it leave
	for _, f := range []struct {
		name  string
		leave bool
	}{
		{"Mode", false},          // derived from Public and Private, so redundant
		{"Public", true},         //
		{"Private", true},        //
		{"ShareToken", false},    // an address somebody may be holding
		{"Name", false},          // the same, reserved
		{"Backend", false},       // this machine's own listening address
		{"Extra", false},         // free text passed to zrok, so anything
		{"OwnEnvironment", true}, //
		{"ApiEndpoint", true},    //
	} {
		held[f.name] = f.leave
	}

	stored := reflect.TypeOf(ZrokConfig{})
	for i := 0; i < stored.NumField(); i++ {
		name := stored.Field(i).Name
		if _, known := held[name]; !known {
			t.Fatalf("ZrokConfig grew a field %q and nobody decided whether it may be "+
				"exported. Add it to this test, and to ExportZrok only if it is "+
				"configuration rather than a credential or an address.", name)
		}
	}

	exported := reflect.TypeOf(ExportZrok{})
	for i := 0; i < exported.NumField(); i++ {
		name := exported.Field(i).Name
		if !held[name] {
			t.Fatalf("ExportZrok exports %q, which is not marked as safe to leave", name)
		}
	}
}

// A settings key nobody allowlisted stays here. The settings table is where a
// value with no home ends up, and the next thing put in it will not be
// considered against the export.
func TestAnUnknownSettingIsNotExported(t *testing.T) {
	d := testDaemon(t)
	if err := d.st.SetSetting("something_invented_later", "a-secret-value"); err != nil {
		t.Fatal(err)
	}
	out, raw := exportOf(t, d)
	if strings.Contains(raw, "a-secret-value") {
		t.Fatal("a setting nobody allowlisted was exported")
	}
	if _, ok := out.Settings["something_invented_later"]; ok {
		t.Fatal("an unknown setting key is in the export")
	}
}

// The two overlay configs live in the settings table as JSON blobs. Exporting
// the settings table generically would carry both wholesale, which is every
// credential atrium holds.
func TestTheOverlayBlobsAreNotExportedAsSettings(t *testing.T) {
	d := testDaemon(t)
	for _, key := range NeverExportedKeys() {
		for _, allowed := range ExportSettingKeys() {
			if key == allowed {
				t.Fatalf("%q is on both lists", key)
			}
		}
	}
	cfg := d.zrokConfig()
	cfg.ShareToken = "blobbedsecret"
	if err := d.saveOverlayConfig(SettingOverlayZrok, cfg); err != nil {
		t.Fatal(err)
	}
	_, raw := exportOf(t, d)
	if strings.Contains(raw, "blobbedsecret") {
		t.Fatal("the zrok settings blob was exported wholesale")
	}
}

// Global auto is not a secret and still must not travel. It is a STATE: whether
// this machine is approving everything right now. Restoring it onto another
// machine is the worst thing an import could silently do.
func TestGlobalAutoIsNotExported(t *testing.T) {
	d := testDaemon(t)
	if err := d.st.SetSetting(store.SettingGlobalAuto, "on"); err != nil {
		t.Fatal(err)
	}
	out, _ := exportOf(t, d)
	if _, ok := out.Settings[store.SettingGlobalAuto]; ok {
		t.Fatal("whether this machine is approving everything was exported")
	}
}

// The second defence. A source is an operator-authored command line, and a
// command line is where somebody puts a token. The allowlist cannot help,
// because the source itself is legitimately exported.
func TestACredentialTypedIntoASourceStopsTheExport(t *testing.T) {
	for _, tc := range []struct{ name, secret string }{
		{"a github token", "ghp_abcdefghijklmnopqrstuvwxyz0123456789"},
		{"a private key", "-----BEGIN RSA PRIVATE KEY-----"},
		{"a long blob", "QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVowMTIzNDU2Nzg5YWJjZGVm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := testDaemon(t)
			if _, err := d.st.SaveSource(store.Source{
				ID: "leaky", Label: "leaky", Enabled: true,
				Cmd:          "gh",
				Args:         []string{"issue", "list", "--token", tc.secret},
				IntervalSecs: store.MinSourceInterval,
			}); err != nil {
				t.Fatal(err)
			}
			_, err := d.BuildExport()
			if err == nil {
				t.Fatal("an export carrying a credential was allowed")
			}
			// And the message must not print the thing it is refusing to print.
			if strings.Contains(err.Error(), tc.secret) {
				t.Fatalf("the refusal echoed the credential: %v", err)
			}
			if !strings.Contains(err.Error(), "history") {
				t.Fatalf("the refusal does not say why it matters: %v", err)
			}
		})
	}
}

// An ordinary configuration exports. A guard that refuses everything is safe
// and useless, and this is the test that would fail if the secret shapes were
// tightened until nothing could get out.
func TestAnOrdinaryConfigurationExportsCleanly(t *testing.T) {
	d := testDaemon(t)
	if _, err := d.st.SaveSource(store.Source{
		ID: "issues", Label: "my issues", Enabled: true,
		Cmd:          "gh",
		Args:         []string{"issue", "list", "--json", "number,title"},
		IntervalSecs: store.MinSourceInterval,
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetSetting(store.SettingSweepDead, "3600"); err != nil {
		t.Fatal(err)
	}
	out, raw := exportOf(t, d)
	if out.Settings[store.SettingSweepDead] != "3600" {
		t.Fatalf("an allowlisted setting did not come through: %+v", out.Settings)
	}
	if !strings.Contains(raw, "my issues") {
		t.Fatal("a source did not come through")
	}
	if out.Version != ExportVersion {
		t.Fatalf("version is %d", out.Version)
	}
}
