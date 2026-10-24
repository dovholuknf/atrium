//go:build integration

package sshwizard

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestKeyscanReadsTheBannerAndTheHostKeyAndNeverAuthenticates(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	var authed atomic.Bool
	cfg := &ssh.ServerConfig{
		ServerVersion: "SSH-2.0-OpenSSH_8.9p1 Ubuntu-3",
		PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) {
			authed.Store(true)
			return nil, errors.New("no")
		},
	}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _, _, _ = ssh.NewServerConn(c, cfg)
			}()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	sc, err := OSEnv{}.Keyscan(context.Background(), "127.0.0.1", port)
	if err != nil {
		t.Fatal(err)
	}
	want := Fingerprint(signer.PublicKey().Marshal())
	if sc.Banner != "SSH-2.0-OpenSSH_8.9p1 Ubuntu-3" || len(sc.Keys) != 1 || sc.Keys[0].Type != "ssh-ed25519" || sc.Keys[0].Fingerprint != want {
		t.Fatalf("%+v, want the banner and %s", sc, want)
	}
	if authed.Load() {
		t.Fatal("the scan tried to log in")
	}
}

func TestKeyscanOfAClosedPortSaysWhy(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	sc, err := OSEnv{}.Keyscan(context.Background(), "127.0.0.1", port)
	if err == nil || len(sc.Keys) != 0 || sc.Banner != "" {
		t.Fatalf("%+v %v", sc, err)
	}
}
