package zitictx

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/openziti/sdk-golang/ziti"
)

type fakeCtx struct {
	ziti.Context
	closed atomic.Int32
	reauth atomic.Int32
}

func (f *fakeCtx) Close()                { f.closed.Add(1) }
func (f *fakeCtx) Reauthenticate() error { f.reauth.Add(1); return nil }

func fakeSDK(t *testing.T) *[]*fakeCtx {
	t.Helper()
	var built []*fakeCtx
	restore := SetSDK(
		func(string) (*ziti.Config, error) { return &ziti.Config{}, nil },
		func(*ziti.Config) (ziti.Context, error) {
			f := &fakeCtx{}
			built = append(built, f)
			return f, nil
		})
	t.Cleanup(func() { CloseAll(); restore() })
	return &built
}

func TestSharedFileBuildsOncePerIdentityFile(t *testing.T) {
	built := fakeSDK(t)
	for i := 0; i < 5; i++ {
		if _, err := SharedFile("test", "a.json"); err != nil {
			t.Fatal(err)
		}
	}
	if len(*built) != 1 || Live() != 1 {
		t.Fatalf("built %d, live %d, want 1 and 1", len(*built), Live())
	}
	if _, err := SharedFile("test", "b.json"); err != nil {
		t.Fatal(err)
	}
	if len(*built) != 2 || Live() != 2 {
		t.Fatalf("built %d, live %d, want 2 and 2", len(*built), Live())
	}
}

func TestSharedHandleCloseKeepsTheContext(t *testing.T) {
	built := fakeSDK(t)
	h, _ := SharedFile("test", "a.json")
	h.Close()
	h.Close()
	if Live() != 1 || (*built)[0].closed.Load() != 0 {
		t.Fatalf("live %d, closed %d, want 1 and 0", Live(), (*built)[0].closed.Load())
	}
}

func TestReloginReauthenticatesWithoutBuilding(t *testing.T) {
	built := fakeSDK(t)
	h, _ := SharedFile("test", "a.json")
	if err := h.(interface{ Relogin() error }).Relogin(); err != nil {
		t.Fatal(err)
	}
	if err := Relogin("a.json"); err != nil {
		t.Fatal(err)
	}
	if len(*built) != 1 || (*built)[0].reauth.Load() != 2 {
		t.Fatalf("built %d, reauth %d, want 1 and 2", len(*built), (*built)[0].reauth.Load())
	}
}

func TestReloginOfAnIdentityNeverBuiltBuildsNothing(t *testing.T) {
	built := fakeSDK(t)
	if err := Relogin("a.json"); err != nil || len(*built) != 0 {
		t.Fatalf("err %v, built %d", err, len(*built))
	}
}

func TestNewRefusesASecondContextForOneKey(t *testing.T) {
	built := fakeSDK(t)
	if _, err := New("test", "k", &ziti.Config{}); err != nil {
		t.Fatal(err)
	}
	if _, err := New("test", "k", &ziti.Config{}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("got %v, want ErrDuplicate", err)
	}
	if Live() != 1 || len(*built) != 1 {
		t.Fatalf("live %d, built %d, want 1 and 1", Live(), len(*built))
	}
}

func TestCloseAllClosesAndFreesTheKey(t *testing.T) {
	built := fakeSDK(t)
	SharedFile("test", "a.json")
	CloseAll()
	if Live() != 0 || (*built)[0].closed.Load() != 1 {
		t.Fatalf("live %d closed %d", Live(), (*built)[0].closed.Load())
	}
}
