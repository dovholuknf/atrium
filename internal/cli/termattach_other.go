//go:build !windows

package cli

// enableVT is nothing outside Windows: a terminal there already speaks escape sequences.
func enableVT() func() { return func() {} }
