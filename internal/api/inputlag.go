package api

import (
	"log"
	"strings"

	"github.com/dovholuknf/atrium/internal/inputlag"
	"github.com/dovholuknf/atrium/internal/store"
)

// SettingInputLag is whether this room logs terminal input lag, `on` or `off`.
// Empty means never set, which is off. See internal/inputlag.
//
// A SETTING AND NOT ONLY A VARIABLE, because the variable is read once at start
// and finding a lag means switching the logging on while the lag is happening.
// A restart is the one thing that makes the lag go away for a while. The gear
// writes this through the hub, which keeps its own copy and passes the write to
// every room, so one checkbox times the browser, the hub and the room together.
const SettingInputLag = "input_lag_log"

// ApplyInputLag switches the room's logging to what is stored, for a daemon
// coming up. A value never written leaves the start-time default alone.
func ApplyInputLag(st *store.Store) {
	v, err := st.Setting(SettingInputLag)
	if err != nil || strings.TrimSpace(v) == "" {
		return
	}
	inputlag.SetLive(v == "on")
}

// setInputLag stores the switch and applies it at once.
func setInputLag(st *store.Store, on bool) error {
	v := "off"
	if on {
		v = "on"
	}
	if err := st.SetSetting(SettingInputLag, v); err != nil {
		return err
	}
	if !inputlag.SetLive(on) {
		log.Printf("[inputlag] %s is set, so the setting is stored and the logging stays as the variable says",
			inputlag.Env)
	}
	return nil
}

// inputLagView is the switch as the settings screen reads it: what is in force,
// and whether the variable is what decided it.
func inputLagView(out map[string]any) {
	out["input_lag_log"] = inputlag.On()
	out["input_lag_pinned"] = inputlag.Pinned()
}
