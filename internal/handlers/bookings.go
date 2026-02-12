package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"easybook/internal/middleware"
	"easybook/internal/models"
	"easybook/internal/session"
	"easybook/internal/utils"
	"easybook/internal/view"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func sendBookingNotFoundPage(a *App, w http.ResponseWriter, r *http.Request, statusCode int) error {
	return a.sendStaticPage(w, r, "404.html", statusCode)
}

func canManageBooking(r *http.Request, booking map[string]any) bool {
	ownerID := objectIDHex(booking["userId"])
	return middleware.CanAccessOwnerResource(r, ownerID)
}

func (a *App) getHotelOptionsHTML(ctx context.Context, selectedHotelID string) (string, error) {
	hotels, _, err := a.Store.FindHotels(ctx, bson.M{}, bson.D{{Key: "title", Value: 1}}, nil, 0, 300)
	if err != nil {
		return "", err
	}

	parts := make([]string, 0, len(hotels))
	for _, hotel := range hotels {
		hotelID := objectIDHex(hotel["_id"])
		selected := ""
		if hotelID == selectedHotelID {
			selected = "selected"
		}
		parts = append(parts, fmt.Sprintf(`
    <option value="%s" %s>
      %s (%s)
    </option>
  `, hotelID, selected, view.EscapeHTML(stringValue(hotel, "title")), view.EscapeHTML(stringValue(hotel, "location"))))
	}

	return strings.Join(parts, ""), nil
}

func (a *App) renderBookingsPage(w http.ResponseWriter, r *http.Request) error {
	query := r.URL.Query()
	scope := "mine"
	if query.Get("scope") == "all" {
		scope = "all"
	}

	user := session.CurrentUser(r)
	includeAll := user != nil && user.Role == "admin" && scope == "all"

	pagination := utils.GetPagination(query.Get("page"), query.Get("limit"), a.Env.BookingsPageSize, a.Env.BookingsPageMax)
	filter := models.BuildBookingFilterFromQuery(query, user, includeAll)
	items, total, err := a.Store.ListBookingsWithDetails(r.Context(), filter, pagination.Skip, int64(pagination.Limit))
	if err != nil {
		return err
	}

	meta := utils.GetPaginationMeta(total, pagination.Page, pagination.Limit)

	bookingsHTML := `<div class="feature-card"><h3>No bookings found</h3></div>`
	if len(items) > 0 {
		parts := make([]string, 0, len(items))
		for _, booking := range items {
			bookingID := objectIDHex(booking["_id"])
			canManage := canManageBooking(r, booking)
			actions := []string{fmt.Sprintf(`<a class="btn" href="/bookings/%s">View</a>`, bookingID)}
			if canManage {
				actions = append(actions, fmt.Sprintf(`<a class="btn btn-outline" href="/bookings/%s/edit">Edit</a>`, bookingID))
				actions = append(actions, fmt.Sprintf(`
          <form method="POST" action="/bookings/%s/delete" style="display:inline;">
            <button class="btn btn-outline" type="submit" onclick="return confirm('Delete this booking?')">Delete</button>
          </form>
        `, bookingID))
			}

			parts = append(parts, fmt.Sprintf(`
        <div class="feature-card" style="text-align:left;">
          <h3>%s</h3>
          <p>
            <strong>Location:</strong> %s<br/>
            <strong>User:</strong> %s<br/>
            <strong>Dates:</strong> %s to %s<br/>
            <strong>Guests:</strong> %s
          </p>
          <div style="display:flex; gap:10px; flex-wrap:wrap; margin-top: 12px;">
            %s
          </div>
        </div>
      `,
				view.EscapeHTML(stringValue(booking, "hotelTitle")),
				view.EscapeHTML(stringValue(booking, "hotelLocation")),
				view.EscapeHTML(stringValue(booking, "userEmail")),
				view.EscapeHTML(stringValue(booking, "checkIn")),
				view.EscapeHTML(stringValue(booking, "checkOut")),
				view.EscapeHTML(formatInt(intValue(booking, "guests"))),
				strings.Join(actions, ""),
			))
		}
		bookingsHTML = strings.Join(parts, "")
	}

	paginationBar := renderPaginationBar(meta, "/bookings", map[string]string{
		"scope": scope,
		"limit": strconv.Itoa(pagination.Limit),
	})

	scopeOptions := `<option value="mine" selected>My bookings</option>`
	roleNote := `<span class="chip">Manage your reservations in one place.</span>`
	if user != nil && user.Role == "admin" {
		scopeOptions = fmt.Sprintf(`
      <option value="mine" %s>My bookings</option>
      <option value="all" %s>All users bookings</option>
    `,
			map[bool]string{true: "selected", false: ""}[scope == "mine"],
			map[bool]string{true: "selected", false: ""}[scope == "all"],
		)
		roleNote = `<span class="chip">Extended access is enabled for this account.</span>`
	}

	return a.renderHTML(w, http.StatusOK, "bookings.html", map[string]any{
		"authControls":  view.Safe(renderAuthControls(user, "/bookings")),
		"roleNote":      view.Safe(roleNote),
		"scopeOptions":  view.Safe(scopeOptions),
		"bookings":      view.Safe(bookingsHTML),
		"paginationBar": view.Safe(paginationBar),
	})
}

