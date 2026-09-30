- **A card says when it last replied.** The card view carries `output_at`, RFC3339, the time its transcript last
  gained an assistant reply with text, mid-turn too, and a card is published on the event stream when it moves. Read
  a moment after every activity hook, and on the reaper's tick for a runner without hooks. Never stored. For /m,
  which re-reads a card's replies when it moves instead of only at turn end. Room side, needs a room deploy.
  (r-new-output-at, with @ui)
