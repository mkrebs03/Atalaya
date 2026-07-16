package web

import (
	"embed"
	"io/fs"
)

//go:embed dist
var embeddedAssets embed.FS

var assets = mustSub(embeddedAssets, "dist")

// Assets returns the immutable filesystem containing the built Vue application.
func Assets() fs.FS {
	return assets
}

func mustSub(root fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(root, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
