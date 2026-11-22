// Package render is step 8 of the pulls-view recipe: merged finding JSON
// in, finding files and walk.txt out. It is pure on purpose. No store, no network, no model. The only things it
// reads are the strings and the run folder's src/ it is handed, so the runner can call it, resend what it refuses,
// and call it again.
package render

import "encoding/json"

// Exposure is rule 26's three answers. All three are needed for a MED or higher.
type Exposure struct {
	Who    string `json:"who"`
	Likely string `json:"likely"`
	OptIn  string `json:"opt_in"`
}

// Finding is one entry of the merge step's JSON. Dispute, Settled and LeftForOperator are what step 7 adds.
type Finding struct {
	ID       string   `json:"id,omitempty"`
	Sev      string   `json:"sev"`
	Path     string   `json:"path"`
	Line     int      `json:"line"`
	Code     string   `json:"code"`
	Says     string   `json:"says"`
	Fix      string   `json:"fix"`
	TestAsk  string   `json:"test_ask"`
	Proven   string   `json:"proven"`
	Impact   string   `json:"impact"`
	Rank     int      `json:"rank"`
	Exposure Exposure `json:"exposure"`
	Cause    string   `json:"cause"`
	Found    string   `json:"found"`
	Leak     string   `json:"leak"`
	RaisedBy string   `json:"raised_by"`

	Dispute         string `json:"dispute,omitempty"`
	Settled         string `json:"settled,omitempty"`
	LeftForOperator bool   `json:"left_for_operator,omitempty"`
}

// UnmarshalJSON also reads left_for_clint, the key older findings files were written with.
func (f *Finding) UnmarshalJSON(raw []byte) error {
	type plain Finding
	var v struct {
		plain
		Old bool `json:"left_for_clint"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	*f = Finding(v.plain)
	f.LeftForOperator = f.LeftForOperator || v.Old
	return nil
}

// Input is everything a render reads.
type Input struct {
	// PRURL is https://github.com/<org>/<repo>/pull/<n>. The first line of every file and the base of every link.
	PRURL string
	// Diff is pr.diff, as fetched.
	Diff string
	// RunDir holds src/. Empty means rule 40 cannot be checked and is skipped.
	RunDir string
	// Findings is the merged list.
	Findings []Finding
	// Raw is every reviewer's and verifier's findings before the merge, for the leak check.
	Raw []Finding
	// Resent is set by the runner on the call after the merge fork answered a Resend. A second failure then acts.
	Resent bool
}

// File is one finding file, not yet on disk.
type File struct {
	Name string
	Text string
}

// Result is what a render that was not refused returns.
type Result struct {
	Files []File
	Walk  string
}
