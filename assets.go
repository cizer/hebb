// Package hebb embeds the function-layer assets (automation scripts, the vault
// template, and the agent skills) into the binary so hebb runs standalone, with
// no repo checkout required. `hebb install` materialises the automation scripts
// onto disk (the hebb data dir) for launchd jobs and copies the agent skills
// into ~/.claude/skills for plain Claude Code, `hebb new` scaffolds from the
// embedded vault template, and `hebb codex` materialises the skills into Codex's
// skills dir. The hebb Claude Code plugin (see plugin/) additionally publishes
// the same skills namespaced (hebb:<name>); plugin and embedded copies are the
// same files, so there is one source of truth. A repo checkout is only needed
// for development, via --asset-root.
package hebb

import "embed"

// Assets carries the function-layer content shipped inside the binary.
//
//go:embed all:automation all:vault-template all:plugin/skills
var Assets embed.FS
