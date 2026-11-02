package link

import "testing"

func TestTheZrokRowSaysItsMaskedShareAndEnv(t *testing.T) {
	on := true
	ht := &HubTransports{}
	ht.Add(TransportItem{Name: TransportRoomZrok, Kind: "zrok", NoIdentity: true, Locked: "first",
		Detail: func() string { return "share " + MaskShare(" abcd1234wxyz ") + ", env e1 at https://api" }},
		TransportSeed{On: &on})
	v := ht.View()
	if len(v) != 1 || v[0].Detail != "share abcd..., env e1 at https://api" || v[0].Switchable || !v[0].NoService {
		t.Fatalf("the zrok row reads %+v", v)
	}
	if MaskShare("abc") != "..." {
		t.Fatalf("a short token showed itself: %q", MaskShare("abc"))
	}
}
