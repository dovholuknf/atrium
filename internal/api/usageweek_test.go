package api

import (
	"strings"
	"testing"
)

// The reset as the settings read answers it: JSON decoded to a map, so each field is picked out.
func weekResetOf(t *testing.T, srv *Server) UsageWeekReset {
	t.Helper()
	m, _ := settingsGet(t, srv)["usage_week_reset"].(map[string]any)
	d, _ := m["day"].(string)
	tm, _ := m["time"].(string)
	tz, _ := m["tz"].(string)
	return UsageWeekReset{Day: d, Time: tm, TZ: tz}
}

// The weekly reset setting: Sunday 18:00 New York until told otherwise, it reads back, and a bad one is refused
// and leaves the old one standing.
func TestUsageWeekResetSettingRoundTrip(t *testing.T) {
	srv, _, _ := fileServer(t)

	got := weekResetOf(t, srv)
	if got != DefaultUsageWeekReset {
		t.Fatalf("a new board reads usage_week_reset=%v, want %v", got, DefaultUsageWeekReset)
	}
	if rec := settingsPost(t, srv, `{"usage_week_reset":{"day":"Mon","time":"09:30","tz":"Europe/London"}}`); rec.Code != 200 {
		t.Fatalf("post = %d %s", rec.Code, rec.Body)
	}
	want := UsageWeekReset{Day: "mon", Time: "09:30", TZ: "Europe/London"}
	if got := weekResetOf(t, srv); got != want {
		t.Fatalf("read back %v, want %v", got, want)
	}
	for _, bad := range []string{
		`{"day":"funday","time":"09:30","tz":"Europe/London"}`,
		`{"day":"mon","time":"25:00","tz":"Europe/London"}`,
		`{"day":"mon","time":"9:30","tz":"Europe/London"}`,
		`{"day":"mon","time":"09:30","tz":"Mars/Olympus"}`,
		`{"day":"mon","time":"09:30","tz":""}`,
	} {
		rec := settingsPost(t, srv, `{"usage_week_reset":`+bad+`}`)
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), "error") {
			t.Fatalf("%s: post = %d %s, want a 400", bad, rec.Code, rec.Body)
		}
	}
	if got := weekResetOf(t, srv); got != want {
		t.Fatalf("a refused post changed it to %v", got)
	}
}
