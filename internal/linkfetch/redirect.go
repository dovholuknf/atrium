// Package linkfetch is the recogniser's built-in `redirect` fetch: follow a pasted link to where it lands and read the
// facts out of that address.
//
// A Discourse topic pasted as /t/<slug> carries no topic number, and Discourse answers it with a redirect to
// /t/<slug>/<num>. gwt asks the same way (a HEAD, then the final URL). The fetch knows nothing about Discourse: the
// row's argument is a pattern with named groups, matched against the address the link landed on.
package linkfetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Redirect is the fetch's name in a recogniser row: fetch "redirect", fetch_args [pattern].
const Redirect = "redirect"

// Timeout bounds one follow. Somebody is watching a dialog not open.
const Timeout = 15 * time.Second

// Client follows the redirects. A seam for tests.
var Client = &http.Client{Timeout: Timeout}

// Follow asks link and matches args[0] against the address it landed on. The named groups that matched are the facts.
// HEAD first, as gwt does, and a GET with the body thrown away when a server refuses a HEAD.
func Follow(ctx context.Context, link string, args []string) (map[string]string, error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return nil, errors.New("the redirect fetch takes one argument, a pattern for the address the link lands on")
	}
	re, err := regexp.Compile(args[0])
	if err != nil {
		return nil, fmt.Errorf("the redirect fetch's pattern is not a valid regular expression: %w", err)
	}
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("the redirect fetch follows only an http or https link")
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	landed, err := land(ctx, http.MethodHead, u.String())
	if err != nil {
		landed, err = land(ctx, http.MethodGet, u.String())
	}
	if err != nil {
		return nil, err
	}
	m := re.FindStringSubmatch(landed)
	if m == nil {
		return nil, fmt.Errorf("the link landed on %s, which the fetch's pattern does not match", landed)
	}
	out := map[string]string{}
	for i, name := range re.SubexpNames() {
		if name != "" && m[i] != "" {
			out[name] = m[i]
		}
	}
	return out, nil
}

// land is the address one request ended on, after every redirect. A 4xx or 5xx is a failure.
func land(ctx context.Context, method, link string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, method, link, nil)
	if err != nil {
		return "", err
	}
	res, err := Client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 400 {
		return "", fmt.Errorf("%s answered %s", link, res.Status)
	}
	return res.Request.URL.String(), nil
}
