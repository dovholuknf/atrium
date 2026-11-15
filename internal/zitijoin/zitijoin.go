// Package zitijoin gets an OpenZiti identity onto this machine with no ziti executable: from a controller URL and an
// IdP sign-in (enroll to cert, or enroll to token), from an uploaded cert and key, or from the device flow of a room
// that has no browser. Every identity lands as its own .json under the directory the caller names.
package zitijoin

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/openziti/identity"
	edge_apis "github.com/openziti/sdk-golang/edge-apis"
	"github.com/openziti/sdk-golang/ziti"
	"github.com/openziti/sdk-golang/ziti/enroll"
)

// Params is what every way in needs.
type Params struct {
	Controller string // the controller URL, with or without a scheme and the /edge/client/v1 path
	Name       string // the entry's name, which names its identity file
	Dir        string // where identities live
	// Pool trusts the controller. Empty means this machine's roots, then the server's own certificate, the way the
	// ziti SDK pins it on enrollment.
	Pool *x509.CertPool
}

// Signer is an external JWT signer the controller lists for sign-in.
type Signer struct {
	Name            string   `json:"name"`
	ExternalAuthURL string   `json:"externalAuthUrl"`
	ClientID        string   `json:"clientId"`
	Audience        string   `json:"audience"`
	Scopes          []string `json:"scopes"`
	Issuer          string   `json:"issuer"`
	EnrollToCert    bool     `json:"enrollToCertEnabled"`
	EnrollToToken   bool     `json:"enrollToTokenEnabled"`
}

var unsafeName = regexp.MustCompile(`[^a-z0-9_-]+`)

func fileBase(name string) string {
	s := strings.Trim(unsafeName.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), ""), "-_")
	if s == "" {
		return "atrium"
	}
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

func rootURL(controller string) (*url.URL, error) {
	c := strings.TrimSpace(controller)
	if c == "" {
		return nil, errors.New("no controller URL")
	}
	if !strings.Contains(c, "://") {
		c = "https://" + c
	}
	u, err := url.Parse(c)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("%q is not a controller URL", controller)
	}
	if u.Scheme != "https" {
		return nil, errors.New("a controller is reached over https")
	}
	return &url.URL{Scheme: "https", Host: u.Host}, nil
}

func clientAPI(u *url.URL) string { return edge_apis.ClientUrl(u.String()) }

func httpClient(pool *x509.CertPool) *http.Client {
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}, Proxy: http.ProxyFromEnvironment},
	}
}

// trust answers the pool that verifies the controller and the CA bundle it hands out.
func trust(p Params, u *url.URL) (*x509.CertPool, []*x509.Certificate, error) {
	if p.Pool != nil {
		return p.Pool, nil, nil
	}
	pool := x509.NewCertPool()
	if sys, err := x509.SystemCertPool(); err == nil && sys != nil {
		pool = sys
	}
	probe, err := http.NewRequest(http.MethodGet, u.String()+"/.well-known/est/cacerts", nil)
	if err != nil {
		return nil, nil, err
	}
	if resp, err := httpClient(pool).Do(probe); err == nil {
		resp.Body.Close()
		return pool, nil, nil
	}
	leaf, err := enroll.FetchServerCert(u.String())
	if err != nil {
		return nil, nil, err
	}
	pinned := x509.NewCertPool()
	pinned.AddCert(leaf)
	return pinned, []*x509.Certificate{leaf}, nil
}

// caBundle is the controller's CAs as PEM, which the identity trusts from then on.
func caBundle(u *url.URL, pool *x509.CertPool, pinned []*x509.Certificate) (string, error) {
	certs := enroll.FetchCertificates(clientAPI(u), pool)
	certs = append(certs, pinned...)
	if len(certs) == 0 {
		leaf, err := enroll.FetchServerCert(u.String())
		if err != nil {
			return "", errors.New("the controller handed out no CA certificates")
		}
		certs = append(certs, leaf)
	}
	var b bytes.Buffer
	for _, c := range certs {
		if err := pem.Encode(&b, &pem.Block{Type: "CERTIFICATE", Bytes: c.Raw}); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

// Signers lists the IdPs the controller accepts, so a join by URL knows where to send the sign-in.
func Signers(ctx context.Context, p Params) ([]Signer, error) {
	u, err := rootURL(p.Controller)
	if err != nil {
		return nil, err
	}
	pool, _, err := trust(p, u)
	if err != nil {
		return nil, err
	}
	var env struct {
		Data []Signer `json:"data"`
	}
	if err := getJSON(ctx, httpClient(pool), clientAPI(u)+"/external-jwt-signers", &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

func getJSON(ctx context.Context, c *http.Client, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", u, resp.Status)
	}
	return json.Unmarshal(body, out)
}

func newKeyAndCSR(cn string) (keyPEM string, csrPEM string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{Country: []string{"US"}, Organization: []string{"NetFoundry"}, CommonName: cn},
	}, key)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr})), nil
}

