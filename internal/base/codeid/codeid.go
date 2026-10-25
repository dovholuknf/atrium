// Package codeid fingerprints the code a room runs, apart from the board it serves.
//
// The board hash (`api.BuildID`) moves with every CSS edit, so a hub redeployed with a board-only change differs from
// every room that runs identical Go. This hash covers the Go sources, go.mod and go.sum only: the room's behaviour.
// It is stamped at build with `-X .../internal/api.CodeID=$(go run ./tools/codeid)` and reported beside `build`.
package codeid

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	pathpkg "path"
	"strings"
)

// Compute hashes the tree rooted at fsys. Paths and bytes go in, in lexical order, with CRLF folded to LF so a Windows
// checkout and a Linux one agree.
func Compute(fsys fs.FS) (string, error) {
	sum := sha256.New()
	err := fs.WalkDir(fsys, ".", func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			if path != "." && skipDir(path) {
				return fs.SkipDir
			}
			return nil
		}
		if !counts(path) || !e.Type().IsRegular() {
			return nil
		}
		raw, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		sum.Write([]byte(path + "\x00"))
		sum.Write(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")))
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)[:8]), nil
}

// skipDir is a directory that holds no room code: the board, the docs, build output, and anything hidden.
func skipDir(path string) bool {
	switch path {
	case "internal/api/web", "docs", "website", "node_modules", "build.claude", "changelog":
		return true
	}
	base := pathpkg.Base(path)
	return strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") || base == "testdata"
}

// counts is a file that is part of the fingerprint: a Go source that is not a test, go.mod and go.sum.
func counts(path string) bool {
	switch path {
	case "go.mod", "go.sum":
		return true
	}
	return strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go")
}
