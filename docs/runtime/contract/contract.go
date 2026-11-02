// Package contract embeds the runner contract's JSON Schemas, one file per call, so the conformance kit and the
// published files cannot differ.
package contract

import "embed"

//go:embed *.schema.json
var Schemas embed.FS
