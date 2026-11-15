package zitijoin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Prompt is what a room with no browser shows: a URL to open on any device and the code to type there.
type Prompt struct {
	URL  string
	Code string
}

type discovery struct {
	DeviceEndpoint string `json:"device_authorization_endpoint"`
	TokenEndpoint  string `json:"token_endpoint"`
}

// DeviceFlow runs the OAuth device authorization grant against an issuer and answers the token it ends with. show is
// called once with what the person has to do, then this waits for them to do it.
func DeviceFlow(ctx context.Context, c *http.Client, issuer, clientID string, scopes []string, show func(Prompt)) (string, error) {
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second}
	}
	var d discovery
	if err := getJSON(ctx, c, strings.TrimSuffix(issuer, "/")+"/.well-known/openid-configuration", &d); err != nil {
		return "", fmt.Errorf("could not read the issuer's configuration: %w", err)
	}
	if d.DeviceEndpoint == "" {
		return "", errors.New("that issuer does not offer the device flow")
	}
	if len(scopes) == 0 {
		scopes = []string{"openid"}
	}
	var dev struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	if err := postForm(ctx, c, d.DeviceEndpoint, url.Values{
		"client_id": {clientID}, "scope": {strings.Join(scopes, " ")},
	}, &dev); err != nil {
		return "", err
	}
	link := dev.VerificationURIComplete
	if link == "" {
		link = dev.VerificationURI
	}
	show(Prompt{URL: link, Code: dev.UserCode})
	wait := time.Duration(dev.Interval) * time.Second
	if wait <= 0 {
		wait = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(dev.ExpiresIn) * time.Second)
	if dev.ExpiresIn <= 0 {
		deadline = time.Now().Add(10 * time.Minute)
	}
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(wait):
		}
		var tok struct {
			IDToken     string `json:"id_token"`
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
		}
		err := postForm(ctx, c, d.TokenEndpoint, url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {dev.DeviceCode}, "client_id": {clientID},
		}, &tok)
		switch tok.Error {
		case "authorization_pending":
			continue
		case "slow_down":
			wait += 5 * time.Second
			continue
		case "":
		default:
			return "", fmt.Errorf("the sign-in was refused: %s", tok.Error)
		}
		if err != nil {
			return "", err
		}
		if tok.IDToken != "" {
			return tok.IDToken, nil
		}
		if tok.AccessToken != "" {
			return tok.AccessToken, nil
		}
		return "", errors.New("the issuer answered with no token")
	}
	return "", errors.New("the sign-in code expired before it was approved")
}

// postForm decodes the body whatever the status, because the device flow reports "still waiting" as a 400.
func postForm(ctx context.Context, c *http.Client, u string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s answered %s with something unreadable", u, resp.Status)
	}
	return nil
}
