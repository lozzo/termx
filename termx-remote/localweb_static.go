package remote

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed localweb/static
var embeddedLocalWebFS embed.FS

func newLocalHubHTTPHandler(hub http.Handler) http.Handler {
	staticFS, err := fs.Sub(embeddedLocalWebFS, "localweb/static")
	if err != nil {
		return hub
	}
	static := http.FileServer(http.FS(staticFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			hub.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			hub.ServeHTTP(w, r)
			return
		}
		static.ServeHTTP(w, r)
	})
}
