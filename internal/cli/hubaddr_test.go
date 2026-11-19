package cli

import "testing"

func TestRoomsAddDialsTheLinkTheRunningHubBound(t *testing.T) {
	dir := t.TempDir()
	c := roomAddCmd("atrium-")
	port, advertise := defaultLinkAddr(), ""
	recordedLink(c, dir, &port, &advertise)
	if port != defaultLinkAddr() {
		t.Fatalf("with no hub recorded the link is %q", port)
	}
	if err := writeHubAddrs(dir, hubAddrs{Board: "127.0.0.1:9778", Link: "0.0.0.0:9779", Advertise: "box:9779"}); err != nil {
		t.Fatal(err)
	}
	recordedLink(c, dir, &port, &advertise)
	if port != "0.0.0.0:9779" || advertise != "box:9779" {
		t.Fatalf("link %q advertise %q", port, advertise)
	}
	if got := recordedBoard(dir); got != "127.0.0.1:9778" {
		t.Fatalf("board %q", got)
	}
	_ = c.Flags().Set("link", "127.0.0.1:7000")
	port, advertise = "127.0.0.1:7000", ""
	recordedLink(c, dir, &port, &advertise)
	if port != "127.0.0.1:7000" || advertise != "" {
		t.Fatalf("a given --link was replaced: %q %q", port, advertise)
	}
}
