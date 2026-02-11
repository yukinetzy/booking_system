package middleware

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[%s] %s %s", time.Now().UTC().Format(time.RFC3339), r.Method, r.URL.RequestURI())
		next.ServeHTTP(w, r)
	})
}

func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("Unhandled panic: %v", recovered)
				if IsAPIRequest(r) {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "Internal server error"})
					return
				}
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

func StaticMiddleware(publicDir string) func(http.Handler) http.Handler {
	absolutePublic, err := filepath.Abs(publicDir)
	if err != nil {
		absolutePublic = publicDir
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				next.ServeHTTP(w, r)
				return
			}

			cleanPath := path.Clean("/" + r.URL.Path)
			if cleanPath == "/" {
				next.ServeHTTP(w, r)
				return
			}
			if strings.Contains(cleanPath, "..") {
				next.ServeHTTP(w, r)
				return
			}

			candidate := filepath.Join(absolutePublic, filepath.FromSlash(strings.TrimPrefix(cleanPath, "/")))
			absoluteCandidate, err := filepath.Abs(candidate)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			if !strings.HasPrefix(strings.ToLower(absoluteCandidate), strings.ToLower(absolutePublic)+strings.ToLower(string(filepath.Separator))) {
				next.ServeHTTP(w, r)
				return
			}

			info, err := os.Stat(absoluteCandidate)
			if err != nil || info.IsDir() {
				next.ServeHTTP(w, r)
				return
			}

			http.ServeFile(w, r, absoluteCandidate)
		})
	}
}

func LogStartup(port int) {
	fmt.Printf("Server running on port %d\n", port)
	fmt.Printf("Open: http://127.0.0.1:%d\n", port)
}
