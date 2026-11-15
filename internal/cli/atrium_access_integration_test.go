//go:build integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon/publish"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link"
	"github.com/dovholuknf/atrium/internal/zitijoin"
)

func accessOver(t *testing.T) (*hubAccess, *hubstore.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := hubstore.Open(filepath.Join(dir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return newHubAccess(store, publish.NewGuard(store, func() bool { return false }), dir), store
}

func viewOf(t *testing.T, a *hubAccess) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(a.View())
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestAnExposingEntryIsRefusedUntilASignInIsSet(t *testing.T) {
	a, _ := accessOver(t)
	pub := `{"kind":"zrok","name":"phone","mode":"public"}`
	if err := a.Add(context.Background(), []byte(pub)); err == nil || !strings.Contains(err.Error(), "sign-in") {
		t.Fatalf("a public zrok share with no sign-in was taken: %v", err)
	}
	own := `{"kind":"address","name":"lab","mode":"oidc","address":"lab.example.com"}`
	if err := a.Add(context.Background(), []byte(own)); err == nil {
		t.Fatal("an own address behind a sign-in was taken with none set")
	}
	if err := a.Add(context.Background(), []byte(strings.Replace(own, "}", `,"noauthSure":true}`, 1))); err != nil {
		t.Fatalf("a confirmed private URL was refused: %v", err)
	}
	if err := a.SaveAuth([]byte(`{"provider":"https://idp.example.com","client":"atrium","allow":["a@example.com"]}`)); err != nil {
		t.Fatal(err)
	}
	if err := a.Add(context.Background(), []byte(pub)); err != nil {
		t.Fatalf("a public share with a sign-in set was refused: %v", err)
	}
	v := viewOf(t, a)
	if got := v["auth"].(map[string]any)["provider"]; got != "https://idp.example.com" {
		t.Fatalf("the sign-in read back as %v", got)
	}
	if n := len(v["entries"].([]any)); n != 2 {
		t.Fatalf("%d entries listed, want 2", n)
	}
}

func TestAnEntryIsListedAndRevokedAndNamesAreUnique(t *testing.T) {
	a, _ := accessOver(t)
	body := []byte(`{"kind":"zrok","name":"home","mode":"private"}`)
	if err := a.Add(context.Background(), body); err != nil {
		t.Fatal(err)
	}
	if err := a.Add(context.Background(), body); err == nil {
		t.Fatal("two entries took one name")
	}
	es := viewOf(t, a)["entries"].([]any)
	id := es[0].(map[string]any)["id"].(string)
	if err := a.Revoke(id); err != nil {
		t.Fatal(err)
	}
	if err := a.Revoke(id); err == nil {
		t.Fatal("revoking twice said nothing")
	}
	if n := len(viewOf(t, a)["entries"].([]any)); n != 0 {
		t.Fatalf("%d entries left after a revoke", n)
	}
}

func TestAPendingRoomWaitsUntilApproved(t *testing.T) {
	a, store := accessOver(t)
	if _, err := store.AddPending("lab", hubstore.TransportZrok); err != nil {
		t.Fatal(err)
	}
	w := viewOf(t, a)["waiting"].([]any)
	if len(w) != 1 || w[0].(map[string]any)["via"] != "zrok" {
		t.Fatalf("waiting read %v", w)
	}
	if err := a.Approve("lab"); err != nil {
		t.Fatal(err)
	}
	if n := len(viewOf(t, a)["waiting"].([]any)); n != 0 {
		t.Fatalf("%d rooms still waiting after approval", n)
	}
	if err := a.Approve("nobody"); err == nil {
		t.Fatal("a room that does not exist was approved")
	}
}

func TestAnUnknownOverlayRoomIsParkedOnceAndAKnownOneIsLeftAlone(t *testing.T) {
	a, store := accessOver(t)
	if _, err := store.Add("known", hubstore.TransportDirect); err != nil {
		t.Fatal(err)
	}
	park := parkOverlayRoom(store)
	park("roomz", "ziti")
	park("roomz", "ziti")
	park("known", "ziti")
	w := viewOf(t, a)["waiting"].([]any)
	if len(w) != 1 || w[0].(map[string]any)["name"] != "roomz" {
		t.Fatalf("waiting read %v", w)
	}
	if err := newHubRooms(store).attaching("roomz", "h", "v"); err == nil || !strings.Contains(err.Error(), "approve") {
		t.Fatalf("a parked room attached or was refused without saying why: %v", err)
	}
	if err := a.Approve("roomz"); err != nil {
		t.Fatal(err)
	}
	if err := newHubRooms(store).attaching("roomz", "h", "v"); err != nil {
		t.Fatalf("an approved room was refused: %v", err)
	}
}

type fakeTransports struct {
	on  map[string]bool
	ids map[string]string
}

func (f *fakeTransports) View() []link.TransportView {
	var out []link.TransportView
	for n := range f.on {
		st := "off"
		if f.on[n] {
			st = "bound"
		}
		out = append(out, link.TransportView{Name: n, On: f.on[n], State: st, Identity: f.ids[n]})
	}
	return out
}

func (f *fakeTransports) Set(name string, on *bool, _, identity *string) (link.TransportView, error) {
	if on != nil {
		f.on[name] = *on
	}
	if identity != nil {
		f.ids[name] = *identity
	}
	return link.TransportView{}, nil
}

func TestEntriesStartTheirListenersAndRevokeStopsThem(t *testing.T) {
	a, _ := accessOver(t)
	ft := &fakeTransports{on: map[string]bool{}, ids: map[string]string{}}
	stopped := false
	a.rt = &accessRuntime{transports: ft, share: func(mode string) (func(), string, error) {
		return func() { stopped = true }, "https://x.share.zrok.io", nil
	}}
	if err := a.SaveAuth([]byte(`{"provider":"https://idp.example.com","client":"atrium","allow":["a@example.com"]}`)); err != nil {
		t.Fatal(err)
	}
	if err := a.saveEntries([]accessEntry{{ID: "z1", Name: "net", Kind: "ziti", Serves: "both", File: "/id/net.json", State: "enrolled"}}); err != nil {
		t.Fatal(err)
	}
	a.StartAll()
	if !ft.on[link.TransportBoardZiti] || !ft.on[link.TransportRoomZiti] || ft.ids[link.TransportBoardZiti] != "/id/net.json" {
		t.Fatalf("a ziti entry started %v with %v", ft.on, ft.ids)
	}
	if got := viewOf(t, a)["entries"].([]any)[0].(map[string]any)["state"]; got != "live" {
		t.Fatalf("a bound ziti entry reads %v", got)
	}
	if err := a.Add(context.Background(), []byte(`{"kind":"zrok","name":"phone","mode":"public","serves":"both"}`)); err != nil {
		t.Fatal(err)
	}
	es := viewOf(t, a)["entries"].([]any)
	z := es[1].(map[string]any)
	if z["serves"] != "board" || z["state"] != "live" || z["url"] != "https://x.share.zrok.io" || ft.on[link.TransportRoomZrok] {
		t.Fatalf("a public zrok entry read %v with %v", z, ft.on)
	}
	if err := a.Revoke(z["id"].(string)); err != nil || !stopped {
		t.Fatalf("revoking the share: %v, stopped %v", err, stopped)
	}
	if err := a.Revoke("z1"); err != nil || ft.on[link.TransportBoardZiti] || ft.on[link.TransportRoomZiti] {
		t.Fatalf("revoking the ziti entry: %v, still on %v", err, ft.on)
	}
}

func TestAZitiURLJoinShowsThePromptThenEnrolls(t *testing.T) {
	a, _ := accessOver(t)
	ft := &fakeTransports{on: map[string]bool{}, ids: map[string]string{}}
	a.rt = &accessRuntime{transports: ft}
	shown, release := make(chan struct{}), make(chan struct{})
	a.join = func(_ context.Context, _ zitijoin.Params, _ zitijoin.JoinURL, show func(zitijoin.Prompt)) (string, error) {
		show(zitijoin.Prompt{URL: "https://idp.example.com/device", Code: "ABCD-EFGH"})
		close(shown)
		<-release
		return "/id/net.json", nil
	}
	body := `{"kind":"ziti","name":"net","how":"url","mode":"cert","serves":"board","controller":"https://c.example.com"}`
	if err := a.Add(context.Background(), []byte(body)); err != nil {
		t.Fatal(err)
	}
	<-shown
	e := viewOf(t, a)["entries"].([]any)[0].(map[string]any)
	p, _ := e["prompt"].(map[string]any)
	if e["state"] != "signing in" || p["code"] != "ABCD-EFGH" {
		t.Fatalf("an entry mid sign-in read %v", e)
	}
	close(release)
	a.signing.Wait()
	e = viewOf(t, a)["entries"].([]any)[0].(map[string]any)
	if e["state"] != "live" || e["prompt"] != nil || ft.ids[link.TransportBoardZiti] != "/id/net.json" {
		t.Fatalf("an entry after sign-in read %v with %v", e, ft.ids)
	}
}

func TestAZitiURLJoinThatFailsSaysWhy(t *testing.T) {
	a, _ := accessOver(t)
	a.join = func(context.Context, zitijoin.Params, zitijoin.JoinURL, func(zitijoin.Prompt)) (string, error) {
		return "", errors.New("the issuer would not answer")
	}
	if err := a.Add(context.Background(), []byte(`{"kind":"ziti","name":"net","how":"url","mode":"cert","controller":"https://c.example.com"}`)); err != nil {
		t.Fatal(err)
	}
	a.signing.Wait()
	e := viewOf(t, a)["entries"].([]any)[0].(map[string]any)
	if s, _ := e["state"].(string); !strings.Contains(s, "would not answer") {
		t.Fatalf("a failed sign-in read %v", e)
	}
}