func (a *App) renderNewBookingPage(w http.ResponseWriter, r *http.Request) error {
	hotelID := r.URL.Query().Get("hotelId")
	hotelOptions, err := a.getHotelOptionsHTML(r.Context(), hotelID)
	if err != nil {
		return err
	}

	return a.renderHTML(w, http.StatusOK, "bookings-new.html", map[string]any{
		"authControls": view.Safe(renderAuthControls(session.CurrentUser(r), "/bookings/new")),
		"errorMessage": "",
		"hotelOptions": view.Safe(hotelOptions),
		"checkIn":      strings.TrimSpace(r.URL.Query().Get("checkIn")),
		"checkOut":     strings.TrimSpace(r.URL.Query().Get("checkOut")),
		"guests":       "1",
		"notes":        "",
		"groupId":      strings.TrimSpace(r.URL.Query().Get("groupId")),
		"todayDate":    todayISODate(),
	})
}

func (a *App) createBookingFromPage(w http.ResponseWriter, r *http.Request) error {
	payload, err := a.parsePayload(r)
	if err != nil {
		return err
	}

	validationErrors, booking := utils.ValidateBookingPayload(payload, false)
	selectedRoomID := firstNonEmpty(
		utils.ToTrimmedString(payload["roomId"]),
		utils.ToTrimmedString(payload["room_id"]),
		utils.ToTrimmedString(payload["hotelId"]),
	)
	hotelOptions, optionsErr := a.getHotelOptionsHTML(r.Context(), selectedRoomID)
	if optionsErr != nil {
		return optionsErr
	}

	if len(validationErrors) > 0 {
		return a.renderHTML(w, http.StatusBadRequest, "bookings-new.html", map[string]any{
			"authControls": view.Safe(renderAuthControls(session.CurrentUser(r), "/bookings/new")),
			"errorMessage": validationErrors[0],
			"hotelOptions": view.Safe(hotelOptions),
			"checkIn":      utils.ToTrimmedString(payload["checkIn"]),
			"checkOut":     utils.ToTrimmedString(payload["checkOut"]),
			"guests":       defaultIfEmpty(utils.ToTrimmedString(payload["guests"]), "1"),
			"notes":        utils.ToTrimmedString(payload["notes"]),
			"todayDate":    todayISODate(),
		})
	}

	roomIDHex := roomIDFromBookingData(booking)
	hotel, err := a.Store.FindHotelByID(r.Context(), roomIDHex, nil)
	if err != nil {
		return err
	}
	if hotel == nil {
		return a.renderHTML(w, http.StatusBadRequest, "bookings-new.html", map[string]any{
			"authControls": view.Safe(renderAuthControls(session.CurrentUser(r), "/bookings/new")),
			"errorMessage": "Selected room does not exist",
			"hotelOptions": view.Safe(hotelOptions),
			"checkIn":      utils.ToTrimmedString(payload["checkIn"]),
			"checkOut":     utils.ToTrimmedString(payload["checkOut"]),
			"guests":       defaultIfEmpty(utils.ToTrimmedString(payload["guests"]), "1"),
			"notes":        utils.ToTrimmedString(payload["notes"]),
			"todayDate":    todayISODate(),
		})
	}

	user := session.CurrentUser(r)
	insertedID, err := a.Store.CreateBooking(r.Context(), booking, user.ID)
	if err != nil {
		if errors.Is(err, models.ErrBookingConflict) {
			return a.renderHTML(w, http.StatusConflict, "bookings-new.html", map[string]any{
				"authControls": view.Safe(renderAuthControls(session.CurrentUser(r), "/bookings/new")),
				"errorMessage": "Selected room is occupied for these dates. Choose different dates or subscribe for notifications.",
				"hotelOptions": view.Safe(hotelOptions),
				"checkIn":      utils.ToTrimmedString(payload["checkIn"]),
				"checkOut":     utils.ToTrimmedString(payload["checkOut"]),
				"guests":       defaultIfEmpty(utils.ToTrimmedString(payload["guests"]), "1"),
				"notes":        utils.ToTrimmedString(payload["notes"]),
				"todayDate":    todayISODate(),
			})
		}
		if errors.Is(err, models.ErrInvalidBookingPayload) {
			return a.renderHTML(w, http.StatusBadRequest, "bookings-new.html", map[string]any{
				"authControls": view.Safe(renderAuthControls(session.CurrentUser(r), "/bookings/new")),
				"errorMessage": "Invalid booking data. Check dates and room.",
				"hotelOptions": view.Safe(hotelOptions),
				"checkIn":      utils.ToTrimmedString(payload["checkIn"]),
				"checkOut":     utils.ToTrimmedString(payload["checkOut"]),
				"guests":       defaultIfEmpty(utils.ToTrimmedString(payload["guests"]), "1"),
				"notes":        utils.ToTrimmedString(payload["notes"]),
				"todayDate":    todayISODate(),
			})
		}
		return err
	}

	http.Redirect(w, r, "/bookings/"+insertedID, http.StatusFound)
	return nil
}

