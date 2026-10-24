package web

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/markbrutx/greenroom/internal/auth"
)

type loginData struct {
	Name string
	Next string
	Err  string
}

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
	m.HandleFunc("GET /login", app.loginPage)
	m.HandleFunc("POST /login", app.login)
	m.HandleFunc("POST /logout", app.logout)
	app.Page("GET /{$}", app.home)
}

func (app *App) home(w http.ResponseWriter, r *http.Request) {
	if len(app.tools) == 0 {
		http.Error(w, "No tools are registered.", http.StatusNotFound)
		return
	}
	http.Redirect(w, r, "/"+app.tools[0].Slug, http.StatusSeeOther)
}

func (app *App) parsePages() error {
	var err error
	app.loginT, err = app.Templates(templateFS, "templates/login.html")
	return err
}

func (app *App) loginPage(w http.ResponseWriter, r *http.Request) {
	next := auth.SafeNext(r.URL.Query().Get("next"))
	if _, ok := app.Auth.Identify(r); ok {
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	app.renderLogin(w, r, loginData{Name: auth.LastName(r), Next: next}, http.StatusOK)
}

func (app *App) renderLogin(w http.ResponseWriter, r *http.Request, d loginData, status int) {
	app.RenderStatus(w, r, app.loginT, View{Title: "Sign in", Data: d}, status)
}

func (app *App) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "The sign-in form is too large.", http.StatusRequestEntityTooLarge)
		return
	}
	next := auth.SafeNext(r.PostForm.Get("next"))
	name, err := app.Auth.Login(r.Context(), app.Auth.ClientIP(r), r.PostForm.Get("name"), r.PostForm.Get("password"))
	if err != nil {
		status := http.StatusUnauthorized
		var blocked auth.BlockedError
		switch {
		case errors.As(err, &blocked):
			status = http.StatusTooManyRequests
		case errors.Is(err, auth.ErrNoName), errors.Is(err, auth.ErrWrongPass):
		default:
			app.Log.Error("login", "err", err)
			err = errors.New("The database did not answer. Try again in a moment.")
			status = http.StatusInternalServerError
		}
		app.renderLogin(w, r, loginData{Name: auth.CleanName(r.PostForm.Get("name")), Next: next, Err: err.Error()}, status)
		return
	}
	app.Log.Info("sign in", "user", name, "ip", app.Auth.ClientIP(r))
	app.Auth.StartSession(w, r, name)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (app *App) logout(w http.ResponseWriter, r *http.Request) {
	app.Auth.EndSession(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
