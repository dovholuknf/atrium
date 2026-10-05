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
	"encoding/asn1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"strings"
	"time"
)

// The two standards Web Push is made of, on the standard library alone. See docs/rnd/web-push-design.md.
//
//   - RFC 8291 encrypts a payload to ONE browser, so the push service that carries it learns nothing of its text.
//   - RFC 8292 (VAPID) signs a short JWT with the hub's own key, so the push service knows which application server
//     is sending and the browser's subscription is tied to that key.
//
// THE PRIVATE KEY NEVER LEAVES THIS FILE'S CALLERS: it is made on first enable, kept in hub_setting, and used to sign.
// Nothing here logs, formats or returns it.

var b64 = base64.RawURLEncoding

// pushPadTo is the size every payload is padded to, so a long card name does not show in the size of the push.
const pushPadTo = 256

// pushRecord is the aes128gcm record size the hub writes. A push service accepts a body of 4096 bytes at most, so one
// record of that size holds the whole message.
const pushRecord = 4096

// ── RFC 8291 ────────────────────────────────────────────

// encryptPush encrypts plaintext to a browser's p256dh and auth secret with a fresh ephemeral key and salt, padded to
// a multiple of pushPadTo.
func encryptPush(plaintext, uaPublic, authSecret []byte) ([]byte, error) {
	eph, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return encryptPushWith(plaintext, uaPublic, authSecret, eph, salt, pushPadTo)
}

// encryptPushWith is encryptPush with the ephemeral key and salt given, which is what the RFC's appendix A vector needs.
// pad is the size the record is padded up to a multiple of, and 0 pads nothing.
func encryptPushWith(plaintext, uaPublic, authSecret []byte, eph *ecdh.PrivateKey, salt []byte, pad int) ([]byte, error) {
	if len(authSecret) != 16 {
		return nil, errors.New("the auth secret has to be 16 bytes")
	}
	if len(salt) != 16 {
		return nil, errors.New("the salt has to be 16 bytes")
	}
	uaKey, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, fmt.Errorf("the p256dh is not a P-256 point: %w", err)
	}
	secret, err := eph.ECDH(uaKey)
	if err != nil {
		return nil, err
	}
	asPublic := eph.PublicKey().Bytes()

	// PRK_key = HKDF-Extract(auth_secret, ecdh_secret), IKM = HKDF-Expand(PRK_key, "WebPush: info" 0x00 ua as, 32)
	prkKey, err := hkdf.Extract(sha256.New, secret, authSecret)
	if err != nil {
		return nil, err
	}
	keyInfo := append(append([]byte("WebPush: info\x00"), uaPublic...), asPublic...)
	ikm, err := hkdf.Expand(sha256.New, prkKey, string(keyInfo), 32)
	if err != nil {
		return nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}

	// One record: the data, the 0x02 that marks the last record, then zeros.
	rec := append(append([]byte{}, plaintext...), 0x02)
	if pad > 0 {
		if r := len(rec) % pad; r != 0 {
			rec = append(rec, make([]byte, pad-r)...)
		}
	}
	if len(rec)+16 > pushRecord {
		return nil, errors.New("the payload is too long for one push")
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// The header: salt, record size, the key id's length, and the key id, which is the ephemeral public key.
	out := make([]byte, 0, 16+4+1+len(asPublic)+len(rec)+16)
	out = append(out, salt...)
	out = binary.BigEndian.AppendUint32(out, pushRecord)
	out = append(out, byte(len(asPublic)))
	out = append(out, asPublic...)
	return gcm.Seal(out, nonce, rec, nil), nil
}

// ── RFC 8292 ────────────────────────────────────────────

// newVAPIDKey makes the hub's signing key. The private half is returned as the base64url of its 32 byte scalar, which
// is how it is kept in hub_setting.
func newVAPIDKey() (string, error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	raw, err := k.Bytes()
	if err != nil {
		return "", err
	}
	return b64.EncodeToString(raw), nil
}

// parseVAPIDKey reads the stored private scalar.
func parseVAPIDKey(stored string) (*ecdsa.PrivateKey, error) {
	raw, err := b64.DecodeString(strings.TrimSpace(stored))
	if err != nil {
		return nil, errors.New("the stored VAPID key is not base64url")
	}
	k, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), raw)
	if err != nil {
		return nil, errors.New("the stored VAPID key is not a P-256 key")
	}
	return k, nil
}

// vapidPublic is the 65 byte public point, base64url, which the browser is given as its applicationServerKey.
func vapidPublic(k *ecdsa.PrivateKey) (string, error) {
	pub, err := k.PublicKey.Bytes()
	if err != nil {
		return "", err
	}
	return b64.EncodeToString(pub), nil
}

// vapidHeader is the Authorization value for a push to endpoint: an ES256 JWT for the endpoint's origin, good for 12
// hours, and the public key.
func vapidHeader(k *ecdsa.PrivateKey, endpoint, sub string, at time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	head, _ := json.Marshal(map[string]string{"typ": "JWT", "alg": "ES256"})
	claims, _ := json.Marshal(map[string]any{"aud": u.Scheme + "://" + u.Host, "exp": at.Add(12 * time.Hour).Unix(),
		"sub": sub})
	signing := b64.EncodeToString(head) + "." + b64.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))
	der, err := ecdsa.SignASN1(rand.Reader, k, digest[:])
	if err != nil {
		return "", err
	}
	var rs struct{ R, S *big.Int }
	if _, err := asn1.Unmarshal(der, &rs); err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	rs.R.FillBytes(sig[:32])
	rs.S.FillBytes(sig[32:])
	pub, err := vapidPublic(k)
	if err != nil {
		return "", err
	}
	return "vapid t=" + signing + "." + b64.EncodeToString(sig) + ", k=" + pub, nil
}

// ── the allowlist ───────────────────────────────────────

// pushHosts are the push services a subscription may name. A leading dot is a suffix with at least one label before it.
var pushHosts = []string{
	"fcm.googleapis.com",
	".push.apple.com",
	"updates.push.services.mozilla.com",
	".notify.windows.com",
}

// pushEndpointOK is the allowlist. It reads the endpoint as net/url parses it and never as a raw string, and it runs at
// subscribe and again on every send, so an allowlist change applies to rows already stored.
func pushEndpointOK(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("that endpoint is not a URL")
	}
	if u.Scheme != "https" {
		return errors.New("a push endpoint has to be https")
	}
	if u.User != nil {
		return errors.New("a push endpoint carries no user name")
	}
	if p := u.Port(); p != "" && p != "443" {
		return errors.New("a push endpoint is on port 443")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return errors.New("a push endpoint has a host")
	}
	if net.ParseIP(host) != nil {
		return errors.New("a push endpoint is a name and not an IP address")
	}
	for _, h := range pushHosts {
		if h[0] == '.' {
			if strings.HasSuffix(host, h) && len(host) > len(h) {
				return nil
			}
		} else if host == h {
			return nil
		}
	}
	return errors.New("that is not a push service this hub sends to")
}
