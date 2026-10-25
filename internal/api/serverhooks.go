package api

import (
	"github.com/dovholuknf/atrium/internal/base/cardproc"
	"github.com/dovholuknf/atrium/internal/store"
)

// serverHooks are the seams a test sets on one Server. Each one, when set, is read instead of the package variable
// the daemon fills in, so a test's setting cannot reach another test's server. A zero value is production.
type serverHooks struct {
	// cardsDir, when non-nil, is the cards folder instead of CardsDir. A pointer so a test can say it is empty.
	cardsDir *string
	// excludedPorts replaces the operating system's reserved port ranges.
	excludedPorts func() []cardproc.Range
	// runnerCompactAtK, heldNoticesOf, owedOf, checkLeanGateway, liveRings and isSupervised replace the package
	// variable of the same name the daemon sets. Setting one to nil takes the package variable.
	runnerCompactAtK func(t *store.Task) int
	heldNoticesOf    func(t *store.Task) (int, string)
	owedOf           func(t *store.Task) (int, string, bool)
	checkLeanGateway func(name string) error
	liveRings        func() (runners, shells int)
	isSupervised     func(taskID string) bool
	// prArchiveCap, when above zero, is the archive cap instead of PRArchiveCap.
	prArchiveCap int64
	// scrapDir, when non-nil, is the scrap folder instead of ScrapDir. A pointer so a test can say it is empty.
	scrapDir *string
	// launchFile, when set, opens a file instead of the operating system's default app.
	launchFile func(path, dir string) error
}

func (s *Server) scrapRoot() string {
	if s.hk.scrapDir != nil {
		return *s.hk.scrapDir
	}
	return ScrapDir
}

func (s *Server) cardsRoot() string {
	if s.hk.cardsDir != nil {
		return *s.hk.cardsDir
	}
	return CardsDir
}

func (s *Server) excluded() []cardproc.Range {
	if s.hk.excludedPorts != nil {
		return s.hk.excludedPorts()
	}
	return excludedPorts()
}

func (s *Server) compactAtK() func(t *store.Task) int {
	if s.hk.runnerCompactAtK != nil {
		return s.hk.runnerCompactAtK
	}
	return RunnerCompactAtK
}

func (s *Server) heldNotices() func(t *store.Task) (int, string) {
	if s.hk.heldNoticesOf != nil {
		return s.hk.heldNoticesOf
	}
	return HeldNoticesOf
}

func (s *Server) owed() func(t *store.Task) (int, string, bool) {
	if s.hk.owedOf != nil {
		return s.hk.owedOf
	}
	return OwedOf
}

func (s *Server) leanGatewayCheck() func(name string) error {
	if s.hk.checkLeanGateway != nil {
		return s.hk.checkLeanGateway
	}
	return CheckLeanGateway
}

func (s *Server) rings() (runners, shells int) {
	if s.hk.liveRings != nil {
		return s.hk.liveRings()
	}
	return liveRings()
}

func (s *Server) supervised() func(taskID string) bool {
	if s.hk.isSupervised != nil {
		return s.hk.isSupervised
	}
	return IsSupervised
}

func (s *Server) archiveCap() int64 {
	if s.hk.prArchiveCap > 0 {
		return s.hk.prArchiveCap
	}
	return PRArchiveCap
}

func (s *Server) launcher() func(path, dir string) error {
	if s.hk.launchFile != nil {
		return s.hk.launchFile
	}
	return osLaunch
}
