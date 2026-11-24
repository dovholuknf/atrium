package capability

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/nowindow"
)

// Hardware is what a room's machine has, probed read-only. A field a probe could not read is left empty.
type Hardware struct {
	At       string `json:"at"`
	CPU      string `json:"cpu,omitempty"`
	Cores    int    `json:"cores,omitempty"`
	RAM      uint64 `json:"ram_bytes,omitempty"`
	RAMFree  uint64 `json:"ram_free_bytes,omitempty"`
	DiskPath string `json:"disk_path,omitempty"`
	DiskFree uint64 `json:"disk_free_bytes,omitempty"`
	GPUs     []GPU  `json:"gpus,omitempty"`
}

// GPU is one graphics device. API is cuda, rocm or metal. Unified is memory shared with the CPU, counted as VRAM.
type GPU struct {
	Name    string `json:"name"`
	VRAM    uint64 `json:"vram_bytes,omitempty"`
	API     string `json:"api,omitempty"`
	Unified bool   `json:"unified,omitempty"`
}

const hwTimeout = 10 * time.Second

// ProbeHardware reads this machine. disk is a path on the volume whose free space is reported, walked up to the
// nearest directory that exists.
func ProbeHardware(ctx context.Context, disk string) Hardware {
	h := Hardware{At: time.Now().UTC().Format(time.RFC3339), Cores: runtime.NumCPU()}
	h.CPU = strings.Join(strings.Fields(cpuModel(ctx)), " ")
	h.RAM, h.RAMFree = memory(ctx)
	if disk != "" {
		if p := existing(disk); p != "" {
			if free, ok := diskFree(p); ok {
				h.DiskPath, h.DiskFree = filepath.ToSlash(p), free
			}
		}
	}
	h.GPUs = append(nvidiaGPUs(ctx), osGPUs(ctx, h)...)
	return h
}

func existing(p string) string {
	for {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
		up := filepath.Dir(p)
		if up == p {
			return ""
		}
		p = up
	}
}

