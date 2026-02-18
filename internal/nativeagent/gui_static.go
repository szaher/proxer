package nativeagent

import (
	"embed"
	"io/fs"
)

//go:embed static
var guiStaticFiles embed.FS

func guiStaticAssets() (fs.FS, error) {
	return fs.Sub(guiStaticFiles, "static")
}
