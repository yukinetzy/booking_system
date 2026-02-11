package middleware

import (
	"net/http"
	"net/url"
	"strings"

	"easybook/internal/session"
)

func IsAPIRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	return strings.HasPrefix(r.URL.Path, "/api")
}

func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if session.CurrentUser(r) != nil {
			next.ServeHTTP(w, r)
			return
		}

		if IsAPIRequest(r) {
			writeJSONError(w, http.StatusUnauthorized, "Authentication required")
			return
		}

		nextPath := r.URL.RequestURI()
		if nextPath == "" {
			nextPath = "/"
		}
		http.Redirect(w, r, "/login?next="+url.QueryEscape(nextPath), http.StatusFound)
	})
}

func RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowedRoles := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		role = strings.TrimSpace(role)
		if role != "" {
			allowedRoles[role] = struct{}{}
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := session.CurrentUser(r)
			if user == nil {
				RequireAuth(next).ServeHTTP(w, r)
				return
			}

			if _, ok := allowedRoles[user.Role]; ok {
				next.ServeHTTP(w, r)
				return
			}

			if IsAPIRequest(r) {
				writeJSONError(w, http.StatusForbidden, "Forbidden")
				return
			}

			http.Error(w, "Forbidden", http.StatusForbidden)
		})
	}
}

func CanAccessOwnerResource(r *http.Request, ownerID string) bool {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return false
	}

	user := session.CurrentUser(r)
	if user == nil {
		return false
	}
	if user.Role == "admin" {
		return true
	}

	return user.ID == ownerID
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte("{\"error\":\"" + message + "\"}"))
}
