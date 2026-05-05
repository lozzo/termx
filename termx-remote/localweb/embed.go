package localweb

import (
	"embed"
	"io/fs"
)

//go:embed static/*
var embeddedStatic embed.FS

func EmbeddedAssets() fs.FS {
	static, err := fs.Sub(embeddedStatic, "static")
	if err != nil {
		return NewStaticAssets(map[string]string{
			"index.html": "<!doctype html><title>TermX Local Remote</title><div id=\"root\">TermX Local Remote</div>",
		})
	}
	return static
}
