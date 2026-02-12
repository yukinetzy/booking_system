package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"easybook/internal/models"
	"easybook/internal/session"
	"easybook/internal/utils"
	"easybook/internal/view"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func (a *App) renderNotificationsPage(w http.ResponseWriter, r *http.Request) error {
	return a.renderHTML(w, http.StatusOK, "notifications.html", map[string]any{
		"authControls": view.Safe(renderAuthControls(session.CurrentUser(r), "/notifications")),
	})
}

func (a *App) subscribeNotificationsAPI(w http.ResponseWriter, r *http.Request) error {
	user := session.CurrentUser(r)
	if user == nil {
		a.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Authentication required"})
		return nil
	}

	payload, err := a.parsePayload(r)
	if err != nil {
		return err
	}

	roomID := firstNonEmpty(
		utils.ToTrimmedString(payload["room_id"]),
		utils.ToTrimmedString(payload["roomId"]),
		utils.ToTrimmedString(payload["hotelId"]),
	)
	checkIn := firstNonEmpty(
		utils.ToTrimmedString(payload["check_in"]),
		utils.ToTrimmedString(payload["checkIn"]),
	)
	checkOut := firstNonEmpty(
		utils.ToTrimmedString(payload["check_out"]),
		utils.ToTrimmedString(payload["checkOut"]),
	)

	waitlistType := strings.ToLower(
		utils.ToTrimmedString(payload["type"]),
	)
	if waitlistType == "" {
		waitlistType = models.WaitlistMain
	}

	if waitlistType != models.WaitlistMain && waitlistType != models.WaitlistPriority {
		a.writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid_waitlist_type",
		})
		return nil
	}

	waitlistID, subscribeErr := a.Store.SubscribeToWaitlist(
		r.Context(),
		user.ID,
		roomID,
		checkIn,
		checkOut,
		waitlistType,
	)

	if subscribeErr != nil {
		if errors.Is(subscribeErr, models.ErrDuplicateWaitlist) {
			a.writeJSON(w, http.StatusConflict, map[string]string{
				"error": "duplicate_subscription",
			})
			return nil
		}
		if errors.Is(subscribeErr, models.ErrPriorityAlreadyTaken) {
			a.writeJSON(w, http.StatusConflict, map[string]string{
				"error": "priority_taken",
			})
			return nil
		}
		if errors.Is(subscribeErr, models.ErrInvalidWaitlistPayload) {
			a.writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "validation_error",
			})
			return nil
		}
		return subscribeErr
	}

	a.writeJSON(w, http.StatusCreated, map[string]string{
		"id":      waitlistID,
		"message": "Subscription created",
	})
	return nil
}

func (a *App) getNotificationsAPI(w http.ResponseWriter, r *http.Request) error {
	user := session.CurrentUser(r)
	if user == nil {
		a.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Authentication required"})
		return nil
	}

	limit := int64(50)
	if queryLimit := strings.TrimSpace(r.URL.Query().Get("limit")); queryLimit != "" {
		if parsed, err := strconv.ParseInt(queryLimit, 10, 64); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	items, unreadCount, err := a.Store.ListNotifications(r.Context(), user.ID, limit)
	if err != nil {
		return err
	}

	responseItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		createdAt := ""
		switch typed := item["createdAt"].(type) {
		case time.Time:
			createdAt = typed.UTC().Format(time.RFC3339)
		case primitive.DateTime:
			createdAt = typed.Time().UTC().Format(time.RFC3339)
		default:
			createdAt = strings.TrimSpace(fmt.Sprint(item["createdAt"]))
		}

		isRead := false
		if value, ok := item["isRead"].(bool); ok {
			isRead = value
		}

		responseItems = append(responseItems, map[string]any{
			"id":        objectIDHex(item["_id"]),
			"title":     strings.TrimSpace(utils.ToTrimmedString(item["title"])),
			"text":      strings.TrimSpace(utils.ToTrimmedString(item["text"])),
			"link":      strings.TrimSpace(utils.ToTrimmedString(item["link"])),
			"createdAt": createdAt,
			"isRead":    isRead,
		})
	}

	a.writeJSON(w, http.StatusOK, map[string]any{
		"items":       responseItems,
		"unreadCount": unreadCount,
	})
	return nil
}

func (a *App) markNotificationReadAPI(w http.ResponseWriter, r *http.Request) error {
	user := session.CurrentUser(r)
	if user == nil {
		a.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Authentication required"})
		return nil
	}

	id := chi.URLParam(r, "id")
	_, err := a.Store.MarkNotificationRead(r.Context(), user.ID, id)
	if err != nil {
		if errors.Is(err, models.ErrNotificationNotFound) {
			a.writeJSON(w, http.StatusNotFound, map[string]string{
				"error":   "not_found",
				"message": "Notification not found",
			})
			return nil
		}
		return err
	}

	a.writeJSON(w, http.StatusOK, map[string]string{"message": "Notification marked as read"})
	return nil
}

func (a *App) markAllNotificationsReadAPI(w http.ResponseWriter, r *http.Request) error {
	user := session.CurrentUser(r)
	if user == nil {
		a.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Authentication required"})
		return nil
	}

	updated, err := a.Store.MarkAllNotificationsRead(r.Context(), user.ID)
	if err != nil {
		return err
	}

	a.writeJSON(w, http.StatusOK, map[string]any{
		"message": "All notifications marked as read",
		"updated": updated,
	})
	return nil
}
