package dockertemplate

import "embed"

//go:embed flake.lock flake.nix
var FS embed.FS
