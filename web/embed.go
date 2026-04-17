package webui

import (
	"embed"
	"io/fs"
)

//go:embed dist
var embeddedDist embed.FS

func DistFS() fs.FS {
	distFS, err := fs.Sub(embeddedDist, "dist")
	if err != nil {
		return nil
	}
	return distFS
}
