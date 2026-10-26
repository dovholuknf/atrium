package cli

import (
	"golang.org/x/sys/windows/registry"

	"github.com/dovholuknf/atrium/internal/api"
)

const openHandlerKey = `Software\Classes\` + api.OpenLinkScheme

// registerOpenHandler writes the protocol under HKCU, which is the desktop user's own and needs no elevation.
func registerOpenHandler(exe string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, openHandlerKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.SetStringValue("", "URL:atrium open"); err != nil {
		return err
	}
	if err := k.SetStringValue("URL Protocol", ""); err != nil {
		return err
	}
	c, _, err := registry.CreateKey(registry.CURRENT_USER, openHandlerKey+`\shell\open\command`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.SetStringValue("", `"`+exe+`" open-handler open "%1"`)
}

func unregisterOpenHandler() error {
	for _, sub := range []string{`\shell\open\command`, `\shell\open`, `\shell`, ``} {
		if err := registry.DeleteKey(registry.CURRENT_USER, openHandlerKey+sub); err != nil && err != registry.ErrNotExist {
			return err
		}
	}
	return nil
}
