package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
)

// toolAlts are other names that satisfy a role, a C compiler being cc, gcc or clang.
var toolAlts = map[string][]string{
	"cc":   {"gcc", "clang"},
	"gcc":  {"cc", "clang"},
	"c++":  {"g++", "clang++"},
	"make": {"mingw32-make", "gmake"},
}

// findToolOrAlt is findTool, also trying the names in toolAlts.
func findToolOrAlt(name, path string) string {
	for _, n := range append([]string{name}, toolAlts[name]...) {
		if p := findTool(n, path); p != "" {
			return p
		}
	}
	return ""
}

// toolExtraDirs are the usual places a toolchain sits without being on the room's PATH, only those that exist.
func toolExtraDirs(goos string) []string {
	var pats []string
	if goos == "windows" {
		local, appdata := os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA")
		pats = []string{`C:\Program Files\Go\bin`, `C:\Program Files\nodejs`, `C:\Program Files\Git\cmd`,
			`C:\msys64\ucrt64\bin`, `C:\msys64\mingw64\bin`, `C:\msys64\clang64\bin`,
			`C:\Program Files\LLVM\bin`, `C:\Strawberry\c\bin`, `C:\mingw64\bin`,
			`C:\Program Files\CMake\bin`,
			`C:\Program Files\Microsoft Visual Studio\*\*\VC\Tools\Llvm\x64\bin`,
			`C:\Program Files\Microsoft Visual Studio\*\*\Common7\IDE\CommonExtensions\Microsoft\CMake\CMake\bin`,
			`C:\Program Files\Microsoft Visual Studio\*\*\Common7\IDE\CommonExtensions\Microsoft\CMake\Ninja`,
			filepath.Join(local, `Programs\Python\Python3*`), filepath.Join(local, `Programs\Python\Python3*\Scripts`),
			filepath.Join(local, `Microsoft\WinGet\Packages\BrechtSanders.WinLibs*\mingw64\bin`),
			filepath.Join(local, `Microsoft\WinGet\Links`),
			filepath.Join(appdata, "npm")}
	} else {
		home, _ := os.UserHomeDir()
		pats = []string{"/usr/local/go/bin", "/usr/local/bin", "/usr/bin", "/opt/homebrew/bin",
			"/Library/Developer/CommandLineTools/usr/bin", filepath.Join(home, ".npm-global/bin")}
	}
	var out []string
	for _, p := range pats {
		m, _ := filepath.Glob(p)
		for _, d := range m {
			if !slices.Contains(out, d) {
				out = append(out, d)
			}
		}
	}
	return out
}

// addToPath appends the dirs not yet on the process PATH, so sessions get them and the system's own tools win.
func addToPath(dirs []string) {
	cur := os.Getenv("PATH")
	have := filepath.SplitList(cur)
	for _, d := range dirs {
		if !slices.Contains(have, d) {
			cur += string(os.PathListSeparator) + d
		}
	}
	os.Setenv("PATH", cur)
}

// toolEnv is the build env a found toolchain implies: CC and CXX, and CGO_ENABLED when there is a C compiler.
func toolEnv(path string) map[string]string {
	out := map[string]string{}
	if p := findToolOrAlt("cc", path); p != "" {
		out["CC"] = filepath.ToSlash(p)
		out["CGO_ENABLED"] = "1"
	}
	if p := findToolOrAlt("c++", path); p != "" {
		out["CXX"] = filepath.ToSlash(p)
	}
	return out
}

// discoverTools puts the usual toolchain directories on the room's PATH and CC, CXX and CGO_ENABLED in its env,
// leaving any the operator already set.
func discoverTools() {
	addToPath(toolExtraDirs(runtime.GOOS))
	for k, v := range toolEnv(os.Getenv("PATH")) {
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}
