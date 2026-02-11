package handlers

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"easybook/internal/models"
	"easybook/internal/session"
	"easybook/internal/view"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const presenceCookieName = "presence_token"

var presenceTokenPattern = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

var globalPresenceRequestLimiter = newPresenceRequestLimiter()

type presenceRequestLimiter struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func newPresenceRequestLimiter() *presenceRequestLimiter {
	return &presenceRequestLimiter{
		last: map[string]time.Time{},
	}
}

func (l *presenceRequestLimiter) Allow(key string, minInterval time.Duration) bool {
	key = strings.TrimSpace(key)
	if key == "" || minInterval <= 0 {
		return true
	}

	now := time.Now().UTC()
	l.mu.Lock()
	defer l.mu.Unlock()

	if previous, exists := l.last[key]; exists && now.Sub(previous) < minInterval {
		return false
	}
	l.last[key] = now

	if len(l.last) > 10000 {
		cutoff := now.Add(-10 * minInterval)
		for itemKey, itemTime := range l.last {
			if itemTime.Before(cutoff) {
				delete(l.last, itemKey)
			}
		}
	}

	return true
}

func (a *App) renderHotelWaitPage(w http.ResponseWriter, r *http.Request) error {
	hotelID := strings.TrimSpace(r.URL.Query().Get("hotelId"))
	if _, err := primitive.ObjectIDFromHex(hotelID); err != nil {
		return sendHotelNotFoundPage(a, w, r, http.StatusBadRequest)
	}

	if !a.Env.PresenceEnabled {
		http.Redirect(w, r, "/hotels/"+hotelID, http.StatusFound)
		return nil
	}

	hotel, err := a.Store.FindHotelByID(r.Context(), hotelID, nil)
	if err != nil {
		return err
	}
	if hotel == nil {
		return sendHotelNotFoundPage(a, w, r, http.StatusNotFound)
	}

	status, err := a.Store.GetHotelPresenceStatus(r.Context(), hotelID, a.Env.PresenceCapacity)
	if err != nil {
		if errors.Is(err, models.ErrInvalidPresencePayload) {
			return sendHotelNotFoundPage(a, w, r, http.StatusBadRequest)
		}
		return err
	}

	return a.renderHTML(w, http.StatusOK, "hotel-wait.html", map[string]any{
		"authControls": view.Safe(renderAuthControls(session.CurrentUser(r), "/hotel-wait?hotelId="+url.QueryEscape(hotelID))),
		"hotelId":      hotelID,
		"hotelTitle":   defaultIfEmpty(stringValue(hotel, "title"), "Selected hotel"),
		"activeUsers":  strconv.FormatInt(status.Active, 10),
		"capacity":     strconv.Itoa(status.Capacity),
		"pollSeconds":  "4",
	})
}

func (a *App) getHotelPresenceStatusAPI(w http.ResponseWriter, r *http.Request) error {
	hotelID := chi.URLParam(r, "id")

	if !a.Env.PresenceEnabled {
		a.writeJSON(w, http.StatusOK, map[string]any{
			"active":    0,
			"capacity":  a.Env.PresenceCapacity,
			"can_enter": true,
		})
		return nil
	}

	rateKey := "status:" + hotelID + ":" + presenceClientKey(r)
	if !globalPresenceRequestLimiter.Allow(rateKey, a.presenceMinInterval()) {
		a.writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error":   "rate_limited",
			"message": "Too many status requests. Please wait a moment.",
		})
		return nil
	}

	status, err := a.Store.GetHotelPresenceStatus(r.Context(), hotelID, a.Env.PresenceCapacity)
	if err != nil {
		if errors.Is(err, models.ErrInvalidPresencePayload) {
			a.writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "validation_error",
				"message": "Invalid hotel id",
			})
			return nil
		}
		return err
	}

	a.writeJSON(w, http.StatusOK, map[string]any{
		"active":    status.Active,
		"capacity":  status.Capacity,
		"can_enter": status.CanEnter,
	})
	return nil
}

