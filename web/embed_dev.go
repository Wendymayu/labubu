//go:build dev

package web

import (
	"io/fs"
	"os"
)

// StaticFS reads from disk in dev mode so npm run dev works.
var StaticFS fs.FS = os.DirFS("web/dist")
