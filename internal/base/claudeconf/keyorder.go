package claudeconf

import (
	"bytes"
	"encoding/json"
	"sort"
)

// marshalInOrder writes doc indented like json.MarshalIndent(doc, "", "  "), with its top-level keys in the order
// they had in orig and any new key after them sorted, so a rewrite of settings.json diffs only where it changed.
func marshalInOrder(orig []byte, doc map[string]json.RawMessage) ([]byte, error) {
	var keys []string
	seen := map[string]bool{}
	dec := json.NewDecoder(bytes.NewReader(orig))
	if t, err := dec.Token(); err == nil && t == json.Delim('{') {
		for dec.More() {
			t, err := dec.Token()
			if err != nil {
				break
			}
			k, _ := t.(string)
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				break
			}
			if _, ok := doc[k]; ok && !seen[k] {
				keys = append(keys, k)
				seen[k] = true
			}
		}
	}
	var added []string
	for k := range doc {
		if !seen[k] {
			added = append(added, k)
		}
	}
	sort.Strings(added)
	keys = append(keys, added...)
	if len(keys) == 0 {
		return []byte("{}"), nil
	}
	var b bytes.Buffer
	b.WriteString("{\n")
	for i, k := range keys {
		name, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		val, err := json.Marshal(doc[k])
		if err != nil {
			return nil, err
		}
		b.WriteString("  ")
		b.Write(name)
		b.WriteString(": ")
		if err := json.Indent(&b, val, "  ", "  "); err != nil {
			return nil, err
		}
		if i < len(keys)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
