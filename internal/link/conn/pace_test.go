package conn

import (
	"testing"
	"time"
)

func TestAQueuedEchoCutsThePaceByAtMostHalf(t *testing.T) {
	var p pacer
	t0 := time.Unix(1000, 0)
	p.sample(100*time.Millisecond, t0)
	p.delay(1<<20, t0)
	p.sample(100*time.Millisecond+3*time.Second, t0.Add(time.Second))
	if want := float64(1<<20) * paceCut; p.rate != want {
		t.Fatalf("rate after a 3s queue = %.0f B/s, want %.0f", p.rate, want)
	}
	cut := p.rate
	p.delay(1<<20, t0.Add(time.Second))
	p.sample(4*time.Second, t0.Add(2*time.Second))
	if p.rate != cut {
		t.Fatal("a sample sent before the cut could show cut the rate again")
	}
}

func TestAQueueJustOverTheTargetCutsInProportion(t *testing.T) {
	var p pacer
	t0 := time.Unix(1000, 0)
	p.sample(100*time.Millisecond, t0)
	p.delay(1<<20, t0)
	p.sample(100*time.Millisecond+paceTarget*5/4, t0.Add(time.Second))
	if want := float64(1<<20) * 4 / 5; p.rate < want*0.99 || p.rate > want*1.01 {
		t.Fatalf("rate = %.0f, want about %.0f", p.rate, want)
	}
}

func TestAShortQueueLetsThePaceGrowAndAnIdleRoomStaysUnpaced(t *testing.T) {
	var p pacer
	t0 := time.Unix(1000, 0)
	p.sample(100*time.Millisecond, t0)
	p.delay(1<<10, t0)
	p.sample(120*time.Millisecond, t0.Add(time.Second))
	if p.rate != 0 {
		t.Fatalf("a room with no queue was paced at %.0f B/s", p.rate)
	}
	p.rate = 100 << 10
	p.ceil = 1 << 20
	p.delay(1<<10, t0.Add(time.Second))
	p.sample(120*time.Millisecond, t0.Add(2*time.Second))
	if p.rate != 100<<10*paceGrow {
		t.Fatalf("rate after a short queue = %.0f, want %.0f", p.rate, 100<<10*paceGrow)
	}
	p.ceil = p.rate
	p.delay(1<<10, t0.Add(2*time.Second))
	p.wrote(t0.Add(2 * time.Second))
	p.sample(120*time.Millisecond, t0.Add(3*time.Second))
	if p.rate != 100<<10*paceGrow*paceProbe {
		t.Fatalf("rate past the last drain = %.0f, want %.0f", p.rate, 100<<10*paceGrow*paceProbe)
	}
	p.sample(120*time.Millisecond, t0.Add(5*time.Second))
	if p.rate != 0 {
		t.Fatal("a sample with no git written kept the pace, so the next push would start on a stale rate")
	}
}

func TestThePaceNeverDropsBelowTheFloor(t *testing.T) {
	var p pacer
	t0 := time.Unix(1000, 0)
	p.sample(100*time.Millisecond, t0)
	p.delay(1, t0)
	p.sample(time.Minute, t0.Add(time.Second))
	if p.rate != paceFloor {
		t.Fatalf("rate = %.0f, want the floor %d", p.rate, paceFloor)
	}
}

func TestPacedWritesAreSpacedAtTheRate(t *testing.T) {
	var slept []time.Duration
	r := &Room{yieldSleep: func(d time.Duration) { slept = append(slept, d) }}
	r.paceWrite(gitWriteChunk)
	if len(slept) != 0 {
		t.Fatal("an unpaced write waited")
	}
	r.pace.rate = float64(gitWriteChunk)
	r.paceWrite(gitWriteChunk)
	r.paceWrite(gitWriteChunk)
	if len(slept) != 1 || slept[0] < 900*time.Millisecond || slept[0] > time.Second {
		t.Fatalf("waits at one chunk a second = %v, want one wait of about 1s", slept)
	}
}

func TestAPacedPushDoesNotYieldToControlCalls(t *testing.T) {
	var slept int
	r := &Room{yieldSleep: func(time.Duration) { slept++ }}
	r.pace.rate = 1 << 20
	r.controlDialing()
	r.yieldToControl()
	if slept != 0 {
		t.Fatalf("a paced git write yielded %d times", slept)
	}
}

func TestABurstOfQueuedEchoesKeepsThePace(t *testing.T) {
	var p pacer
	t0 := time.Unix(1000, 0)
	p.sample(100*time.Millisecond, t0)
	p.delay(1<<20, t0)
	p.delay(1<<10, t0.Add(900*time.Millisecond))
	p.wrote(t0.Add(900 * time.Millisecond))
	p.sample(3*time.Second, t0.Add(time.Second))
	cut := p.rate
	for i := 1; i <= 4; i++ {
		p.sample(3*time.Second-time.Duration(i)*paceBeat, t0.Add(time.Second+time.Duration(i)*time.Millisecond))
	}
	if p.rate != cut {
		t.Fatalf("echoes that came back together moved the rate from %.0f to %.0f", cut, p.rate)
	}
}

func TestADrainingQueueIsNotCutAgain(t *testing.T) {
	var p pacer
	t0 := time.Unix(1000, 0)
	p.sample(100*time.Millisecond, t0)
	p.delay(1<<20, t0)
	p.sample(2*time.Second, t0.Add(time.Second))
	p.delay(1<<20, t0.Add(time.Second))
	p.sample(3*time.Second, t0.Add(5*time.Second))
	cut := p.rate
	p.delay(1<<20, t0.Add(5*time.Second))
	p.sample(2*time.Second, t0.Add(8*time.Second))
	if p.rate != cut {
		t.Fatalf("a queue that fell from 3s to 2s was cut from %.0f to %.0f", cut, p.rate)
	}
}

func TestAWriteBlockedOnAFullOverlayKeepsThePace(t *testing.T) {
	var p pacer
	t0 := time.Unix(1000, 0)
	p.sample(100*time.Millisecond, t0)
	p.delay(1<<20, t0)
	p.wrote(t0)
	p.delay(1<<10, t0.Add(900*time.Millisecond))
	p.sample(3*time.Second, t0.Add(time.Second))
	cut := p.rate
	p.sample(3*time.Second, t0.Add(4*time.Second))
	if p.rate == 0 {
		t.Fatalf("a push blocked in its write for 3s was read as idle, so the pace of %.0f was dropped", cut)
	}
}

func TestAStarvedSampleDoesNotDropTheDrainRateTheClimbAimsFor(t *testing.T) {
	var p pacer
	t0 := time.Unix(1000, 0)
	p.ceil = 1 << 20
	p.sample(100*time.Millisecond, t0)
	p.delay(1<<12, t0)
	p.wrote(t0)
	p.sample(3*time.Second, t0.Add(4*time.Second))
	if p.ceil < 1<<20*paceCut {
		t.Fatalf("4 KiB in 4s took the climb's aim from %d to %.0f B/s", 1<<20, p.ceil)
	}
}
