package main

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/hubstore"
	zroksdk "github.com/openziti/zrok/v2/sdk/golang/sdk"
)

// The public share's login dispatch: decision 1 turned into a zrok ShareRequest.
// updb becomes a "user:password" pair, oidc becomes a provider, and anything
// that would leave a public URL open with no login is refused.

func TestApplyShareAuthUpdbSetsBasicAuth(t *testing.T) {
	req := &zroksdk.ShareRequest{}
	err := applyShareAuth(req, hubstore.ShareAuth{Scheme: "updb", User: "clint", Pass: "hunter2"})
	if err != nil {
		t.Fatalf("updb was refused: %v", err)
	}
	if len(req.BasicAuth) != 1 || req.BasicAuth[0] != "clint:hunter2" {
		t.Fatalf("BasicAuth was %v, wanted [clint:hunter2]", req.BasicAuth)
	}
	if req.OauthProvider != "" {
		t.Fatalf("updb also set an oauth provider: %q", req.OauthProvider)
	}
}

func TestApplyShareAuthOIDCSetsProvider(t *testing.T) {
	req := &zroksdk.ShareRequest{}
	err := applyShareAuth(req, hubstore.ShareAuth{Scheme: "oidc", OIDCProvider: "google"})
	if err != nil {
		t.Fatalf("oidc was refused: %v", err)
	}
	if req.OauthProvider != "google" {
		t.Fatalf("OauthProvider was %q, wanted google", req.OauthProvider)
	}
	if len(req.BasicAuth) != 0 {
		t.Fatalf("oidc also set basic auth: %v", req.BasicAuth)
	}
}

func TestApplyShareAuthRefusesNoLogin(t *testing.T) {
	req := &zroksdk.ShareRequest{}
	if err := applyShareAuth(req, hubstore.ShareAuth{}); err == nil {
		t.Fatal("a public share with no login was allowed; it must be refused")
	}
}

func TestApplyShareAuthUpdbNeedsBothUserAndPass(t *testing.T) {
	for _, a := range []hubstore.ShareAuth{
		{Scheme: "updb", User: "clint"},
		{Scheme: "updb", Pass: "hunter2"},
	} {
		req := &zroksdk.ShareRequest{}
		if err := applyShareAuth(req, a); err == nil {
			t.Fatalf("updb with %+v was allowed; it needs both a user and a password", a)
		}
	}
}

func TestApplyShareAuthRefusesColonInUsername(t *testing.T) {
	req := &zroksdk.ShareRequest{}
	err := applyShareAuth(req, hubstore.ShareAuth{Scheme: "updb", User: "cl:int", Pass: "p"})
	if err == nil || !strings.Contains(err.Error(), "colon") {
		t.Fatalf("a colon in the username should be refused, got %v", err)
	}
}
