package link

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"math/big"
	"strings"
	"testing"
	"time"
)

func unb64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := b64.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// decryptPush is the browser's half, for the tests: it reads an aes128gcm body with the user agent's private key and
// auth secret.
func decryptPush(t *testing.T, body []byte, ua *ecdh.PrivateKey, authSecret []byte) []byte {
	t.Helper()
	salt := body[:16]
	idlen := int(body[20])
	asPublic := body[21 : 21+idlen]
	if rs := binary.BigEndian.Uint32(body[16:20]); rs != pushRecord {
		t.Fatalf("record size %d", rs)
	}
	asKey, err := ecdh.P256().NewPublicKey(asPublic)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := ua.ECDH(asKey)
	if err != nil {
		t.Fatal(err)
	}
	prkKey, _ := hkdf.Extract(sha256.New, secret, authSecret)
	keyInfo := append(append([]byte("WebPush: info\x00"), ua.PublicKey().Bytes()...), asPublic...)
	ikm, _ := hkdf.Expand(sha256.New, prkKey, string(keyInfo), 32)
	prk, _ := hkdf.Extract(sha256.New, ikm, salt)
	cek, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	rec, err := gcm.Open(nil, nonce, body[21+idlen:], nil)
	if err != nil {
		t.Fatalf("the body does not decrypt: %v", err)
	}
	// Strip the padding: zeros, then the 0x02 delimiter.
	i := len(rec) - 1
	for i >= 0 && rec[i] == 0 {
		i--
	}
	if i < 0 || rec[i] != 0x02 {
		t.Fatal("no last-record delimiter")
	}
	return rec[:i]
}

// TestRFC8291AppendixA is the worked example in RFC 8291 appendix A. With the example's keys and salt the body is
// byte for byte the RFC's, and the RFC's body decrypts to its plaintext.
func TestRFC8291AppendixA(t *testing.T) {
	plain := "When I grow up, I want to be a watermelon"
	asPriv := unb64(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw")
	uaPriv := unb64(t, "q1dXpw3UpT5VOmu_cf_v6ih07Aems3njxI-JWgLcM94")
	uaPub := unb64(t, "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4")
	auth := unb64(t, "BTBZMqHH6r4Tts7J_aSIgg")
	salt := unb64(t, "DGv6ra1nlYgDCS1FRnbzlw")
	want := unb64(t, "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6Tlz"+
		"AC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN")

	eph, err := ecdh.P256().NewPrivateKey(asPriv)
	if err != nil {
		t.Fatal(err)
	}
	got, err := encryptPushWith([]byte(plain), uaPub, auth, eph, salt, 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("the body is not the RFC's\n got %x\nwant %x", got, want)
	}
	ua, err := ecdh.P256().NewPrivateKey(uaPriv)
	if err != nil {
		t.Fatal(err)
	}
	if out := decryptPush(t, want, ua, auth); string(out) != plain {
		t.Fatalf("the RFC's body decrypts to %q", out)
	}
}

func TestEncryptPushPadsToTheBlock(t *testing.T) {
	ua, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	for _, plain := range []string{"x", strings.Repeat("a", 200), strings.Repeat("b", 300)} {
		body, err := encryptPush([]byte(plain), ua.PublicKey().Bytes(), auth)
		if err != nil {
			t.Fatal(err)
		}
		// header 16+4+1+65, tag 16
		rec := len(body) - 86 - 16
		if rec%pushPadTo != 0 {
			t.Fatalf("%d bytes of record is not a multiple of %d", rec, pushPadTo)
		}
		if got := decryptPush(t, body, ua, auth); string(got) != plain {
			t.Fatalf("round trip gave %q", got)
		}
	}
	if _, err := encryptPush([]byte(strings.Repeat("z", 5000)), ua.PublicKey().Bytes(), auth); err == nil {
		t.Fatal("a payload past one record was accepted")
	}
	if _, err := encryptPush([]byte("x"), []byte("not a point"), auth); err == nil {
		t.Fatal("a bad p256dh was accepted")
	}
}

func TestVAPIDHeaderVerifies(t *testing.T) {
	stored, err := newVAPIDKey()
	if err != nil {
		t.Fatal(err)
	}
	k, err := parseVAPIDKey(stored)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1_800_000_000, 0)
	h, err := vapidHeader(k, "https://fcm.googleapis.com/fcm/send/abc", "https://example.invalid/atrium", at)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "vapid t=") {
		t.Fatalf("header %q", h)
	}
	parts := strings.SplitN(strings.TrimPrefix(h, "vapid t="), ", k=", 2)
	if len(parts) != 2 {
		t.Fatalf("header %q", h)
	}
	if want, _ := vapidPublic(k); parts[1] != want {
		t.Fatal("k is not the public key")
	}
	if err := verifyVAPID(parts[0], parts[1], "https://fcm.googleapis.com", "https://example.invalid/atrium", at); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h, stored) {
		t.Fatal("the private key is in the header")
	}
}