func (a *App) renderBookingDetailsPage(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		return sendBookingNotFoundPage(a, w, r, http.StatusBadRequest)
	}

	booking, err := a.Store.FindBookingByIDWithDetails(r.Context(), id)
	if err != nil {
		return err
	}
	if booking == nil {
		return sendBookingNotFoundPage(a, w, r, http.StatusNotFound)
	}

	if !canManageBooking(r, booking) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return nil
	}

	bookingID := objectIDHex(booking["_id"])
	actionButtons := fmt.Sprintf(`
    <a class="btn btn-outline" href="/bookings/%s/edit">Edit</a>
    <form method="POST" action="/bookings/%s/delete" style="display:inline;">
      <button class="btn btn-outline" type="submit" onclick="return confirm('Delete this booking?')">Delete</button>
    </form>
  `, bookingID, bookingID)

	return a.renderHTML(w, http.StatusOK, "bookings-item.html", map[string]any{
		"authControls":  view.Safe(renderAuthControls(session.CurrentUser(r), "/bookings/"+bookingID)),
		"id":            bookingID,
		"hotelTitle":    defaultIfEmpty(stringValue(booking, "hotelTitle"), "Unknown hotel"),
		"hotelLocation": defaultIfEmpty(stringValue(booking, "hotelLocation"), "-"),
		"userEmail":     defaultIfEmpty(stringValue(booking, "userEmail"), "-"),
		"checkIn":       stringValue(booking, "checkIn"),
		"checkOut":      stringValue(booking, "checkOut"),
		"guests":        formatInt(intValue(booking, "guests")),
		"notes":         defaultIfEmpty(stringValue(booking, "notes"), "-"),
		"actionButtons": view.Safe(actionButtons),
	})
}

