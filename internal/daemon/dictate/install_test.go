package dictate

import (
	"strings"
	"testing"
)

func TestPlanShowsSizeAndSource(t *testing.T) {
	p := (&Installer{Dir: "x"}).Plan()
	if p.Total != pinned[0].Size+pinned[1].Size || !strings.Contains(p.Source, "ggml-org/whisper.cpp") {
		t.Fatalf("%+v", p)
	}
	for _, a := range pinned {
		if len(a.SHA256) != 64 || !strings.HasPrefix(a.URL, "https://") {
			t.Fatalf("asset %+v is not pinned", a)
		}
	}
}
