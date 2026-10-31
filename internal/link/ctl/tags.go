package ctl

import "strings"

// HasTag reports whether tags holds want, trimmed and case-insensitively.
func HasTag(tags []string, want string) bool {
	for _, t := range tags {
		if strings.EqualFold(strings.TrimSpace(t), want) {
			return true
		}
	}
	return false
}

// OriginTag marks a card that atrium_launch created. See OriginTag in internal/link.
const OriginTag = "origin:agent"
