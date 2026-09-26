//go:build tools

// This file pins the build-time tools the server needs. It is never compiled
// into the server: the "tools" build tag keeps it out of every build, but
// `go mod tidy` and CI read it, so the pinned version stays in go.mod/go.sum
// and `go run` resolves it reproducibly from the module cache.
//
// The code generator itself is invoked by scripts/gen.sh:
//
//	go run github.com/atombender/go-jsonschema@v0.24.1 ...
//
// Keep the version in tools.go, scripts/gen.sh and go.mod in sync; bumping the
// generator is one commit that regenerates internal/protocol/gen.
package tools

import (
	// Code generator behind scripts/gen.sh and `make gen`.
	_ "github.com/atombender/go-jsonschema"
)
