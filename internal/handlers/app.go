package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"easybook/internal/config"
	"easybook/internal/models"
	"easybook/internal/session"
	"easybook/internal/view"
)

type App struct {
	Env      config.Env
	Store    *models.Store
	Sessions *session.Manager
	Renderer *view.Renderer
	ViewsDir string
}

func NewApp(env config.Env, store *models.Store, sessions *session.Manager, renderer *view.Renderer, viewsDir string) *App {
	return &App{
		Env:      env,
		Store:    store,
		Sessions: sessions,
		Renderer: renderer,
		ViewsDir: viewsDir,
	}
}

func (a *App) withError(handler func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := handler(w, r); err != nil {
			a.handleError(w, r, err)
		}
	}
}

func (a *App) handleError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("Unhandled error: %v", err)
	if strings.HasPrefix(r.URL.Path, "/api") {
		a.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Internal server error"})
		return
	}
	http.Error(w, "Internal server error", http.StatusInternalServerError)
}

func (a *App) renderHTML(w http.ResponseWriter, status int, fileName string, replacements map[string]any) error {
	html, err := a.Renderer.Render(fileName, replacements)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(html))
	return nil
}

func (a *App) sendStaticPage(w http.ResponseWriter, r *http.Request, fileName string, statusCode int) error {
	fullPath := filepath.Join(a.ViewsDir, fileName)
	if _, err := os.Stat(fullPath); err != nil {
		return err
	}

	w.WriteHeader(statusCode)
	http.ServeFile(w, r, fullPath)
	return nil
}

func (a *App) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (a *App) parsePayload(r *http.Request) (map[string]any, error) {
	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if strings.Contains(contentType, "application/json") {
		defer r.Body.Close()
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()

		payload := map[string]any{}
		if err := decoder.Decode(&payload); err != nil {
			if errors.Is(err, io.EOF) {
				return payload, nil
			}
			return nil, fmt.Errorf("decode json payload: %w", err)
		}
		return payload, nil
	}

	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("parse form payload: %w", err)
	}
	payload := map[string]any{}
	for key, values := range r.Form {
		if len(values) == 0 {
			continue
		}
		if len(values) == 1 {
			payload[key] = values[0]
			continue
		}
		valuesCopy := make([]string, len(values))
		copy(valuesCopy, values)
		payload[key] = valuesCopy
	}
	return payload, nil
}

func (a *App) NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api") {
		a.writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}

	_ = a.sendStaticPage(w, r, "404.html", http.StatusNotFound)
}
