package userenv

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// current is the machine environment then the user's as the registry holds them now, keyed by upper case name. PATH
// is the two joined.
func current() map[string]string {
	out := map[string]string{}
	for _, src := range []struct {
		root registry.Key
		sub  string
	}{
		{registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`},
		{registry.CURRENT_USER, `Environment`},
	} {
		k, err := registry.OpenKey(src.root, src.sub, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		names, _ := k.ReadValueNames(-1)
		for _, n := range names {
			v, typ, err := k.GetStringValue(n)
			if err != nil || v == "" {
				continue
			}
			// Only REG_EXPAND_SZ holds %NAME%, and a literal $ or % elsewhere is the value.
			if typ == registry.EXPAND_SZ {
				if x, err := registry.ExpandString(v); err == nil {
					v = x
				}
			}
			key := strings.ToUpper(n)
			if key == "PATH" && out[key] != "" {
				v = out[key] + ";" + v
			}
			out[key] = v
		}
		k.Close()
	}
	return out
}
