package userenv

import (
	"os"
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
			v, _, err := k.GetStringValue(n)
			if err != nil || v == "" {
				continue
			}
			v = os.ExpandEnv(expandPercent(v))
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

// expandPercent turns %NAME% into ${NAME} for os.ExpandEnv, leaving an unknown or unpaired % alone.
func expandPercent(s string) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(s, '%')
		if i < 0 {
			break
		}
		j := strings.IndexByte(s[i+1:], '%')
		if j < 0 {
			break
		}
		name := s[i+1 : i+1+j]
		if name != "" && os.Getenv(name) != "" {
			b.WriteString(s[:i] + "${" + name + "}")
		} else {
			b.WriteString(s[:i+j+2])
		}
		s = s[i+j+2:]
	}
	b.WriteString(s)
	return b.String()
}
