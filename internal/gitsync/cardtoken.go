package gitsync

import (
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
// Tokens do not survive a restart of the room: a card launched before it must be relaunched to push.
type CardTokens struct {
	// Live, when set, says whether a card may still use its token. The room answers from the card's status, so a
	// card that ended, was shelved or was culled loses its token even if nobody called Revoke.
	Live func(card string) bool

	mu sync.Mutex
	by map[string][sha256.Size]byte
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
