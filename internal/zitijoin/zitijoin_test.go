package zitijoin

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func selfSigned(t *testing.T) (certPEM, keyPEM string, key *ecdsa.PrivateKey) {
	t.Helper()
	key, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "x"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := x509.MarshalECPrivateKey(key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd})), key
}

func controller(t *testing.T, issued string) (*httptest.Server, Params) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/enroll/token"):
			if r.Header.Get("Authorization") != "Bearer good" {
				http.Error(w, `{"error":"no"}`, http.StatusUnauthorized)
				return
			}
			var b struct{ ClientCsr string }
			_ = json.NewDecoder(r.Body).Decode(&b)
			if !strings.Contains(b.ClientCsr, "CERTIFICATE REQUEST") {
				http.Error(w, "no csr", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"cert": issued}})
		case strings.HasSuffix(r.URL.Path, "/external-jwt-signers"):
			_, _ = w.Write([]byte(`{"data":[{"name":"idp","clientId":"c","externalAuthUrl":"https://idp"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return srv, Params{Controller: srv.URL, Name: "Net 2!", Dir: t.TempDir(), Pool: pool}
}

func TestEnrollToCertWritesAnIdentityWithItsOwnKey(t *testing.T) {
	t.Parallel()
	cert, _, _ := selfSigned(t)
	_, p := controller(t, cert)
	path, err := EnrollToCert(context.Background(), p, "good")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, "/net2.json") {
		t.Fatalf("path %s", path)
	}
	raw, _ := os.ReadFile(path)
	for _, want := range []string{"pem:-----BEGIN EC PRIVATE KEY", "pem:-----BEGIN CERTIFICATE", `"ztAPI"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("identity lacks %s: %s", want, raw)
		}
	}
	if _, err := EnrollToCert(context.Background(), p, "good"); err == nil {
		t.Fatal("a second identity overwrote the first")
	}
}

func TestEnrollToCertRefusedTokenWritesNothing(t *testing.T) {
	t.Parallel()
	cert, _, _ := selfSigned(t)
	_, p := controller(t, cert)
	if _, err := EnrollToCert(context.Background(), p, "bad"); err == nil {
		t.Fatal("a bad token enrolled")
	}
	if ents, _ := os.ReadDir(p.Dir); len(ents) != 0 {
		t.Fatalf("left %d files", len(ents))
	}
}

func TestEnrollToTokenHoldsNoCredential(t *testing.T) {
	t.Parallel()
	_, p := controller(t, "")
	path, err := EnrollToToken(context.Background(), p, "idp")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "PRIVATE KEY") || strings.Contains(string(raw), `"cert": "pem`) {
		t.Fatalf("a token identity holds a credential: %s", raw)
	}
	if b, _ := os.ReadFile(SignInFile(path)); string(b) != "idp" {
		t.Fatalf("signer not kept: %q", b)
	}
}

func TestImportChecksThePair(t *testing.T) {
	t.Parallel()
	cert, key, _ := selfSigned(t)
	_, otherKey, _ := selfSigned(t)
	_, p := controller(t, "")
	if _, err := Import(context.Background(), p, cert, otherKey); err == nil || !strings.Contains(err.Error(), "belong") {
		t.Fatalf("mismatched pair accepted: %v", err)
	}
	if _, err := Import(context.Background(), p, cert, key); err != nil {
		t.Fatal(err)
	}
}

func TestSignersListsTheControllersIdPs(t *testing.T) {
	t.Parallel()
	_, p := controller(t, "")
	got, err := Signers(context.Background(), p)
	if err != nil || len(got) != 1 || got[0].ClientID != "c" {
		t.Fatalf("%v %v", got, err)
	}
}

func TestControllerMustBeHTTPS(t *testing.T) {
	t.Parallel()
	if _, err := rootURL("http://x"); err == nil {
		t.Fatal("http accepted")
	}
	if u, err := rootURL("ctrl.example.com:1280/edge/client/v1"); err != nil || u.String() != "https://ctrl.example.com:1280" {
		t.Fatalf("%v %v", u, err)
	}
}

func TestDeviceFlowPollsUntilApproved(t *testing.T) {
	t.Parallel()
	polls := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_, _ = w.Write([]byte(`{"device_authorization_endpoint":"` + srv.URL + `/dev","token_endpoint":"` + srv.URL + `/tok"}`))
		case "/dev":
			_, _ = w.Write([]byte(`{"device_code":"d","user_code":"ABCD","verification_uri":"` + srv.URL + `/v","interval":1,"expires_in":30}`))
		case "/tok":
			polls++
			if polls < 2 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id_token":"jwt"}`))
		}
	}))
	defer srv.Close()
	var shown Prompt
	tok, err := DeviceFlow(context.Background(), srv.Client(), srv.URL, "c", nil, func(p Prompt) { shown = p })
	if err != nil || tok != "jwt" || shown.Code != "ABCD" || polls != 2 {
		t.Fatalf("tok=%q err=%v shown=%+v polls=%d", tok, err, shown, polls)
	}
}

func TestParseJoinURL(t *testing.T) {
	t.Parallel()
	j, err := ParseJoinURL("ziti://c.example.com:1280/atrium-link?room=lab&signer=idp")
	if err != nil || j.Controller != "c.example.com:1280" || j.Service != "atrium-link" || j.Room != "lab" || j.Signer != "idp" {
		t.Fatalf("%+v %v", j, err)
	}
	if _, err := ParseJoinURL("ziti://c/svc"); err == nil {
		t.Fatal("a join URL with no room was accepted")
	}
	if IsJoinURL("atrium-join:abc") || !IsJoinURL(" ziti://x") {
		t.Fatal("IsJoinURL")
	}
}

func TestJoinSignsInByDeviceFlowThenEnrollsToCert(t *testing.T) {
	t.Parallel()
	cert, _, _ := selfSigned(t)
	var idp *httptest.Server
	idp = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_, _ = w.Write([]byte(`{"device_authorization_endpoint":"` + idp.URL + `/dev","token_endpoint":"` + idp.URL + `/tok"}`))
		case "/dev":
			_, _ = w.Write([]byte(`{"device_code":"d","user_code":"WXYZ","verification_uri":"` + idp.URL + `/v","interval":1,"expires_in":30}`))
		case "/tok":
			_, _ = w.Write([]byte(`{"id_token":"good"}`))
		}
	}))
	defer idp.Close()
	ctrl := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/external-jwt-signers"):
			_, _ = w.Write([]byte(`{"data":[{"name":"idp","clientId":"c","issuer":"` + idp.URL + `"}]}`))
		case strings.HasSuffix(r.URL.Path, "/enroll/token"):
			if r.Header.Get("Authorization") != "Bearer good" {
				http.Error(w, "no", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"cert": cert}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ctrl.Close()
	pool := x509.NewCertPool()
	pool.AddCert(ctrl.Certificate())
	var shown Prompt
	path, err := Join(context.Background(), Params{Dir: t.TempDir(), Pool: pool},
		JoinURL{Controller: strings.TrimPrefix(ctrl.URL, "https://"), Service: "s", Room: "lab"},
		func(p Prompt) { shown = p })
	if err != nil || shown.Code != "WXYZ" || !strings.HasSuffix(path, "/lab.json") {
		t.Fatalf("path=%q err=%v shown=%+v", path, err, shown)
	}
}
