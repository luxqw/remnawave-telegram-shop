package webapp

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
)

// distFS embeds the built frontend (web/admin -> npm run build -> dist/, copied here by the
// Dockerfile's frontend build stage). A placeholder index.html is committed at
// static/dist/index.html so `go build ./...` never breaks for contributors who haven't run the
// frontend build.
//
//go:embed static/dist
var distFS embed.FS

// staticHandler serves the embedded SPA under the /admin/ prefix, falling back to index.html for
// any path that isn't a real asset (client-side routes like /admin/#/users/123).
func (h *Handler) staticHandler() http.Handler {
	sub, err := fs.Sub(distFS, "static/dist")
	if err != nil {
		panic(fmt.Errorf("webapp: embed static/dist: %w", err))
	}
	fileServer := http.StripPrefix("/admin", http.FileServer(http.FS(sub)))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqPath := strings.TrimPrefix(r.URL.Path, "/admin")
		reqPath = strings.TrimPrefix(reqPath, "/")
		if reqPath == "" {
			reqPath = "index.html"
		}
		if _, statErr := fs.Stat(sub, reqPath); statErr != nil {
			data, readErr := fs.ReadFile(sub, "index.html")
			if readErr != nil {
				http.Error(w, "index.html missing from embedded static assets", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(data)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
