package api

import "github.com/dovholuknf/atrium/internal/cardcolors"

// SettingCardColors and CardColors live in internal/cardcolors, shared with the hub.
const SettingCardColors = cardcolors.Setting

type CardColors = cardcolors.Colors

// CardColors reads the setting, seeding it when the row does not exist.
func (s *Server) CardColors() CardColors {
	raw, _ := s.st.Setting(SettingCardColors)
	c, seed := cardcolors.Load(raw)
	if seed {
		if v, err := cardcolors.Encode(c); err == nil {
			_ = s.st.SetSetting(SettingCardColors, v)
		}
	}
	return c
}

func (s *Server) setCardColors(c CardColors) error {
	v, err := cardcolors.Encode(c)
	if err != nil {
		return err
	}
	return s.st.SetSetting(SettingCardColors, v)
}
