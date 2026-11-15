//go:build integration

package link

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTheHubAnswersItsOwnSignIn(t *testing.T) {
	front, _, done := two(t, settingsRoom("alpha", "moss"), settingsRoom("beta", "ember"))
	defer done()
	saved := map[string]any{"enabled": false}
	front.Config.Handler.(*Proxy).SetHubAuth(func() any { return saved }, func(b []byte) error {
		return json.Unmarshal(b, &saved)
	})

	if got := getJSON(t, front.URL+"/v1/auth"); got["enabled"] != false {
		t.Fatalf("the hub's sign-in read %v", got)
	}
	req, _ := http.NewRequest(http.MethodPut, front.URL+"/v1/auth", bytes.NewReader([]byte(`{"enabled":true}`)))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK || saved["enabled"] != true {
		t.Fatalf("saving the hub's sign-in answered %d and stored %v", res.StatusCode, saved)
	}

	req, _ = http.NewRequest(http.MethodPut, front.URL+"/v1/auth", bytes.NewReader([]byte(`{"enabled":false}`)))
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden || saved["enabled"] != true {
		t.Fatalf("a save from off the machine answered %d and stored %v", res.StatusCode, saved)
	}
}

func TestAHubWithOnlyItsOwnRoomStillAnswersItsSignIn(t *testing.T) {
	proxy, done := oneRoom(t, "alpha", settingsRoom("alpha", "moss"))
	defer done()
	front := httptest.NewServer(proxy)
	defer front.Close()
	saved := map[string]any{"enabled": false}
	proxy.SetHubAuth(func() any { return saved }, func(b []byte) error { return json.Unmarshal(b, &saved) })

	req, _ := http.NewRequest(http.MethodPut, front.URL+"/v1/auth", bytes.NewReader([]byte(`{"enabled":true}`)))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK || saved["enabled"] != true {
		t.Fatalf("saving on a one-room hub answered %d and stored %v", res.StatusCode, saved)
	}
}
