package gitsync

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
	"sync"
)

// HeaderCardToken carries a card's git token to the room's hub forwarder. It is sent by the card's own git, from
// `http.<forwarder url>.extraHeader`, and it is NEVER forwarded on to the hub.
const HeaderCardToken = "X-Atrium-Card-Token"

// CardAuth is how the room's hub forwarder knows which card is asking. It is the whole of what the forwarder
// needs, kept this small so the security design's 2c card tokens replace it without touching the forwarder.
//
// THE TOKEN AUTHORIZES THE FORWARDER ROUTE AND NOTHING ELSE. It is not accepted on any other agent-listener route.
type CardAuth interface {
	// Mint makes a fresh token for the card and forgets any it had. The token is secret: it goes into the card's
	// environment and nowhere else.
	Mint(card string) (token string, err error)
	// Check says which card a token belongs to. ok is false for a token that is unknown, malformed, revoked, or
	// whose card is no longer live.
	Check(token string) (card string, ok bool)
	// Revoke forgets the card's token.
	Revoke(card string)
}

// CardTokens is the in-memory CardAuth. A token is `<card id>.<256 random bits as hex>`. The room keeps only a
// SHA-256 of the secret, so the table does not hold a usable credential, and the comparison is constant-time.
//
// The room keeps the hash of each secret it handed out in its store and gives it back through RestoreHash at start, so a
// runner the pty host kept through a restart still has a good token.
type CardTokens struct {
	// Live, when set, says whether a card may still use its token. The room answers from the card's status, so a
	// card that ended, was paused or was culled loses its token even if nobody called Revoke.
	Live func(card string) bool

	mu      sync.Mutex
	by      map[string][sha256.Size]byte
	roomKey [32]byte
}

// Mint implements CardAuth.
func (c *CardTokens) Mint(card string) (string, error) {
	if !ValidCardID(card) {
		return "", errNotACard
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", err
	}
	tok := card + "." + hex.EncodeToString(secret[:])
	c.mu.Lock()
	if c.by == nil {
		c.by = map[string][sha256.Size]byte{}
	}
	c.by[card] = sha256.Sum256(secret[:])
	c.mu.Unlock()
	return tok, nil
}

// TokenHash is the card and the hex SHA-256 of the secret in a token, what the room may keep. ok is false for a malformed
// token.
func TokenHash(token string) (card, hash string, ok bool) {
	i := strings.LastIndexByte(token, '.')
	if i < 0 {
		return "", "", false
	}
	card = token[:i]
	secret, err := hex.DecodeString(token[i+1:])
	if err != nil || len(secret) != 32 || !ValidCardID(card) {
		return "", "", false
	}
	sum := sha256.Sum256(secret)
	return card, hex.EncodeToString(sum[:]), true
}

// RestoreHash puts back the hash of a token Mint made before a restart. It reports false for one that is malformed.
func (c *CardTokens) RestoreHash(card, hash string) bool {
	sum, err := hex.DecodeString(hash)
	if err != nil || len(sum) != sha256.Size || !ValidCardID(card) {
		return false
	}
	c.mu.Lock()
	if c.by == nil {
		c.by = map[string][sha256.Size]byte{}
	}
	c.by[card] = [sha256.Size]byte(sum)
	c.mu.Unlock()
	return true
}

// RoomToken is a token for the card that only this process holds: the room pushes with it for atrium_git_push, so the
// runner's own token is neither needed nor replaced. It is made fresh at each start and is never stored or given out.
func (c *CardTokens) RoomToken(card string) (string, error) {
	if !ValidCardID(card) {
		return "", errNotACard
	}
	if err := c.ensureKey(); err != nil {
		return "", err
	}
	return card + "." + hex.EncodeToString(c.roomSecret(card)), nil
}

func (c *CardTokens) ensureKey() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.roomKey == ([32]byte{}) {
		_, err := rand.Read(c.roomKey[:])
		return err
	}
	return nil
}

// roomSecret is HMAC-SHA256(room key, card), so a leaked room token opens that one card only.
func (c *CardTokens) roomSecret(card string) []byte {
	c.mu.Lock()
	key := c.roomKey
	c.mu.Unlock()
	m := hmac.New(sha256.New, key[:])
	m.Write([]byte(card))
	return m.Sum(nil)
}

func (c *CardTokens) isRoom(card string, secret []byte) bool {
	c.mu.Lock()
	set := c.roomKey != ([32]byte{})
	c.mu.Unlock()
	return set && subtle.ConstantTimeCompare(secret, c.roomSecret(card)) == 1
}

// Check implements CardAuth.
func (c *CardTokens) Check(token string) (string, bool) {
	i := strings.LastIndexByte(token, '.')
	if i < 0 {
		return "", false
	}
	card, secretHex := token[:i], token[i+1:]
	secret, err := hex.DecodeString(secretHex)
	if err != nil || len(secret) != 32 || !ValidCardID(card) {
		return "", false
	}
	got := sha256.Sum256(secret)
	c.mu.Lock()
	want, found := c.by[card]
	c.mu.Unlock()
	if c.isRoom(card, secret) {
		if c.Live != nil && !c.Live(card) {
			return "", false
		}
		return card, true
	}
	// Compared even when the card is unknown, so the time taken does not say whether a card exists.
	ok := subtle.ConstantTimeCompare(got[:], want[:]) == 1 && found
	if !ok {
		return "", false
	}
	if c.Live != nil && !c.Live(card) {
		return "", false
	}
	return card, true
}

// Revoke implements CardAuth.
func (c *CardTokens) Revoke(card string) {
	c.mu.Lock()
	delete(c.by, card)
	c.mu.Unlock()
}

type tokenErr string

func (e tokenErr) Error() string { return string(e) }

const errNotACard = tokenErr("that is not a card id, so it gets no git token")