func postEnrollToken(ctx context.Context, c *http.Client, u *url.URL, token, csrPEM string) (string, error) {
	body, _ := json.Marshal(map[string]string{"clientCsr": csrPEM})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, clientAPI(u)+"/enroll/token", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the controller refused the enrollment: %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var env struct {
		Data struct {
			Cert string `json:"cert"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || env.Data.Cert == "" {
		return "", errors.New("the controller's answer held no certificate")
	}
	return env.Data.Cert, nil
}

func write(p Params, cfg ziti.Config) (string, error) {
	if err := os.MkdirAll(p.Dir, 0o700); err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(p.Dir, fileBase(p.Name)+".json")
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("an identity named %q already exists. pick another name", fileBase(p.Name))
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", err
	}
	return filepath.ToSlash(path), nil
}

// EnrollToCert signs in with an IdP's JWT, gets a certificate for a key made here, and keeps both. It is the
// recommended way in, because nothing needs the IdP again.
func EnrollToCert(ctx context.Context, p Params, token string) (string, error) {
	u, err := rootURL(p.Controller)
	if err != nil {
		return "", err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", errors.New("no sign-in token")
	}
	pool, pinned, err := trust(p, u)
	if err != nil {
		return "", err
	}
	ca, err := caBundle(u, pool, pinned)
	if err != nil {
		return "", err
	}
	keyPEM, csr, err := newKeyAndCSR(fileBase(p.Name))
	if err != nil {
		return "", err
	}
	cert, err := postEnrollToken(ctx, httpClient(pool), u, token, csr)
	if err != nil {
		return "", err
	}
	return write(p, ziti.Config{ZtAPI: clientAPI(u), ID: identity.Config{
		Key: "pem:" + keyPEM, Cert: "pem:" + cert, CA: "pem:" + ca,
	}})
}

// TokenMarker is the key under which an enroll-to-token identity says it holds no credential, so every start of a hub
// or room has to sign in at the IdP.
const TokenMarker = "atriumSignIn"

// EnrollToToken records the controller and its CAs and nothing that authenticates. Every hub and room start then
// needs an IdP sign-in. It is the less safe choice, and callers must say so.
func EnrollToToken(ctx context.Context, p Params, signer string) (string, error) {
	u, err := rootURL(p.Controller)
	if err != nil {
		return "", err
	}
	pool, pinned, err := trust(p, u)
	if err != nil {
		return "", err
	}
	ca, err := caBundle(u, pool, pinned)
	if err != nil {
		return "", err
	}
	path, err := write(p, ziti.Config{ZtAPI: clientAPI(u), ID: identity.Config{CA: "pem:" + ca}})
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(strings.TrimSuffix(path, ".json")+".signin", []byte(strings.TrimSpace(signer)), 0o600)
}

// SignInFile is where an enroll-to-token identity keeps the name of the signer to sign in with.
func SignInFile(identityPath string) string {
	return strings.TrimSuffix(identityPath, ".json") + ".signin"
}

// Import keeps a certificate and key a person already holds, the way Desktop Edge for Windows takes them.
func Import(ctx context.Context, p Params, certPEM, keyPEM string) (string, error) {
	u, err := rootURL(p.Controller)
	if err != nil {
		return "", err
	}
	if err := checkPair(certPEM, keyPEM); err != nil {
		return "", err
	}
	pool, pinned, err := trust(p, u)
	if err != nil {
		return "", err
	}
	ca, err := caBundle(u, pool, pinned)
	if err != nil {
		return "", err
	}
	return write(p, ziti.Config{ZtAPI: clientAPI(u), ID: identity.Config{
		Key: "pem:" + strings.TrimSpace(keyPEM) + "\n", Cert: "pem:" + strings.TrimSpace(certPEM) + "\n", CA: "pem:" + ca,
	}})
}

func checkPair(certPEM, keyPEM string) error {
	cb, _ := pem.Decode([]byte(certPEM))
	if cb == nil {
		return errors.New("that certificate is not PEM")
	}
	crt, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return fmt.Errorf("that certificate is not readable: %w", err)
	}
	if time.Now().After(crt.NotAfter) {
		return fmt.Errorf("that certificate expired at %s", crt.NotAfter.Format(time.RFC3339))
	}
	kb, _ := pem.Decode([]byte(keyPEM))
	if kb == nil {
		return errors.New("that key is not PEM")
	}
	var key crypto.Signer
	switch {
	case strings.Contains(kb.Type, "EC"):
		key, err = x509.ParseECPrivateKey(kb.Bytes)
	case strings.Contains(kb.Type, "RSA"):
		key, err = x509.ParsePKCS1PrivateKey(kb.Bytes)
	default:
		var k any
		k, err = x509.ParsePKCS8PrivateKey(kb.Bytes)
		if s, ok := k.(crypto.Signer); ok {
			key = s
		} else if err == nil {
			err = errors.New("unsupported key type")
		}
	}
	if err != nil {
		return fmt.Errorf("that key is not readable: %w", err)
	}
	type eq interface{ Equal(crypto.PublicKey) bool }
	if pub, ok := key.Public().(eq); !ok || !pub.Equal(crt.PublicKey) {
		return errors.New("that key does not belong to that certificate")
	}
	return nil
}

// EnrollJWT enrolls a one-time .jwt the controller issued, in process.
func EnrollJWT(p Params, jwt string) (string, error) {
	claims, tok, err := enroll.ParseToken(jwt)
	if err != nil {
		return "", fmt.Errorf("that is not an enrollment token: %w", err)
	}
	var alg ziti.KeyAlgVar
	if err := alg.Set("EC"); err != nil {
		return "", err
	}
	cfg, err := enroll.Enroll(enroll.EnrollmentFlags{Token: claims, JwtToken: tok, JwtString: jwt, KeyAlg: alg})
	if err != nil {
		return "", fmt.Errorf("the controller would not enroll it: %w", err)
	}
	return write(p, *cfg)
}
