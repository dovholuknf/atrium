package daemon

import (
	"fmt"
	"sort"
	"strings"
)

// Reading a configuration back off a checkout.
//
// The mirror of `export.go`, and the dangerous direction. An export that is
// missing something costs a rebuild. An import that overwrites something costs
// whatever it overwrote, and the operator finds out later.
//
// SO IT NEVER OVERWRITES SILENTLY. Anything already set is left alone and
// reported. `force` overwrites, and it is a decision somebody has to make
// having read the list of what would change.
//
// Nothing here can bring a credential back, because none was ever exported. A
// machine restored from a checkout still has to be enabled against zrok and
// still has to enroll a ziti identity. That is the design working rather than a
// gap in it: the file holds what a repository should hold.

// ImportChange is one thing an import would do, or did.
type ImportChange struct {
	// Kind is `setting`, `harness`, `fixture`, `source`, `action`, `rule`,
	// `recogniser`, `theme` or `overlay`.
	Kind string `json:"kind"`
	// Name is which one, in whatever way that kind is identified.
	Name string `json:"name"`
	// Action is `add`, `replace` or `keep`.
	Action string `json:"action"`
	// Why explains a `keep`, which is the only one that needs explaining.
	Why string `json:"why,omitempty"`
}

// ImportResult is what an import would do, or what it did.
type ImportResult struct {
	// Applied is false for a dry run, which is the default.
	Applied bool           `json:"applied"`
	Changes []ImportChange `json:"changes"`
	// Kept counts what was left alone, so a caller can say "12 already set"
	// without walking the list.
	Kept int    `json:"kept"`
	Note string `json:"note,omitempty"`
}