func (a *App) heartbeatHotelPresenceAPI(w http.ResponseWriter, r *http.Request) error {
	if !a.Env.PresenceEnabled {
		a.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "disabled": true})
		return nil
	}

	hotelID := chi.URLParam(r, "id")
	token, ok := readPresenceToken(r)
	if !ok {
		a.writeJSON(w, http.StatusOK, map[string]any{
			"ok":     false,
			"reason": "no_slot",
		})
		return nil
	}

	rateKey := "heartbeat:" + hotelID + ":" + token
	if !globalPresenceRequestLimiter.Allow(rateKey, a.presenceMinInterval()) {
		a.writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"ok":      false,
			"reason":  "rate_limited",
			"message": "Too many heartbeat requests",
		})
		return nil
	}

	currentUser := session.CurrentUser(r)
	userID := ""
	if currentUser != nil {
		userID = currentUser.ID
	}

	alive, err := a.Store.HeartbeatHotelPresence(r.Context(), hotelID, token, userID, a.presenceTTL())
	if err != nil {
		if errors.Is(err, models.ErrInvalidPresencePayload) {
			a.writeJSON(w, http.StatusBadRequest, map[string]any{
				"ok":      false,
				"reason":  "validation_error",
				"message": "Invalid hotel id",
			})
			return nil
		}
		return err
	}

	if !alive {
		a.writeJSON(w, http.StatusOK, map[string]any{
			"ok":     false,
			"reason": "no_slot",
		})
		return nil
	}

	a.writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

func (a *App) acquireHotelPresenceForPage(w http.ResponseWriter, r *http.Request, hotelID string) (bool, error) {
	if !a.Env.PresenceEnabled {
		return true, nil
	}

	token, err := a.ensurePresenceToken(w, r)
	if err != nil {
		return false, err
	}

	currentUser := session.CurrentUser(r)
	userID := ""
	if currentUser != nil {
		userID = currentUser.ID
	}

	acquired, _, err := a.Store.AcquireHotelPresence(
		r.Context(),
		hotelID,
		token,
		userID,
		a.presenceTTL(),
		a.Env.PresenceCapacity,
	)
	if err != nil {
		return false, err
	}
	if !acquired {
		http.Redirect(w, r, "/hotel-wait?hotelId="+url.QueryEscape(hotelID), http.StatusFound)
		return false, nil
	}

	return true, nil
}

func (a *App) ensurePresenceToken(w http.ResponseWriter, r *http.Request) (string, error) {
	if token, ok := readPresenceToken(r); ok {
		return token, nil
	}

	token, err := generatePresenceToken()
	if err != nil {
		return "", err
	}

	maxAgeSeconds := int(a.presenceTTL().Seconds()) * 20
	if maxAgeSeconds < 300 {
		maxAgeSeconds = 300
	}
	if maxAgeSeconds > 86400 {
		maxAgeSeconds = 86400
	}

	http.SetCookie(w, &http.Cookie{
		Name:     presenceCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.Env.IsProduction,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAgeSeconds,
		Expires:  time.Now().UTC().Add(time.Duration(maxAgeSeconds) * time.Second),
	})

	return token, nil
}

func readPresenceToken(r *http.Request) (string, bool) {
	if r == nil {
		return "", false
	}
	cookie, err := r.Cookie(presenceCookieName)
	if err != nil {
		return "", false
	}

	token := strings.ToLower(strings.TrimSpace(cookie.Value))
	if !presenceTokenPattern.MatchString(token) {
		return "", false
	}

	return token, true
}

func generatePresenceToken() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}

	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x", buffer[0:4], buffer[4:6], buffer[6:8], buffer[8:10], buffer[10:16]), nil
}

func (a *App) presenceTTL() time.Duration {
	return time.Duration(a.Env.PresenceTTLSeconds) * time.Second
}

func (a *App) presenceMinInterval() time.Duration {
	return time.Duration(a.Env.PresenceMinIntervalSeconds) * time.Second
}

func presenceClientKey(r *http.Request) string {
	if r == nil {
		return "unknown"
	}

	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 {
			value := strings.TrimSpace(parts[0])
			if value != "" {
				return value
			}
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-Ip")); realIP != "" {
		return realIP
	}

	remoteAddr := strings.TrimSpace(r.RemoteAddr)
	if remoteAddr == "" {
		return "unknown"
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	if strings.TrimSpace(host) == "" {
		return remoteAddr
	}
	return host
}