func (a *App) renderEditBookingPage(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		return sendBookingNotFoundPage(a, w, r, http.StatusBadRequest)
	}

	booking, err := a.Store.FindBookingByIDWithDetails(r.Context(), id)
	if err != nil {
		return err
	}
	if booking == nil {
		return sendBookingNotFoundPage(a, w, r, http.StatusNotFound)
	}

	if !canManageBooking(r, booking) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return nil
	}

	hotelOptions, err := a.getHotelOptionsHTML(r.Context(), roomIDFromBookingData(booking))
	if err != nil {
		return err
	}

	bookingID := objectIDHex(booking["_id"])
	return a.renderHTML(w, http.StatusOK, "bookings-edit.html", map[string]any{
		"authControls": view.Safe(renderAuthControls(session.CurrentUser(r), "/bookings/"+bookingID+"/edit")),
		"id":           bookingID,
		"errorMessage": "",
		"hotelOptions": view.Safe(hotelOptions),
		"checkIn":      stringValue(booking, "checkIn"),
		"checkOut":     stringValue(booking, "checkOut"),
		"guests":       formatInt(intValue(booking, "guests")),
		"notes":        stringValue(booking, "notes"),
		"todayDate":    todayISODate(),
	})
}

func (a *App) updateBookingFromPage(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		return sendBookingNotFoundPage(a, w, r, http.StatusBadRequest)
	}

	existing, err := a.Store.FindBookingByIDWithDetails(r.Context(), id)
	if err != nil {
		return err
	}
	if existing == nil {
		return sendBookingNotFoundPage(a, w, r, http.StatusNotFound)
	}
	if !canManageBooking(r, existing) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return nil
	}

	payload, err := a.parsePayload(r)
	if err != nil {
		return err
	}

	validationErrors, booking := utils.ValidateBookingPayload(payload, false)
	selectedRoomID := firstNonEmpty(
		utils.ToTrimmedString(payload["roomId"]),
		utils.ToTrimmedString(payload["room_id"]),
		utils.ToTrimmedString(payload["hotelId"]),
	)
	if selectedRoomID == "" {
		selectedRoomID = roomIDFromBookingData(existing)
	}
	hotelOptions, optionsErr := a.getHotelOptionsHTML(r.Context(), selectedRoomID)
	if optionsErr != nil {
		return optionsErr
	}

	if len(validationErrors) > 0 {
		return a.renderHTML(w, http.StatusBadRequest, "bookings-edit.html", map[string]any{
			"authControls": view.Safe(renderAuthControls(session.CurrentUser(r), "/bookings/"+id+"/edit")),
			"id":           id,
			"errorMessage": validationErrors[0],
			"hotelOptions": view.Safe(hotelOptions),
			"checkIn":      utils.ToTrimmedString(payload["checkIn"]),
			"checkOut":     utils.ToTrimmedString(payload["checkOut"]),
			"guests":       defaultIfEmpty(utils.ToTrimmedString(payload["guests"]), "1"),
			"notes":        utils.ToTrimmedString(payload["notes"]),
			"todayDate":    todayISODate(),
		})
	}

	roomIDHex := roomIDFromBookingData(booking)
	hotel, err := a.Store.FindHotelByID(r.Context(), roomIDHex, nil)
	if err != nil {
		return err
	}
	if hotel == nil {
		return a.renderHTML(w, http.StatusBadRequest, "bookings-edit.html", map[string]any{
			"authControls": view.Safe(renderAuthControls(session.CurrentUser(r), "/bookings/"+id+"/edit")),
			"id":           id,
			"errorMessage": "Selected room does not exist",
			"hotelOptions": view.Safe(hotelOptions),
			"checkIn":      utils.ToTrimmedString(payload["checkIn"]),
			"checkOut":     utils.ToTrimmedString(payload["checkOut"]),
			"guests":       defaultIfEmpty(utils.ToTrimmedString(payload["guests"]), "1"),
			"notes":        utils.ToTrimmedString(payload["notes"]),
			"todayDate":    todayISODate(),
		})
	}

	matched, err := a.Store.UpdateBookingByID(r.Context(), id, booking)
	if err != nil {
		if errors.Is(err, models.ErrBookingConflict) {
			return a.renderHTML(w, http.StatusConflict, "bookings-edit.html", map[string]any{
				"authControls": view.Safe(renderAuthControls(session.CurrentUser(r), "/bookings/"+id+"/edit")),
				"id":           id,
				"errorMessage": "Selected room is occupied for these dates. Choose different dates or subscribe for notifications.",
				"hotelOptions": view.Safe(hotelOptions),
				"checkIn":      utils.ToTrimmedString(payload["checkIn"]),
				"checkOut":     utils.ToTrimmedString(payload["checkOut"]),
				"guests":       defaultIfEmpty(utils.ToTrimmedString(payload["guests"]), "1"),
				"notes":        utils.ToTrimmedString(payload["notes"]),
				"todayDate":    todayISODate(),
			})
		}
		if errors.Is(err, models.ErrInvalidBookingPayload) {
			return a.renderHTML(w, http.StatusBadRequest, "bookings-edit.html", map[string]any{
				"authControls": view.Safe(renderAuthControls(session.CurrentUser(r), "/bookings/"+id+"/edit")),
				"id":           id,
				"errorMessage": "Invalid booking data. Check dates and room.",
				"hotelOptions": view.Safe(hotelOptions),
				"checkIn":      utils.ToTrimmedString(payload["checkIn"]),
				"checkOut":     utils.ToTrimmedString(payload["checkOut"]),
				"guests":       defaultIfEmpty(utils.ToTrimmedString(payload["guests"]), "1"),
				"notes":        utils.ToTrimmedString(payload["notes"]),
				"todayDate":    todayISODate(),
			})
		}
		return err
	}
	if matched == 0 {
		return sendBookingNotFoundPage(a, w, r, http.StatusNotFound)
	}

	a.triggerWaitlistProcessing(r.Context(), roomIDFromBookingData(existing), roomIDHex)

	http.Redirect(w, r, "/bookings/"+id, http.StatusFound)
	return nil
}

