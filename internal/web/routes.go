package web

import (
	"context"
	"net/http"
	"time"
)

func (app *App) routes() {
	m := app.mux
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	m.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := app.DB.Ping(ctx); err != nil {
			http.Error(w, "database unreachable", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok\n"))
	})
	m.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("User-agent: *\nDisallow: /\n"))
	})
	m.HandleFunc("GET /static/", app.serveStatic)
	m.HandleFunc("GET /brand.css", app.serveBrandCSS)
	m.HandleFunc("GET /brand/logo", app.serveLogo)
	m.HandleFunc("GET /favicon.ico", app.serveLogo)
}
