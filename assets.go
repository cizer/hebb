// Package hebb embeds the function-layer assets (skills, automation scripts,
// and the vault template) into the binary so hebb runs standalone, with no repo
// checkout required. `hebb install` materialises these onto disk (the hebb data
// dir) and links the skills into ~/.claude/skills. A repo checkout is only
// needed for development, via `hebb install --asset-root <repo>`.
package hebb

import "embed"

// Assets carries the function-layer content shipped inside the binary.
//
//go:embed all:skills all:automation all:vault-template
var Assets embed.FS
