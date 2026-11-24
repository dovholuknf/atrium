package capability

import "testing"

func TestParseNvidiaReadsEachGPUWithItsVRAM(t *testing.T) {
	got := parseNvidia("NVIDIA GeForce RTX 4090, 24564\nNVIDIA RTX A2000, 6138\n\n")
	if len(got) != 2 || got[0].Name != "NVIDIA GeForce RTX 4090" || got[0].VRAM != 24564<<20 || got[0].API != "cuda" {
		t.Fatalf("%+v", got)
	}
	if parseNvidia("") != nil {
		t.Fatal("no output found a gpu")
	}
}

func TestParseROCmAndProfilerReadTheirJSON(t *testing.T) {
	r := parseROCm(`{"card0": {"VRAM Total Memory (B)": "17163091968", "Card Series": "Radeon RX 7800 XT"}, "system": {}}`)
	if len(r) != 1 || r[0].Name != "Radeon RX 7800 XT" || r[0].VRAM != 17163091968 || r[0].API != "rocm" {
		t.Fatalf("rocm: %+v", r)
	}
	m := parseProfiler(`{"SPDisplaysDataType": [{"sppci_model": "Apple M2 Max", "sppci_bus": "spdisplays_builtin",
		"spdisplays_mtlgpufamilysupport": "spdisplays_metal3"}]}`, 32<<30)
	if len(m) != 1 || m[0].Name != "Apple M2 Max" || m[0].API != "metal" || m[0].VRAM != 32<<30 || !m[0].Unified {
		t.Fatalf("apple silicon: %+v", m)
	}
	i := parseProfiler(`{"SPDisplaysDataType": [{"sppci_model": "AMD Radeon Pro 5500M", "spdisplays_vram": "8 GB"}]}`, 0)
	if len(i) != 1 || i[0].VRAM != 8<<30 || i[0].Unified {
		t.Fatalf("discrete mac gpu: %+v", i)
	}
}

func TestHardwareNeedJudgesGPUAndFigures(t *testing.T) {
	h := Hardware{Cores: 16, RAM: 64 << 30, DiskFree: 200 << 30, GPUs: []GPU{{Name: "x", VRAM: 24 << 30, API: "cuda"}}}
	for need, want := range map[string]bool{
		"gpu": true, "gpu:cuda": true, "gpu:rocm": false, "vram>=12g": true, "vram>=32G": false,
		"ram>=64g": true, "disk>=1t": false, "cores>=8": true, "cores>=32": false,
	} {
		handled, ok, why := HardwareNeed(need, h)
		if !handled || ok != want || (!ok && why == "") {
			t.Errorf("%s: handled %v ok %v why %q", need, handled, ok, why)
		}
	}
	if _, ok, _ := HardwareNeed("vram>=12g", Hardware{GPUs: []GPU{{VRAM: 12282 << 20}}}); !ok {
		t.Error("a 12 GB card reporting 12282 MiB failed vram>=12g")
	}
	if handled, _, _ := HardwareNeed("ssh:x", h); handled {
		t.Error("a capability id was taken for hardware")
	}
	if _, ok, why := HardwareNeed("vram>=1g", Hardware{}); ok || why != "vram not known" {
		t.Errorf("a room with no gpu: %v %q", ok, why)
	}
}