// ApplyImport reads a configuration in, or says what it would do.
//
// `apply` false is the default and the useful one: it answers "what would this
// change", which is the question somebody restoring a machine actually has.
func (d *Daemon) ApplyImport(in *Export, apply, force bool) (*ImportResult, error) {
	if in == nil {
		return nil, fmt.Errorf("nothing to import")
	}
	// A version this daemon does not understand is refused whole rather than
	// applied in part. Half a configuration is worse than none, because it
	// looks like it worked.
	if in.Version != ExportVersion {
		return nil, fmt.Errorf("this file is version %d and this atrium reads version %d. "+
			"importing it could apply half of a shape it does not understand",
			in.Version, ExportVersion)
	}

	res := &ImportResult{Applied: apply}

	// Settings, and ONLY the ones the export was allowed to carry. A file that
	// has been edited by hand, or written by something else, does not get to
	// name a key the export itself would refuse.
	allowed := map[string]bool{}
	for _, k := range exportedSettings {
		allowed[k] = true
	}
	keys := make([]string, 0, len(in.Settings))
	for k := range in.Settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !allowed[k] {
			res.Changes = append(res.Changes, ImportChange{
				Kind: "setting", Name: k, Action: "keep",
				Why: "not a setting atrium exports, so not one it will import",
			})
			res.Kept++
			continue
		}
		have, err := d.st.Setting(k)
		if err != nil {
			return nil, err
		}
		switch {
		case strings.TrimSpace(have) == "":
			res.Changes = append(res.Changes, ImportChange{
				Kind: "setting", Name: k, Action: "add"})
			if apply {
				if err := d.st.SetSetting(k, in.Settings[k]); err != nil {
					return nil, err
				}
			}
		case have == in.Settings[k]:
			// Already what the file says. Not a change and not worth reporting
			// as one, or a restore onto the machine it came from reads as
			// rewriting everything.
		case force:
			res.Changes = append(res.Changes, ImportChange{
				Kind: "setting", Name: k, Action: "replace"})
			if apply {
				if err := d.st.SetSetting(k, in.Settings[k]); err != nil {
					return nil, err
				}
			}
		default:
			res.Changes = append(res.Changes, ImportChange{
				Kind: "setting", Name: k, Action: "keep",
				Why: "already set here to something else. pass force to overwrite",
			})
			res.Kept++
		}
	}

	for _, h := range in.Harnesses {
		if h == nil {
			continue
		}
		exists := false
		if got, err := d.st.Harness(h.ID); err == nil && got != nil {
			exists = true
		}
		change, do := decide("harness", h.ID, exists, force)
		res.Changes = append(res.Changes, change)
		if change.Action == "keep" {
			res.Kept++
		}
		if apply && do {
			if _, err := d.st.SaveHarness(*h); err != nil {
				return nil, fmt.Errorf("harness %s: %w", h.ID, err)
			}
		}
	}
	for _, s := range in.Sources {
		if s == nil {
			continue
		}
		exists := false
		if got, err := d.st.SourceByID(s.ID); err == nil && got != nil {
			exists = true
		}
		change, do := decide("source", s.ID, exists, force)
		res.Changes = append(res.Changes, change)
		if change.Action == "keep" {
			res.Kept++
		}
		if apply && do {
			if _, err := d.st.SaveSource(*s); err != nil {
				return nil, fmt.Errorf("source %s: %w", s.ID, err)
			}
		}
	}

	for _, rec := range in.Recognisers {
		if rec == nil {
			continue
		}
		exists := false
		if got, err := d.st.RecogniserByID(rec.ID); err == nil && got != nil {
			exists = true
		}
		change, do := decide("recogniser", rec.ID, exists, force)
		res.Changes = append(res.Changes, change)
		if change.Action == "keep" {
			res.Kept++
		}
		if apply && do {
			// SaveRecogniser compiles the pattern, so a hand-edited file with a
			// broken expression in it fails the import naming the row rather
			// than landing a row that can never match.
			if _, err := d.st.SaveRecogniser(*rec); err != nil {
				return nil, fmt.Errorf("recogniser %s: %w", rec.ID, err)
			}
		}
	}

	// The brought terminal palettes, by the same rule as everything else here:
	// add what is missing, keep what is there, replace only when told.
	//
	// Every one goes through `SaveTermTheme`, which validates. A file edited by
	// hand, or written by something that is not atrium, cannot put a value that
	// is not a colour into this table by arriving as a configuration rather
	// than through the editor.
	for _, th := range in.Themes {
		have, err := d.st.TermThemeByName(th.Name)
		if err != nil {
			return nil, err
		}
		change, do := decide("theme", th.Name, have != nil, force)
		res.Changes = append(res.Changes, change)
		if change.Action == "keep" {
			res.Kept++
		}
		if apply && do {
			if _, err := d.st.SaveTermTheme(th); err != nil {
				return nil, fmt.Errorf("theme %s: %w", th.Name, err)
			}
		}
	}

	// OVERLAY CONFIGURATION IS MERGED, NEVER REPLACED, and this is the one
	// place that matters most.
	//
	// The stored config holds credentials the export never carried. Writing the
	// imported shape over it would erase the account token and the identity
	// path with values that were deliberately absent from the file, and the
	// machine would look configured and refuse to share.
	zrok := d.zrokConfig()
	before := zrok
	zrok.Public, zrok.Private = in.Overlays.Zrok.Public, in.Overlays.Zrok.Private
	zrok.OwnEnvironment = in.Overlays.Zrok.OwnEnvironment
	if in.Overlays.Zrok.ApiEndpoint != "" {
		zrok.ApiEndpoint = in.Overlays.Zrok.ApiEndpoint
	}
	zrok.normalise()
	if zrok != before {
		res.Changes = append(res.Changes, ImportChange{
			Kind: "overlay", Name: "zrok", Action: "replace",
			Why: "sharing options and instance only. the account token here is untouched",
		})
		if apply {
			if err := d.saveOverlayConfig(SettingOverlayZrok, zrok); err != nil {
				return nil, err
			}
		}
	}
	if svc := strings.TrimSpace(in.Overlays.Ziti.Service); svc != "" {
		ziti := d.zitiConfig()
		if ziti.Service != svc {
			res.Changes = append(res.Changes, ImportChange{
				Kind: "overlay", Name: "ziti", Action: "replace",
				Why: "service name only. the identity here is untouched",
			})
			if apply {
				ziti.Service = svc
				if err := d.saveOverlayConfig(SettingOverlayZiti, ziti); err != nil {
					return nil, err
				}
			}
		}
	}

	if !apply {
		res.Note = "nothing was written. this is what importing would do."
	}
	if res.Kept > 0 {
		res.Note = strings.TrimSpace(res.Note + " " + fmt.Sprintf(
			"%d thing(s) already set here were left alone.", res.Kept))
	}
	// Said every time, because it is the question somebody restoring a machine
	// asks next and the answer is not obvious from a list of changes.
	res.Note = strings.TrimSpace(res.Note + " no credentials are in this file, so zrok " +
		"still has to be enabled and a ziti identity still has to be enrolled.")
	return res, nil
}

// decide is the same rule for every keyed row: add what is missing, keep what
// is there, replace only when told.
func decide(kind, name string, exists, force bool) (ImportChange, bool) {
	switch {
	case !exists:
		return ImportChange{Kind: kind, Name: name, Action: "add"}, true
	case force:
		return ImportChange{Kind: kind, Name: name, Action: "replace"}, true
	default:
		return ImportChange{
			Kind: kind, Name: name, Action: "keep",
			Why: "already here. pass force to overwrite",
		}, false
	}
}
