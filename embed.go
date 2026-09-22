// Package claudebox holds files that belong at the project root, where anyone
// opening the repository sees them, but that a binary still has to carry.
//
// go:embed cannot reach above its own package, so the embed lives here rather
// than beside the code that uses it. internal/commandspec parses these bytes;
// this package only supplies them, so there is one copy of the file and no
// build step keeping two in step.
package claudebox

import _ "embed"

// CommandsExample is the default slash-command allowlist: what cbx writes to a
// box that has no spec yet, and what cbx-setuptool uploads.
//
//go:embed commands.example.yaml
var CommandsExample []byte

// OpenAPI describes the HTTP API. Served by `cbx serve` at /openapi.yaml and
// /openapi.json, and checked against the server's route table by a test.
//
//go:embed openapi.yaml
var OpenAPI []byte
