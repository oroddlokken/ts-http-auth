//go:build debug

package keep

import (
	"github.com/k0kubun/pp/v3"
)

// This package keeps pp available for debugging.
// Build with -tags debug to include it.
var _ = pp.Println
