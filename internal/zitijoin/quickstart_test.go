//go:build quickstart

package zitijoin

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/openziti/sdk-golang/ziti"
)

// Runs against a throwaway `ziti edge quickstart`: ZITI_QS_CTRL is its host:port, ZITI_QS_JWT a one-time .jwt, and
// ZITI_QS_IDP the base URL of an IdP whose signer the controller lists.
func env(t *testing.T, k string) string {
	t.Helper()
	v := os.Getenv(k)
	if v == "" {
		t.Skip(k + " is not set")
	}
	return v
}

func authenticates(t *testing.T, path string) {
	t.Helper()
	cfg, err := ziti.NewConfigFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ziti.NewContext(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Authenticate(); err != nil {
		t.Fatalf("%s did not authenticate: %v", path, err)
	}
}

func TestQuickstart(t *testing.T) {
	ctrl := env(t, "ZITI_QS_CTRL")
	jwt, _ := os.ReadFile(env(t, "ZITI_QS_JWT"))
	dir := t.TempDir()
	ctx := context.Background()

	var jwtPath string
	t.Run("enroll by jwt", func(t *testing.T) {
		var err error
		jwtPath, err = EnrollJWT(Params{Name: "byjwt", Dir: dir}, string(jwt))
		if err != nil {
			t.Fatal(err)
		}
		authenticates(t, jwtPath)
	})

	t.Run("import cert and key", func(t *testing.T) {
		raw, _ := os.ReadFile(jwtPath)
		var id struct {
			ID struct{ Cert, Key string } `json:"id"`
		}
		if err := json.Unmarshal(raw, &id); err != nil {
			t.Fatal(err)
		}
		path, err := Import(ctx, Params{Controller: ctrl, Name: "imported", Dir: dir},
			strings.TrimPrefix(id.ID.Cert, "pem:"), strings.TrimPrefix(id.ID.Key, "pem:"))
		if err != nil {
			t.Fatal(err)
		}
		authenticates(t, path)
	})

	t.Run("device flow then enroll to cert", func(t *testing.T) {
		var shown Prompt
		path, err := Join(ctx, Params{Dir: dir}, JoinURL{Controller: ctrl, Service: "s", Room: "devroom", Signer: "idp"},
			func(p Prompt) { shown = p; t.Logf("room shows: open %s and enter %s", p.URL, p.Code) })
		if err != nil {
			t.Fatal(err)
		}
		if shown.Code == "" {
			t.Fatal("nothing was shown")
		}
		authenticates(t, path)
	})

	t.Run("enroll to cert with a token", func(t *testing.T) {
		idp := env(t, "ZITI_QS_IDP")
		tok, err := DeviceFlow(ctx, nil, idp, "atrium-test", nil, func(Prompt) {})
		if err != nil {
			t.Fatal(err)
		}
		path, err := EnrollToCert(ctx, Params{Controller: ctrl, Name: "tocert", Dir: dir}, tok)
		if err != nil {
			t.Fatal(err)
		}
		authenticates(t, path)
	})
}
