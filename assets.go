// Package hebb embeds the function-layer assets (automation scripts, the vault
// template, and the agent skills) into the binary so hebb runs standalone, with
// no repo checkout required. `hebb install` materialises the automation scripts
// onto disk (the hebb data dir) for launchd jobs, `hebb new` scaffolds from the
// embedded vault template, and `hebb codex` materialises the skills into Codex's
// skills dir. The skills are also published to Claude Code via the plugin (see
// plugin/); the embedded copy is the same files, so there is one source of
// truth. A repo checkout is only needed for development, via --asset-root.
package hebb

import "embed"

// Assets carries the function-layer content shipped inside the binary.
//
//go:embed all:automation all:vault-template all:plugin/skills
var Assets embed.FS