func (a *App) deleteBookingFromPage(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		return sendBookingNotFoundPage(a, w, r, http.StatusBadRequest)
	}

	existing, err := a.Store.FindBookingByIDWithDetails(r.Context(), id)
	if err != nil {
		return err
	}
	if existing == nil {
		return sendBookingNotFoundPage(a, w, r, http.StatusNotFound)
	}
	if !canManageBooking(r, existing) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return nil
	}

	deletedCount, err := a.Store.DeleteBookingByID(r.Context(), id)
	if err != nil {
		return err
	}
	if deletedCount == 0 {
		return sendBookingNotFoundPage(a, w, r, http.StatusNotFound)
	}

	a.triggerWaitlistProcessing(r.Context(), roomIDFromBookingData(existing))

	http.Redirect(w, r, "/bookings", http.StatusFound)
	return nil
}

func (a *App) getBookingsAPI(w http.ResponseWriter, r *http.Request) error {
	query := r.URL.Query()
	scope := "mine"
	if query.Get("scope") == "all" {
		scope = "all"
	}

	user := session.CurrentUser(r)
	includeAll := user != nil && user.Role == "admin" && scope == "all"
	pagination := utils.GetPagination(query.Get("page"), query.Get("limit"), a.Env.BookingsPageSize, a.Env.BookingsPageMax)
	filter := models.BuildBookingFilterFromQuery(query, user, includeAll)

	items, total, err := a.Store.ListBookingsWithDetails(r.Context(), filter, pagination.Skip, int64(pagination.Limit))
	if err != nil {
		return err
	}

	meta := utils.GetPaginationMeta(total, pagination.Page, pagination.Limit)
	a.writeJSON(w, http.StatusOK, map[string]any{"items": items, "meta": meta})
	return nil
}

