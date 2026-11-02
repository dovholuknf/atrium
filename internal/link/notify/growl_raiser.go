package notify

import "log"

// GrowlRaiser is what a growl store that can hold a question about no card adds to GrowlStore. A store without it
// raises none, and the event still goes out.
type GrowlRaiser interface {
	// Raise raises the row under its own id on a room, once. See hubstore.Store.GrowlRaise.
	Raise(room string, g GrowlRow) (raised bool, err error)
	// End ends the row with that id. See hubstore.Store.GrowlEnd.
	End(id string) (changed bool, err error)
}

// Raiser is the growler's store as a GrowlRaiser, false when the store cannot raise a question about no card.
func (g *Growler) Raiser() (GrowlRaiser, bool) {
	rs, ok := g.st.(GrowlRaiser)
	return rs, ok
}

// Live lists the rows still open.
func (g *Growler) Live() ([]GrowlRow, error) { return g.st.Live() }

// Publish sends the set to the board now.
func (g *Growler) Publish() { g.publish(nil) }

// Ask raises the row on a room once and publishes when it is new. A store that cannot raise one does nothing.
func (g *Growler) Ask(room string, row GrowlRow) {
	rs, ok := g.Raiser()
	if !ok {
		return
	}
	raised, err := rs.Raise(room, row)
	if err != nil {
		log.Printf("[hub] growlers could not raise %s: %v", row.ID, err)
		return
	}
	if raised {
		g.Publish()
	}
}

// EndAbout ends the question when its request is over.
func (g *Growler) EndAbout(id string) {
	rs, ok := g.Raiser()
	if !ok {
		return
	}
	changed, err := rs.End(id)
	if err != nil {
		log.Printf("[hub] growlers could not end %s: %v", id, err)
		return
	}
	if changed {
		g.Publish()
	}
}
