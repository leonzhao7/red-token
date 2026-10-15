package web

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:dist
var embedded embed.FS

func Handler() http.Handler {
	assets, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}

		if info, statErr := fs.Stat(assets, name); statErr == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		// Missing assets should remain 404s; all other paths are SPA routes.
		if path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}

		index, readErr := fs.ReadFile(assets, "index.html")
		if readErr != nil {
			http.Error(w, "frontend assets are not available", http.StatusNotFound)
			return
		}
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
	})
}
