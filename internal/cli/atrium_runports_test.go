package cli

import (
	"strings"
	"testing"
)

func probesWith(held, atrium map[int]bool) portProbes {
	return portProbes{free: func(p int) bool { return !held[p] }, atrium: func(p int) bool { return atrium[p] }}
}

func TestARunPicksTheNextFreeBlockAndSaysWhy(t *testing.T) {
	none := map[int]bool{}
	if b, run, note, err := pickRunBlock(runBlock{}, false, probesWith(none, none)); err != nil || run || note != "" ||
		b != blockAt(7778) {
		t.Fatalf("a free machine: %+v %v %q %v", b, run, note, err)
	}
	b, _, note, _ := pickRunBlock(runBlock{}, false, probesWith(map[int]bool{7778: true}, none))
	if b != blockAt(7878) || !strings.Contains(note, "port 7778 is in use by another program") ||
		!strings.Contains(note, "board 7878, link 7879, room board 7881 and agents 7877") {
		t.Fatalf("7778 held: %+v %q", b, note)
	}
	b, _, note, _ = pickRunBlock(runBlock{}, false, probesWith(map[int]bool{7778: true}, map[int]bool{7778: true}))
	if b != blockAt(7878) || !strings.Contains(note, "another atrium, perhaps another account's") {
		t.Fatalf("another atrium on 7778: %+v %q", b, note)
	}
	if b, run, _, _ := pickRunBlock(blockAt(7878), false, probesWith(map[int]bool{7878: true}, map[int]bool{7878: true})); !run ||
		b.board != 7878 {
		t.Fatalf("this hub up on its recorded board: %+v %v", b, run)
	}
	if b, _, note, _ := pickRunBlock(blockAt(7878), false, probesWith(none, none)); b != blockAt(7878) || note != "" {
		t.Fatalf("the recorded block lost to the default: %+v %q", b, note)
	}
	if b, _, note, _ := pickRunBlock(runBlock{}, true, probesWith(map[int]bool{7781: true, 7777: true}, none)); b !=
		blockAt(7778) || note != "" {
		t.Fatalf("this room's own ports counted as held: %+v %q", b, note)
	}
	all := map[int]bool{}
	for i := 0; i < runBlockTries; i++ {
		all[7779+100*i] = true
	}
	if _, _, _, err := pickRunBlock(runBlock{}, false, probesWith(all, none)); err == nil ||
		!strings.Contains(err.Error(), "--addr") {
		t.Fatalf("every block held: %v", err)
	}
}
