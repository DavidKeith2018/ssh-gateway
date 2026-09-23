package gateway

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web/dist
var assets embed.FS

//go:embed packaging/icon.png
var desktopIcon []byte

func DesktopIcon() []byte { return desktopIcon }

func assetHandler() http.Handler {
	root, _ := fs.Sub(assets, "web/dist")
	return http.FileServer(http.FS(root))
}

func Assets() fs.FS { root, _ := fs.Sub(assets, "web/dist"); return root }
