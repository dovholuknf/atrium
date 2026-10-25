package daemon

import (
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// currentUserPath is the machine PATH then the user PATH as the registry holds them now, so a runner sees a tool
// installed after the room started.
func currentUserPath() (string, bool) {
	var parts []string
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
		v, _, err := k.GetStringValue("Path")
		k.Close()
		if err != nil || v == "" {
			continue
		}
		parts = append(parts, os.ExpandEnv(expandPercent(v)))
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, ";"), true
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
