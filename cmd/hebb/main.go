// Command hebb is the CLI entrypoint for the hebb knowledge-vault engine.
// The command logic lives in the cli package, over the core engine.
package main

import "github.com/cizer/hebb/cli"

// version is overridden at build time via -ldflags.
var version = "0.0.0-dev"

func main() {
	cli.Execute(version)
}
