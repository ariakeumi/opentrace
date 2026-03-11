package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/*
var staticFiles embed.FS

var content fs.FS

func init() {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	content = sub
}

func ServeIndex(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(content, "index.html")
	if err != nil {
		http.Error(w, "index unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func StaticHandler(name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, content, name)
	})
}
