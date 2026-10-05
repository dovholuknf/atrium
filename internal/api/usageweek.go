package api

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// UsageWeekReset is when the weekly usage limit resets: a fixed weekday and wall
// time in a named zone, not a rolling seven days. The usage tab's "this week"
// range starts at the last one. One per board, kept as a setting, so every
// browser on the board agrees. The reset itself is worked out in the browser
// (js/usage-charts.js), which is where the tab is drawn.
type UsageWeekReset struct {
	Day  string `json:"day"`  // sun, mon, tue, wed, thu, fri or sat
	Time string `json:"time"` // 24 hour HH:MM
	TZ   string `json:"tz"`   // an IANA zone name
}

// SettingUsageWeekReset holds the JSON of a UsageWeekReset. Unset is the default.
const SettingUsageWeekReset = "usage_week_reset"

// DefaultUsageWeekReset is what a board has until it is told otherwise.
var DefaultUsageWeekReset = UsageWeekReset{Day: "sun", Time: "18:00", TZ: "America/New_York"}

var (
	usageWeekDays = map[string]bool{"sun": true, "mon": true, "tue": true, "wed": true, "thu": true, "fri": true, "sat": true}
	usageWeekTime = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
)

// Check says why a reset is not usable, or nil.
func (u UsageWeekReset) Check() error {
	if !usageWeekDays[u.Day] {
		return fmt.Errorf("day is sun, mon, tue, wed, thu, fri or sat")
	}
	if !usageWeekTime.MatchString(u.Time) {
		return fmt.Errorf("time is 24 hour HH:MM")
	}
	if _, err := time.LoadLocation(u.TZ); err != nil || strings.TrimSpace(u.TZ) == "" {
		return fmt.Errorf("tz is not a time zone name this machine knows, like America/New_York")
	}
	return nil
}

func (s *Server) usageWeekReset() UsageWeekReset {
	v, err := s.st.Setting(SettingUsageWeekReset)
	if err != nil || strings.TrimSpace(v) == "" {
		return DefaultUsageWeekReset
	}
	var u UsageWeekReset
	if json.Unmarshal([]byte(v), &u) != nil || u.Check() != nil {
		return DefaultUsageWeekReset
	}
	return u
}

func (s *Server) setUsageWeekReset(u UsageWeekReset) error {
	u.Day = strings.ToLower(strings.TrimSpace(u.Day))
	u.Time = strings.TrimSpace(u.Time)
	u.TZ = strings.TrimSpace(u.TZ)
	if err := u.Check(); err != nil {
		return err
	}
	b, _ := json.Marshal(u)
	return s.st.SetSetting(SettingUsageWeekReset, string(b))
}
