package daemon

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// The provider's public signing keys.
//
// This is what makes an id token evidence rather than a claim. Without it the
// token is a base64 document anybody can write, and a login becomes a form
// where you type your own subject.
//
// RSA only. Every provider in reach here signs with RS256, and supporting a
// curve nobody uses would be code with no way to know it works. A provider
// using something else fails with a sentence saying so, which is better than
// silently falling through to a weaker check.

// jwksCache holds the keys, because they change rarely and a fetch per login is
// a round trip per login.
var jwksCache struct {
	sync.Mutex
	keys map[string]map[string]*rsa.PublicKey
	got  map[string]time.Time
}

// jwksFresh is how long to trust a fetched key set.
//
// An hour. Providers rotate on a scale of days and publish the new key before
// they use it, so an hour is well inside the window where both are listed.
const jwksFresh = time.Hour

// jwksFor fetches a provider's keys, or returns what was fetched recently.
func jwksFor(ctx context.Context, url string) (map[string]*rsa.PublicKey, error) {
	jwksCache.Lock()
	if k, ok := jwksCache.keys[url]; ok && time.Since(jwksCache.got[url]) < jwksFresh {
		jwksCache.Unlock()
		return k, nil
	}
	jwksCache.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not fetch the provider's signing keys: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("the provider answered %s for its signing keys", res.Status)
	}

	var doc struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("the provider's signing keys could not be read: %w", err)
	}

	out := map[string]*rsa.PublicKey{}
	for _, k := range doc.Keys {
		if k.Kty != "RSA" || k.N == "" || k.E == "" {
			continue
		}
		n, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		e, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		// The exponent is big-endian and usually three bytes. Built up rather
		// than assumed to be 65537, because a provider is allowed to use
		// something else and quietly getting it wrong would reject every
		// token with a signature error.
		var exp int
		for _, b := range e {
			exp = exp<<8 | int(b)
		}
		if exp == 0 {
			continue
		}
		out[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exp}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the provider publishes no RSA signing keys, and atrium " +
			"verifies nothing else")
	}

	jwksCache.Lock()
	if jwksCache.keys == nil {
		jwksCache.keys, jwksCache.got = map[string]map[string]*rsa.PublicKey{}, map[string]time.Time{}
	}
	jwksCache.keys[url], jwksCache.got[url] = out, time.Now()
	jwksCache.Unlock()
	return out, nil
}