func (a *App) getBookingByIDAPI(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		a.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid ID"})
		return nil
	}

	booking, err := a.Store.FindBookingByIDWithDetails(r.Context(), id)
	if err != nil {
		return err
	}
	if booking == nil {
		a.writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return nil
	}
	if !canManageBooking(r, booking) {
		a.writeJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
		return nil
	}

	a.writeJSON(w, http.StatusOK, booking)
	return nil
}

func (a *App) createBookingAPI(w http.ResponseWriter, r *http.Request) error {
	payload, err := a.parsePayload(r)
	if err != nil {
		return err
	}

	validationErrors, booking := utils.ValidateBookingPayload(payload, false)
	if len(validationErrors) > 0 {
		a.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "validation_error", "message": validationErrors[0]})
		return nil
	}

	roomIDHex := roomIDFromBookingData(booking)
	hotel, err := a.Store.FindHotelByID(r.Context(), roomIDHex, nil)
	if err != nil {
		return err
	}
	if hotel == nil {
		a.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "validation_error", "message": "Selected room does not exist"})
		return nil
	}

	user := session.CurrentUser(r)
	insertedID, err := a.Store.CreateBooking(r.Context(), booking, user.ID)
	if err != nil {
		if errors.Is(err, models.ErrBookingConflict) {
			a.writeJSON(w, http.StatusConflict, map[string]string{
				"error":   "booking_conflict",
				"message": "Room is already booked for selected dates",
			})
			return nil
		}
		if errors.Is(err, models.ErrInvalidBookingPayload) {
			a.writeJSON(w, http.StatusBadRequest, map[string]string{
				"error":   "validation_error",
				"message": err.Error(),
			})
			return nil
		}
		return err
	}

	a.writeJSON(w, http.StatusCreated, map[string]string{"_id": insertedID})
	return nil
}

func (a *App) updateBookingAPI(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		a.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid ID"})
		return nil
	}

	existing, err := a.Store.FindBookingByIDWithDetails(r.Context(), id)
	if err != nil {
		return err
	}
	if existing == nil {
		a.writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return nil
	}
	if !canManageBooking(r, existing) {
		a.writeJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
		return nil
	}

	payload, err := a.parsePayload(r)
	if err != nil {
		return err
	}

	validationErrors, booking := utils.ValidateBookingPayload(payload, true)
	if len(validationErrors) > 0 {
		a.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "validation_error", "message": validationErrors[0]})
		return nil
	}

	if roomRaw, hasRoom := booking["roomId"]; hasRoom {
		roomIDHex := objectIDHex(roomRaw)
		hotel, findErr := a.Store.FindHotelByID(r.Context(), roomIDHex, nil)
		if findErr != nil {
			return findErr
		}
		if hotel == nil {
			a.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "validation_error", "message": "Selected room does not exist"})
			return nil
		}
	}

	matched, err := a.Store.UpdateBookingByID(r.Context(), id, booking)
	if err != nil {
		if errors.Is(err, models.ErrBookingConflict) {
			a.writeJSON(w, http.StatusConflict, map[string]string{
				"error":   "booking_conflict",
				"message": "Room is already booked for selected dates",
			})
			return nil
		}
		if errors.Is(err, models.ErrInvalidBookingPayload) {
			a.writeJSON(w, http.StatusBadRequest, map[string]string{
				"error":   "validation_error",
				"message": err.Error(),
			})
			return nil
		}
		return err
	}
	if matched == 0 {
		a.writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return nil
	}

	updatedRoomID := roomIDFromBookingData(booking)
	if updatedRoomID == "" {
		updatedRoomID = roomIDFromBookingData(existing)
	}
	a.triggerWaitlistProcessing(r.Context(), roomIDFromBookingData(existing), updatedRoomID)

	a.writeJSON(w, http.StatusOK, map[string]string{"message": "Updated"})
	return nil
}

