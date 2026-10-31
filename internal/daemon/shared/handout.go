package shared

// DispatchHandout is what a room is told to start, in the reply to its
// check-in.
//
// A SEPARATE SHAPE FROM `store.Dispatch` on purpose. What crosses is an
// instruction, not a row: no token bookkeeping the room has no use for, no
// attempt count, and no state, because the only state a room can observe is
// "you have this now".
type DispatchHandout struct {
	ID      string   `json:"id"`
	Token   string   `json:"token"`
	Harness string   `json:"harness"`
	Cwd     string   `json:"cwd,omitempty"`
	Title   string   `json:"title,omitempty"`
	Prompt  string   `json:"prompt,omitempty"`
	Why     string   `json:"why,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Window  string   `json:"window,omitempty"`
}
