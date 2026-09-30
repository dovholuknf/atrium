- docs/rnd/room-deploy-hold-design.md: one call asks for a room deploy, the deploy owner holds the room (every gated
  call refused with "end your turn and wait", agent messages held), redeploys, and every held card gets one wake line.
- docs/rnd/freeze-budget-design.md: clint's r-037 answers folded in (tree spend with a soft then hard stop, separate
  spend and cache-read limits), and section 1 is now the hold step both designs share.
