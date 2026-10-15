package link

import (
	"crypto/x509"
	"testing"
)

func verifyHubCert(t *testing.T, k Keys, host string) error {
	t.Helper()
	ca, _, err := k.CA()
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := k.readCertFile("hub.crt")
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	_, err = leaf.Verify(x509.VerifyOptions{DNSName: host, Roots: pool})
	return err
}

func TestHubCertAdvertiseHostPortBecomesIPAndDNSSANs(t *testing.T) {
	k := Keys{Dir: t.TempDir()}
	if err := k.EnsureCA(Hosts("192.168.1.68:7779", "hub.example.com:7779", "bare.example.com")); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"192.168.1.68", "hub.example.com", "bare.example.com", "127.0.0.1"} {
		if err := verifyHubCert(t, k, h); err != nil {
			t.Errorf("%s should verify: %v", h, err)
		}
	}
	leaf, _ := k.readCertFile("hub.crt")
	for _, d := range leaf.DNSNames {
		if d == "192.168.1.68:7779" || d == "hub.example.com:7779" {
			t.Errorf("port leaked into a DNS SAN: %q", d)
		}
	}
}

func TestHubCertOldPortSANIsRegeneratedAndMatchingIsKept(t *testing.T) {
	k := Keys{Dir: t.TempDir()}
	if err := k.EnsureCA(Hosts()); err != nil {
		t.Fatal(err)
	}
	before, _ := k.readCertFile("hub.crt")
	if err := k.ensureServer(Hosts()); err != nil {
		t.Fatal(err)
	}
	same, _ := k.readCertFile("hub.crt")
	if before.SerialNumber.Cmp(same.SerialNumber) != 0 {
		t.Error("a matching certificate was regenerated")
	}
	// Hand-build the old bad shape: the advertise string as a DNS name.
	ips, _ := splitSANs(Hosts())
	if sansMatch(before, ips, append(Hosts(), "192.168.1.68:7779")) {
		t.Error("a different SAN set matched")
	}
	if err := k.EnsureCA(Hosts("192.168.1.68:7779")); err != nil {
		t.Fatal(err)
	}
	if err := verifyHubCert(t, k, "192.168.1.68"); err != nil {
		t.Errorf("new advertise address not covered: %v", err)
	}
}