// probeOut is a read-only command's output, "" when it is missing or fails.
func probeOut(ctx context.Context, argv ...string) string {
	ctx, cancel := context.WithTimeout(ctx, hwTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var out capped
	cmd.Stdout = &out
	nowindow.Hide(cmd)
	if cmd.Run() != nil {
		return ""
	}
	return out.b.String()
}

func nvidiaGPUs(ctx context.Context) []GPU {
	return parseNvidia(probeOut(ctx, "nvidia-smi", "--query-gpu=name,memory.total", "--format=csv,noheader,nounits"))
}

func parseNvidia(text string) []GPU {
	var out []GPU
	for _, l := range strings.Split(text, "\n") {
		name, mib, ok := strings.Cut(strings.TrimSpace(l), ",")
		if !ok || strings.TrimSpace(name) == "" {
			continue
		}
		g := GPU{Name: strings.TrimSpace(name), API: "cuda"}
		if n, err := strconv.ParseUint(strings.TrimSpace(mib), 10, 64); err == nil {
			g.VRAM = n << 20
		}
		out = append(out, g)
	}
	return out
}

func parseROCm(text string) []GPU {
	var cards map[string]map[string]any
	if json.Unmarshal([]byte(text), &cards) != nil {
		return nil
	}
	var ids []string
	for id := range cards {
		if strings.HasPrefix(id, "card") {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var out []GPU
	for _, id := range ids {
		g := GPU{API: "rocm"}
		for k, v := range cards[id] {
			s := strings.TrimSpace(fmt.Sprint(v))
			switch {
			case strings.HasPrefix(k, "VRAM Total Memory"):
				g.VRAM, _ = strconv.ParseUint(s, 10, 64)
			case k == "Card Series" || k == "Card series":
				g.Name = s
			}
		}
		if g.Name == "" {
			g.Name = id
		}
		out = append(out, g)
	}
	return out
}

func parseProfiler(text string, ram uint64) []GPU {
	var doc struct {
		SP []map[string]any `json:"SPDisplaysDataType"`
	}
	if json.Unmarshal([]byte(text), &doc) != nil {
		return nil
	}
	var out []GPU
	for _, d := range doc.SP {
		g := GPU{Name: str(d["sppci_model"])}
		if g.Name == "" {
			g.Name = str(d["_name"])
		}
		for k := range d {
			if strings.HasPrefix(k, "spdisplays_mtlgpufamilysupport") {
				g.API = "metal"
			}
		}
		vram := str(d["spdisplays_vram"])
		if vram == "" {
			vram = str(d["spdisplays_vram_shared"])
		}
		if n, ok := ParseSize(strings.ReplaceAll(vram, " ", "")); ok {
			g.VRAM = n
		} else if str(d["sppci_bus"]) == "spdisplays_builtin" && ram > 0 {
			g.VRAM, g.Unified = ram, true
		}
		out = append(out, g)
	}
	return out
}

func str(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

var sizeShape = regexp.MustCompile(`(?i)^(\d+(?:\.\d+)?)\s*([kmgt]?)i?b?$`)

// ParseSize reads 12g, 512MB or 1.5T as bytes, 1024-based. A bare number is bytes.
func ParseSize(s string) (uint64, bool) {
	m := sizeShape.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	shift := map[string]uint{"": 0, "k": 10, "m": 20, "g": 30, "t": 40}[strings.ToLower(m[2])]
	return uint64(f * float64(uint64(1)<<shift)), true
}

// Size is n bytes as a short figure like 24g or 512m.
func Size(n uint64) string {
	for _, u := range []struct {
		s     string
		shift uint
	}{{"t", 40}, {"g", 30}, {"m", 20}, {"k", 10}} {
		if n >= 1<<u.shift {
			v := float64(n) / float64(uint64(1)<<u.shift)
			if v >= 10 || v == float64(uint64(v)) {
				return fmt.Sprintf("%.0f%s", v, u.s)
			}
			return fmt.Sprintf("%.1f%s", v, u.s)
		}
	}
	return fmt.Sprint(n)
}

var needShape = regexp.MustCompile(`^(vram|ram|disk|cores)\s*>=\s*(\S+)$`)

// HardwareNeed judges a need about hardware: gpu, gpu:cuda, gpu:rocm, gpu:metal, and vram, ram, disk or cores with
// >= a figure (vram>=12g, cores>=8). vram is the largest one device has, ram is total and disk is free space. handled
// is false for a need that is not about hardware.
func HardwareNeed(need string, h Hardware) (handled, ok bool, why string) {
	n := strings.ToLower(strings.TrimSpace(need))
	if n == "gpu" || strings.HasPrefix(n, "gpu:") {
		api := strings.TrimPrefix(strings.TrimPrefix(n, "gpu"), ":")
		for _, g := range h.GPUs {
			if api == "" || g.API == api {
				return true, true, ""
			}
		}
		if api == "" {
			return true, false, "no gpu found"
		}
		return true, false, "no " + api + " gpu found"
	}
	m := needShape.FindStringSubmatch(n)
	if m == nil {
		return false, false, ""
	}
	var have uint64
	switch m[1] {
	case "vram":
		for _, g := range h.GPUs {
			have = max(have, g.VRAM)
		}
	case "ram":
		have = h.RAM
	case "disk":
		have = h.DiskFree
	case "cores":
		have = uint64(h.Cores)
	}
	want, good := ParseSize(m[2])
	if m[1] == "cores" {
		c, err := strconv.ParseUint(m[2], 10, 64)
		want, good = c, err == nil
	}
	if !good {
		return true, false, "cannot read " + m[2] + " as a figure"
	}
	// A 12 GB card reports 12282 MiB and 32 GB of RAM a little less, so a byte figure has 3% slack.
	if m[1] != "cores" {
		want -= want * 3 / 100
	}
	if have >= want {
		return true, true, ""
	}
	if have == 0 {
		return true, false, m[1] + " not known"
	}
	if m[1] == "cores" {
		return true, false, fmt.Sprintf("cores %d", have)
	}
	return true, false, m[1] + " " + Size(have)
}

// Summary is the hardware as short lines for a reader.
func (h Hardware) Summary() []string {
	var out []string
	if h.CPU != "" || h.Cores > 0 {
		out = append(out, strings.TrimSpace(fmt.Sprintf("cpu: %s, %d cores", h.CPU, h.Cores)))
	}
	if h.RAM > 0 {
		s := "ram: " + Size(h.RAM)
		if h.RAMFree > 0 {
			s += ", " + Size(h.RAMFree) + " free"
		}
		out = append(out, s)
	}
	for _, g := range h.GPUs {
		s := "gpu: " + g.Name
		if g.API != "" {
			s += ", " + g.API
		}
		if g.VRAM > 0 {
			s += ", vram " + Size(g.VRAM)
			if g.Unified {
				s += " unified"
			}
		}
		out = append(out, s)
	}
	if len(h.GPUs) == 0 {
		out = append(out, "gpu: no cuda, rocm or metal gpu found")
	}
	if h.DiskPath != "" {
		out = append(out, "disk: "+Size(h.DiskFree)+" free on "+h.DiskPath)
	}
	return out
}
