// Package assets embeds the runtime resources distributed with the executable.
// It lives at the repository root so go:embed can include the canonical native
// sources, UI, build recipe, and license notices without maintaining copies.
package assets

import "embed"

// Native contains the source and build inputs extracted by setup.
//
//go:embed Makefile LICENSE THIRD_PARTY.md native/*
var Native embed.FS

// Console contains the Quickshell permission UI.
//
//go:embed quickshell/*.qml
var Console embed.FS
