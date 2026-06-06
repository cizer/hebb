// Package hebb embeds the function-layer assets (automation scripts and the
// vault template) into the binary so hebb runs standalone, with no repo checkout
// required. `hebb install` materialises the automation scripts onto disk (the
// hebb data dir) for launchd jobs, and `hebb new` scaffolds from the embedded
// vault template. The agent-facing skills ship in the hebb Claude Code plugin
// (see plugin/), not the binary. A repo checkout is only needed for development,
// via --asset-root.
package hebb

import "embed"

// Assets carries the function-layer content shipped inside the binary.
//
//go:embed all:automation all:vault-template
var Assets embed.FS
