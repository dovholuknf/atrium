package store

import "testing"

func TestSettingLeanWorkerGatewayAndMercuriusFor(t *testing.T) {
	st := openTestStore(t)
	if st.LeanWorkerGateway() != "" {
		t.Fatal("default is not empty")
	}
	if err := st.SetSetting(SettingLeanWorkerGateway, " mercurius-worker "); err != nil {
		t.Fatal(err)
	}
	if got := st.LeanWorkerGateway(); got != "mercurius-worker" {
		t.Fatalf("got %q", got)
	}
	lean := []string{"atrium:lean"}
	for _, c := range []struct {
		tags []string
		gw   string
		want string
	}{
		{lean, "", ""},
		{nil, "mercurius-worker", ""},
		{lean, "mercurius-worker", "mercurius: mercurius-worker"},
		{append([]string{"atrium:mcp:mercurius"}, lean...), "mercurius-worker", "mercurius: mercurius (wide)"},
	} {
		if got := MercuriusFor(c.tags, c.gw); got != c.want {
			t.Errorf("MercuriusFor(%v, %q) = %q, want %q", c.tags, c.gw, got, c.want)
		}
	}
}