func (a *App) deleteBookingAPI(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		a.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid ID"})
		return nil
	}

	existing, err := a.Store.FindBookingByIDWithDetails(r.Context(), id)
	if err != nil {
		return err
	}
	if existing == nil {
		a.writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return nil
	}
	if !canManageBooking(r, existing) {
		a.writeJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
		return nil
	}

	deletedCount, err := a.Store.DeleteBookingByID(r.Context(), id)
	if err != nil {
		return err
	}
	if deletedCount == 0 {
		a.writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return nil
	}

	a.triggerWaitlistProcessing(r.Context(), roomIDFromBookingData(existing))

	a.writeJSON(w, http.StatusOK, map[string]string{"message": "Deleted"})
	return nil
}

func (a *App) getBookingAvailabilityAPI(w http.ResponseWriter, r *http.Request) error {
	query := r.URL.Query()
	roomID := firstNonEmpty(
		strings.TrimSpace(query.Get("room_id")),
		strings.TrimSpace(query.Get("roomId")),
		strings.TrimSpace(query.Get("hotelId")),
	)
	checkIn := firstNonEmpty(strings.TrimSpace(query.Get("check_in")), strings.TrimSpace(query.Get("checkIn")))
	checkOut := firstNonEmpty(strings.TrimSpace(query.Get("check_out")), strings.TrimSpace(query.Get("checkOut")))
	excludeBookingID := firstNonEmpty(strings.TrimSpace(query.Get("exclude_booking_id")), strings.TrimSpace(query.Get("excludeBookingId")))

	available, err := a.Store.CheckRoomAvailability(r.Context(), roomID, checkIn, checkOut, excludeBookingID)
	if err != nil {
		if errors.Is(err, models.ErrInvalidBookingPayload) {
			a.writeJSON(w, http.StatusBadRequest, map[string]string{
				"error":   "validation_error",
				"message": err.Error(),
			})
			return nil
		}
		return err
	}

	if !available {
		a.writeJSON(w, http.StatusOK, map[string]any{
			"available": false,
			"error":     "booking_conflict",
			"message":   "Room is occupied for selected dates",
		})
		return nil
	}

	a.writeJSON(w, http.StatusOK, map[string]any{
		"available": true,
	})
	return nil
}

func (a *App) triggerWaitlistProcessing(ctx context.Context, roomIDs ...string) {
	seen := map[string]struct{}{}
	for _, roomID := range roomIDs {
		roomID = strings.TrimSpace(roomID)
		if roomID == "" {
			continue
		}
		if _, exists := seen[roomID]; exists {
			continue
		}
		seen[roomID] = struct{}{}

		if _, err := a.Store.ProcessWaitlistForRoom(ctx, roomID); err != nil {
			log.Printf("waitlist processing failed for room %s: %v", roomID, err)
		}
	}
}

func roomIDFromBookingData(booking map[string]any) string {
	if booking == nil {
		return ""
	}
	return firstNonEmpty(
		objectIDHex(booking["roomId"]),
		objectIDHex(booking["hotelId"]),
	)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func todayISODate() string {
	return time.Now().In(time.Local).Format("2006-01-02")
}

func defaultIfEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (a *App) getFallbackBookingByGroupIDAPI(w http.ResponseWriter, r *http.Request) error {
	groupIDText := strings.TrimSpace(r.URL.Query().Get("group_id"))
	if groupIDText == "" {
		a.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "group_id required"})
		return nil
	}

	user := session.CurrentUser(r)
	if user == nil {
		a.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return nil
	}

	booking, err := a.Store.FindBookingByGroupIDAndUserID(r.Context(), groupIDText, user.ID)
	if err != nil {
		return err
	}
	if booking == nil {
		a.writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return nil
	}

	a.writeJSON(w, http.StatusOK, booking)
	return nil
}