// verifyVAPID checks a JWT the way a push service would.
func verifyVAPID(jwt, pub, aud, sub string, at time.Time) error {
	segs := strings.Split(jwt, ".")
	if len(segs) != 3 {
		return vapidErr("not three segments")
	}
	raw, err := b64.DecodeString(pub)
	if err != nil || len(raw) != 65 {
		return vapidErr("k is not a 65 byte point")
	}
	x, y := new(big.Int).SetBytes(raw[1:33]), new(big.Int).SetBytes(raw[33:])
	sig, err := b64.DecodeString(segs[2])
	if err != nil || len(sig) != 64 {
		return vapidErr("the signature is not 64 bytes")
	}
	digest := sha256.Sum256([]byte(segs[0] + "." + segs[1]))
	key := ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}
	if !ecdsa.Verify(&key, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		return vapidErr("the signature does not verify")
	}
	claims := string(unbStr(segs[1]))
	for _, want := range []string{`"aud":"` + aud + `"`, `"sub":"` + sub + `"`,
		`"exp":` + itoaN(int(at.Add(12*time.Hour).Unix()))} {
		if !strings.Contains(claims, want) {
			return vapidErr("claims lack " + want + ": " + claims)
		}
	}
	if h := string(unbStr(segs[0])); !strings.Contains(h, `"ES256"`) {
		return vapidErr("header " + h)
	}
	return nil
}

type vapidErr string

func (e vapidErr) Error() string { return string(e) }

func unbStr(s string) []byte { b, _ := b64.DecodeString(s); return b }

func TestPushEndpointAllowlist(t *testing.T) {
	cases := []struct {
		url string
		ok  bool
	}{
		{"https://fcm.googleapis.com/fcm/send/abc", true},
		{"https://FCM.googleapis.com/fcm/send/abc", true},
		{"https://web.push.apple.com/QGx", true},
		{"https://updates.push.services.mozilla.com/wpush/v2/gAAAA", true},
		{"https://db5p.notify.windows.com/w/?token=x", true},
		{"https://fcm.googleapis.com:443/fcm/send/abc", true},
		{"https://evilpush.apple.com.example/x", false},
		{"https://xpush.apple.com/x", false},
		{"https://push.apple.com/x", false},
		{"https://user@fcm.googleapis.com/x", false},
		{"https://user:pw@web.push.apple.com/x", false},
		{"https://fcm.googleapis.com:8443/x", false},
		{"https://fcm.googleapis.com./x", false},
		{"https://fcm.googleapis.com.evil.example/x", false},
		{"https://evil.example/fcm.googleapis.com", false},
		{"https://127.0.0.1/x", false},
		{"https://[::1]/x", false},
		{"https://[::1]:443/x", false},
		{"https://10.0.0.1:443/x", false},
		{"http://fcm.googleapis.com/x", false},
		{"//fcm.googleapis.com/x", false},
		{"fcm.googleapis.com/x", false},
		{"https:///x", false},
		{"", false},
		{"https://fcm.googleapis.com\\@evil.example/x", false},
	}
	for _, c := range cases {
		if err := pushEndpointOK(c.url); (err == nil) != c.ok {
			t.Errorf("%q: ok=%v, err=%v", c.url, c.ok, err)
		}
	}
}
