//go:build integration

package link

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func sshKey(name string) string {
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString([]byte("key body "+name))
}

// A ROOM PUBLISHES ITS KEY AND LOGIN, EVERY ROOM PULLS THE SET, and a room can only publish as itself.
func TestRoomsPublishTheirSSHAndPullTheSet(t *testing.T) {
	h := newSpecHub(t, "room-d", "room-e")
	ctx := context.Background()
	if err := PublishSSH(ctx, plain{h.addr}, "room-d", SSHPublish{User: "localai", Key: sshKey("room-d") + " me@room-d", Port: 22, On: true}); err != nil {
		t.Fatal(err)
	}
	if err := PublishSSH(ctx, plain{h.addr}, "room-e", SSHPublish{User: "claude", Key: sshKey("room-e"), Port: 2222}); err != nil {
		t.Fatal(err)
	}
	set, err := FetchSSHSet(ctx, plain{h.addr}, "room-e")
	if err != nil || len(set) != 2 {
		t.Fatalf("%+v %v", set, err)
	}
	if set[0].Room != "room-d" || set[0].User != "localai" || set[0].Key != sshKey("room-d") || !set[0].On {
		t.Fatalf("room-d: %+v", set[0])
	}
	if set[1].Room != "room-e" || set[1].User != "claude" || set[1].Port != 2222 {
		t.Fatalf("room-e: %+v", set[1])
	}

	// The board reads the same set.
	res, err := http.Get(h.front.URL + "/_hub/sshkeys")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	var v struct{ Keys []SSHEntry }
	if res.StatusCode != 200 || json.Unmarshal(body, &v) != nil || len(v.Keys) != 2 {
		t.Fatalf("board: %d %s", res.StatusCode, body)
	}
}

// A ROOM THE HUB DOES NOT KNOW IS REFUSED, a bad key is refused, and a removed room's key leaves the next pull.
func TestSSHSetRefusesUnknownRoomsAndDropsRemovedOnes(t *testing.T) {
	h := newSpecHub(t, "room-d", "room-e")
	ctx := context.Background()
	if err := PublishSSH(ctx, plain{h.addr}, "ghost", SSHPublish{User: "claude", Key: sshKey("x"), Port: 22}); err == nil {
		t.Fatal("an unknown room published")
	}
	if err := PublishSSH(ctx, plain{h.addr}, "room-d", SSHPublish{User: "claude", Key: `command="x" ` + sshKey("x"), Port: 22}); err == nil {
		t.Fatal("a key with options was accepted")
	}
	for _, r := range []string{"room-d", "room-e"} {
		if err := PublishSSH(ctx, plain{h.addr}, r, SSHPublish{User: "claude", Key: sshKey(r), Port: 22, On: true}); err != nil {
			t.Fatal(err)
		}
	}
	room, err := h.st.ByName("room-d")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.Remove(room.ID, "gone"); err != nil {
		t.Fatal(err)
	}
	set, err := FetchSSHSet(ctx, plain{h.addr}, "room-e")
	if err != nil || len(set) != 1 || set[0].Room != "room-e" {
		t.Fatalf("after removing room-d: %+v %v", set, err)
	}
}
