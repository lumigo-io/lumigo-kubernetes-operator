//go:build tools
// +build tools

package tools

import (
	_ "github.com/tcnksm/ghr"
	_ "go.opentelemetry.io/collector/cmd/builder"
	_ "golang.org/x/tools/cmd/goimports"
)
