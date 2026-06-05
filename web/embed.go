//go:build !dev

package web

import (
	"embed"
	"io/fs"
)

//go:embed dist
var distFS embed.FS

// StaticFS serves the embedded frontend files.
var StaticFS fs.FS = distFS
