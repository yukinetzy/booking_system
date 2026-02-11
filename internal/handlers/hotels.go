package handlers

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"easybook/internal/models"
	"easybook/internal/session"
	"easybook/internal/utils"
	"easybook/internal/view"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func sendHotelNotFoundPage(a *App, w http.ResponseWriter, r *http.Request, statusCode int) error {
	return a.sendStaticPage(w, r, "404.html", statusCode)
}

func buildHotelImageURL(hotel map[string]any) string {
	customImage := strings.TrimSpace(stringValue(hotel, "imageUrl"))
	if matched, _ := regexp.MatchString(`^https?://\S+$`, customImage); matched {
		return customImage
	}

	seed := url.QueryEscape(fmt.Sprintf("%s-%s", stringValue(hotel, "title"), stringValue(hotel, "location")))
	if seed == "-" || seed == "" {
		seed = "hotel-city"
	}
	return "https://picsum.photos/seed/" + seed + "/1200/800"
}

func buildRatingStarsHTML(rating float64) string {
	safeRating := rating
	if math.IsNaN(safeRating) || math.IsInf(safeRating, 0) {
		safeRating = 0
	}
	activeCount := int(math.Round(safeRating))
	if activeCount < 0 {
		activeCount = 0
	}
	if activeCount > 5 {
		activeCount = 5
	}

	parts := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		if i < activeCount {
			parts = append(parts, `<span class="rating-star active">&#9733;</span>`)
		} else {
			parts = append(parts, `<span class="rating-star">&#9733;</span>`)
		}
	}
	return strings.Join(parts, "")
}

func buildRatingVotesText(hotel map[string]any) string {
	votes := intValue(hotel, "ratingVotes")
	if votes <= 0 {
		return "No votes yet"
	}
	suffix := "s"
	if votes == 1 {
		suffix = ""
	}
	return fmt.Sprintf("%d vote%s", votes, suffix)
}

func buildGuestBookButton(hotelID string) string {
	nextPath := "/bookings/new?hotelId=" + hotelID
	loginURL := "/login?next=" + url.QueryEscape(nextPath)
	return fmt.Sprintf(`
    <a
      class="btn btn-outline guest-book-btn"
      data-login-url="%s"
      href="%s"
    >Book</a>
  `, view.EscapeHTML(loginURL), view.EscapeHTML(loginURL))
}

func buildGuestRateButton(hotelID string) string {
	nextPath := "/hotels/" + hotelID
	loginURL := "/login?next=" + url.QueryEscape(nextPath)
	return fmt.Sprintf(`
    <a
      class="btn btn-outline guest-rate-btn"
      data-login-url="%s"
      href="%s"
    >Rate</a>
  `, view.EscapeHTML(loginURL), view.EscapeHTML(loginURL))
}

func buildHotelCardHTML(r *http.Request, hotel map[string]any) string {
	user := session.CurrentUser(r)
	hotelID := objectIDHex(hotel["_id"])
	amenities := stringSliceValue(hotel, "amenities")
	amenitiesParts := make([]string, 0, len(amenities))
	for _, amenity := range amenities {
		amenitiesParts = append(amenitiesParts, `<span class="chip">`+view.EscapeHTML(amenity)+`</span>`)
	}
	amenitiesHTML := strings.Join(amenitiesParts, "")

	imageURL := buildHotelImageURL(hotel)
	rating := floatValue(hotel, "rating")
	ratingStars := buildRatingStarsHTML(rating)
	ratingVotesText := buildRatingVotesText(hotel)

	actions := []string{fmt.Sprintf(`<a class="btn" href="/hotels/%s">View</a>`, hotelID)}
	if user != nil {
		actions = append(actions, fmt.Sprintf(`<a class="btn btn-outline" href="/bookings/new?hotelId=%s">Book</a>`, hotelID))
	} else {
		actions = append(actions, buildGuestBookButton(hotelID))
	}

	if user != nil && user.Role == "admin" {
		actions = append(actions, fmt.Sprintf(`<a class="btn btn-outline" href="/hotels/%s/edit">Edit</a>`, hotelID))
		actions = append(actions, fmt.Sprintf(`
      <form method="POST" action="/hotels/%s/delete" style="display:inline;">
        <button class="btn btn-outline" type="submit" onclick="return confirm('Delete this hotel?')">Delete</button>
      </form>
    `, hotelID))
	}

	return fmt.Sprintf(`
    <article class="feature-card hotel-card">
      <img class="hotel-cover" src="%s" alt="%s" loading="lazy" />

      <div class="hotel-card-body">
        <h3>%s</h3>
        <p>%s</p>

        <p>
          <strong>City:</strong> %s<br/>
          <strong>Address:</strong> %s<br/>
          <strong>Price:</strong> %s KZT<br/>
          <strong>Available rooms:</strong> %s
        </p>

        <div class="rating-row" aria-label="Rating %s out of 5">
          <div class="rating-stars">%s</div>
          <span class="rating-value">%s/5</span>
          <span class="rating-count">%s</span>
        </div>

        <div class="chip-row">%s</div>

        <div class="hotel-card-actions">
          %s
        </div>
      </div>
    </article>
  `,
		view.EscapeHTML(imageURL),
		view.EscapeHTML(stringValue(hotel, "title")),
		view.EscapeHTML(stringValue(hotel, "title")),
		view.EscapeHTML(stringValue(hotel, "description")),
		view.EscapeHTML(stringValue(hotel, "location")),
		view.EscapeHTML(stringValue(hotel, "address")),
		view.EscapeHTML(formatNumber(floatValue(hotel, "price_per_night"))),
		view.EscapeHTML(formatInt(intValue(hotel, "available_rooms"))),
		view.EscapeHTML(formatNumber(rating)),
		ratingStars,
		view.EscapeHTML(formatNumber(rating)),
		view.EscapeHTML(ratingVotesText),
		amenitiesHTML,
		strings.Join(actions, ""),
	)
}

func (a *App) renderHotelsPage(w http.ResponseWriter, r *http.Request) error {
	query := r.URL.Query()
	q := query.Get("q")
	city := query.Get("city")
	minPrice := query.Get("minPrice")
	maxPrice := query.Get("maxPrice")
	minRating := query.Get("minRating")
	sortKey := query.Get("sort")
	fields := query.Get("fields")

	pagination := utils.GetPagination(query.Get("page"), query.Get("limit"), a.Env.HotelsPageSize, a.Env.HotelsPageMax)
	filter := models.BuildHotelFilterFromQuery(query)
	sortQuery := models.BuildHotelSortFromQuery(sortKey)
	projection := models.BuildHotelProjectionFromQuery(fields)

	hotels, total, err := a.Store.FindHotels(r.Context(), filter, sortQuery, projection, pagination.Skip, int64(pagination.Limit))
	if err != nil {
		return err
	}

	cities, err := a.Store.DistinctHotelCities(r.Context())
	if err != nil {
		return err
	}

	meta := utils.GetPaginationMeta(total, pagination.Page, pagination.Limit)

	results := `<div class="feature-card"><h3>No hotels found</h3></div>`
	if len(hotels) > 0 {
		parts := make([]string, 0, len(hotels))
		for _, hotel := range hotels {
			parts = append(parts, buildHotelCardHTML(r, hotel))
		}
		results = strings.Join(parts, "")
	}

	cityOptions := []string{`<option value="">All</option>`}
	for _, item := range cities {
		selected := ""
		if item == city {
			selected = "selected"
		}
		cityOptions = append(cityOptions, fmt.Sprintf(`<option value="%s" %s>%s</option>`, view.EscapeHTML(item), selected, view.EscapeHTML(item)))
	}

	sortOptions := []struct {
		Value string
		Label string
	}{
		{Value: "", Label: "Default (rating)"},
		{Value: "price_asc", Label: "Price ascending"},
		{Value: "price_desc", Label: "Price descending"},
		{Value: "rating_desc", Label: "Rating high to low"},
		{Value: "title_asc", Label: "Title A-Z"},
		{Value: "title_desc", Label: "Title Z-A"},
	}
	sortOptionsHTML := make([]string, 0, len(sortOptions))
	for _, item := range sortOptions {
		selected := ""
		if item.Value == sortKey {
			selected = "selected"
		}
		sortOptionsHTML = append(sortOptionsHTML, fmt.Sprintf(`<option value="%s" %s>%s</option>`, view.EscapeHTML(item.Value), selected, view.EscapeHTML(item.Label)))
	}

	ratingOptions := []struct {
		Value string
		Label string
	}{
		{Value: "", Label: "Any rating"},
		{Value: "4.5", Label: "4.5 and higher"},
		{Value: "4", Label: "4.0 and higher"},
		{Value: "3.5", Label: "3.5 and higher"},
		{Value: "3", Label: "3.0 and higher"},
	}
	ratingOptionsHTML := make([]string, 0, len(ratingOptions))
	for _, item := range ratingOptions {
		selected := ""
		if item.Value == minRating {
			selected = "selected"
		}
		ratingOptionsHTML = append(ratingOptionsHTML, fmt.Sprintf(`<option value="%s" %s>%s</option>`, view.EscapeHTML(item.Value), selected, view.EscapeHTML(item.Label)))
	}

	paginationBar := renderPaginationBar(meta, "/hotels", map[string]string{
		"q":         q,
		"city":      city,
		"minPrice":  minPrice,
		"maxPrice":  maxPrice,
		"minRating": minRating,
		"sort":      sortKey,
		"fields":    fields,
		"limit":     strconv.Itoa(pagination.Limit),
	})

	manageAction := ""
	if user := session.CurrentUser(r); user != nil && user.Role == "admin" {
		manageAction = `<a class="btn" href="/hotels/new">Add hotel</a>`
	}

	bookingNotice := ""
	if query.Get("loginRequired") == "1" {
		bookingNotice = `<div class="notice notice-warning">You need an account to book. Redirecting to login...</div>`
	}

	return a.renderHTML(w, http.StatusOK, "hotels.html", map[string]any{
		"q":             q,
		"cityOptions":   view.Safe(strings.Join(cityOptions, "")),
		"minPrice":      minPrice,
		"maxPrice":      maxPrice,
		"minRating":     minRating,
		"ratingOptions": view.Safe(strings.Join(ratingOptionsHTML, "")),
		"sortOptions":   view.Safe(strings.Join(sortOptionsHTML, "")),
		"manageAction":  view.Safe(manageAction),
		"authControls":  view.Safe(renderAuthControls(session.CurrentUser(r), "/hotels")),
		"paginationBar": view.Safe(paginationBar),
		"bookingNotice": view.Safe(bookingNotice),
		"results":       view.Safe(results),
	})
}

func (a *App) renderNewHotelPage(w http.ResponseWriter, r *http.Request) error {
	return a.renderHTML(w, http.StatusOK, "hotels-new.html", map[string]any{
		"authControls":    view.Safe(renderAuthControls(session.CurrentUser(r), "/hotels/new")),
		"errorMessage":    "",
		"title":           "",
		"description":     "",
		"location":        "",
		"address":         "",
		"price_per_night": "",
		"rating":          "",
		"available_rooms": "",
		"amenities":       "",
		"imageUrl":        "",
	})
}

func (a *App) createHotelFromPage(w http.ResponseWriter, r *http.Request) error {
	payload, err := a.parsePayload(r)
	if err != nil {
		return err
	}

	validationErrors, hotel := utils.ValidateHotelPayload(payload, false)
	if len(validationErrors) > 0 {
		return a.renderHTML(w, http.StatusBadRequest, "hotels-new.html", map[string]any{
			"authControls":    view.Safe(renderAuthControls(session.CurrentUser(r), "/hotels/new")),
			"errorMessage":    validationErrors[0],
			"title":           utils.ToTrimmedString(payload["title"]),
			"description":     utils.ToTrimmedString(payload["description"]),
			"location":        utils.ToTrimmedString(payload["location"]),
			"address":         utils.ToTrimmedString(payload["address"]),
			"price_per_night": utils.ToTrimmedString(payload["price_per_night"]),
			"rating":          utils.ToTrimmedString(payload["rating"]),
			"available_rooms": utils.ToTrimmedString(payload["available_rooms"]),
			"amenities":       utils.ToTrimmedString(payload["amenities"]),
			"imageUrl":        utils.ToTrimmedString(payload["imageUrl"]),
		})
	}

	user := session.CurrentUser(r)
	createdBy := ""
	if user != nil {
		createdBy = user.ID
	}

	insertedID, err := a.Store.CreateHotel(r.Context(), hotel, createdBy)
	if err != nil {
		return err
	}

	http.Redirect(w, r, "/hotels/"+insertedID, http.StatusFound)
	return nil
}

func (a *App) renderHotelDetailsPage(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		return sendHotelNotFoundPage(a, w, r, http.StatusBadRequest)
	}

	hotel, err := a.Store.FindHotelByID(r.Context(), id, nil)
	if err != nil {
		return err
	}
	if hotel == nil {
		return sendHotelNotFoundPage(a, w, r, http.StatusNotFound)
	}

	hotelID := objectIDHex(hotel["_id"])
	canRender, err := a.acquireHotelPresenceForPage(w, r, hotelID)
	if err != nil {
		if errors.Is(err, models.ErrInvalidPresencePayload) {
			return sendHotelNotFoundPage(a, w, r, http.StatusBadRequest)
		}
		return err
	}
	if !canRender {
		return nil
	}

	amenitiesText := strings.Join(stringSliceValue(hotel, "amenities"), ", ")
	imageURL := buildHotelImageURL(hotel)
	rating := floatValue(hotel, "rating")
	ratingStars := buildRatingStarsHTML(rating)
	ratingVotesText := buildRatingVotesText(hotel)

	manageButtons := ""
	user := session.CurrentUser(r)
	if user != nil && user.Role == "admin" {
		manageButtons = fmt.Sprintf(`
      <a href="/hotels/%s/edit" class="btn btn-outline">Edit</a>
      <form method="POST" action="/hotels/%s/delete" style="display:inline;">
        <button type="submit" class="btn btn-outline" onclick="return confirm('Delete this hotel?')">Delete</button>
      </form>
    `, hotelID, hotelID)
	}

	bookButton := ""
	if user != nil {
		bookButton = fmt.Sprintf(`<a href="/bookings/new?hotelId=%s" class="btn">Book Now</a>`, hotelID)
	} else {
		bookButton = buildGuestBookButton(hotelID)
	}

	ratingActions := ""
	if user != nil {
		ratingActions = fmt.Sprintf(`
      <form method="POST" action="/hotels/%s/rate" class="rating-form">
        <input type="hidden" name="next" value="/hotels/%s" />
        <label for="score">Rate this hotel</label>
        <div class="rating-form-row">
          <select id="score" name="score" required>
            <option value="5">5 - Excellent</option>
            <option value="4">4 - Very good</option>
            <option value="3">3 - Good</option>
            <option value="2">2 - Fair</option>
            <option value="1">1 - Poor</option>
          </select>
          <button class="btn btn-outline" type="submit">Submit rating</button>
        </div>
      </form>
    `, hotelID, hotelID)
	} else {
		ratingActions = buildGuestRateButton(hotelID)
	}

	ratingNotice := ""
	query := r.URL.Query()
	if query.Get("rated") == "1" {
		ratingNotice = `<div class="notice notice-success">Thanks! Your rating was saved.</div>`
	} else if query.Get("ratingError") == "1" {
		ratingNotice = `<div class="notice notice-warning">Please choose a valid rating from 1 to 5.</div>`
	}

	presenceEnabled := "false"
	if a.Env.PresenceEnabled {
		presenceEnabled = "true"
	}

	return a.renderHTML(w, http.StatusOK, "hotels-item.html", map[string]any{
		"id":              hotelID,
		"title":           stringValue(hotel, "title"),
		"description":     stringValue(hotel, "description"),
		"location":        stringValue(hotel, "location"),
		"address":         stringValue(hotel, "address"),
		"imageUrl":        imageURL,
		"price":           fmt.Sprintf("%s KZT / night", formatNumber(floatValue(hotel, "price_per_night"))),
		"rating":          formatNumber(rating),
		"ratingStars":     view.Safe(ratingStars),
		"ratingVotes":     ratingVotesText,
		"available_rooms": formatInt(intValue(hotel, "available_rooms")),
		"amenities":       amenitiesText,
		"authControls":    view.Safe(renderAuthControls(user, "/hotels/"+hotelID)),
		"ratingNotice":    view.Safe(ratingNotice),
		"ratingActions":   view.Safe(ratingActions),
		"bookButton":      view.Safe(bookButton),
		"manageButtons":   view.Safe(manageButtons),
		"presenceEnabled": presenceEnabled,
		"heartbeatEvery":  "15",
	})
}

func (a *App) renderEditHotelPage(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		return sendHotelNotFoundPage(a, w, r, http.StatusBadRequest)
	}

	hotel, err := a.Store.FindHotelByID(r.Context(), id, nil)
	if err != nil {
		return err
	}
	if hotel == nil {
		return sendHotelNotFoundPage(a, w, r, http.StatusNotFound)
	}

	hotelID := objectIDHex(hotel["_id"])
	return a.renderHTML(w, http.StatusOK, "hotels-edit.html", map[string]any{
		"id":              hotelID,
		"title":           stringValue(hotel, "title"),
		"description":     stringValue(hotel, "description"),
		"location":        stringValue(hotel, "location"),
		"address":         stringValue(hotel, "address"),
		"price_per_night": formatNumber(floatValue(hotel, "price_per_night")),
		"rating":          formatNumber(floatValue(hotel, "rating")),
		"available_rooms": formatInt(intValue(hotel, "available_rooms")),
		"amenities":       strings.Join(stringSliceValue(hotel, "amenities"), ", "),
		"imageUrl":        stringValue(hotel, "imageUrl"),
		"authControls":    view.Safe(renderAuthControls(session.CurrentUser(r), "/hotels/"+hotelID+"/edit")),
		"errorMessage":    "",
	})
}

func (a *App) updateHotelFromPage(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		return sendHotelNotFoundPage(a, w, r, http.StatusBadRequest)
	}

	payload, err := a.parsePayload(r)
	if err != nil {
		return err
	}

	validationErrors, hotel := utils.ValidateHotelPayload(payload, false)
	if len(validationErrors) > 0 {
		return a.renderHTML(w, http.StatusBadRequest, "hotels-edit.html", map[string]any{
			"id":              id,
			"title":           utils.ToTrimmedString(payload["title"]),
			"description":     utils.ToTrimmedString(payload["description"]),
			"location":        utils.ToTrimmedString(payload["location"]),
			"address":         utils.ToTrimmedString(payload["address"]),
			"price_per_night": utils.ToTrimmedString(payload["price_per_night"]),
			"rating":          utils.ToTrimmedString(payload["rating"]),
			"available_rooms": utils.ToTrimmedString(payload["available_rooms"]),
			"amenities":       utils.ToTrimmedString(payload["amenities"]),
			"imageUrl":        utils.ToTrimmedString(payload["imageUrl"]),
			"authControls":    view.Safe(renderAuthControls(session.CurrentUser(r), "/hotels/"+id+"/edit")),
			"errorMessage":    validationErrors[0],
		})
	}

	matched, err := a.Store.UpdateHotelByID(r.Context(), id, hotel)
	if err != nil {
		return err
	}
	if matched == 0 {
		return sendHotelNotFoundPage(a, w, r, http.StatusNotFound)
	}

	http.Redirect(w, r, "/hotels/"+id, http.StatusFound)
	return nil
}

func (a *App) deleteHotelFromPage(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		return sendHotelNotFoundPage(a, w, r, http.StatusBadRequest)
	}

	deleted, err := a.Store.DeleteHotelByID(r.Context(), id)
	if err != nil {
		return err
	}
	if deleted == 0 {
		return sendHotelNotFoundPage(a, w, r, http.StatusNotFound)
	}

	http.Redirect(w, r, "/hotels", http.StatusFound)
	return nil
}

func (a *App) rateHotelFromPage(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		return sendHotelNotFoundPage(a, w, r, http.StatusBadRequest)
	}

	payload, err := a.parsePayload(r)
	if err != nil {
		return err
	}

	score, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprint(payload["score"])))
	nextPath := getSafeRedirectPath(utils.ToTrimmedString(payload["next"]), "/hotels/"+id)
	if score < 1 || score > 5 {
		separator := "?"
		if strings.Contains(nextPath, "?") {
			separator = "&"
		}
		http.Redirect(w, r, nextPath+separator+"ratingError=1", http.StatusFound)
		return nil
	}

	matched, _, _, err := a.Store.RateHotelByID(r.Context(), id, score)
	if err != nil {
		return err
	}
	if matched == 0 {
		return sendHotelNotFoundPage(a, w, r, http.StatusNotFound)
	}

	separator := "?"
	if strings.Contains(nextPath, "?") {
		separator = "&"
	}
	http.Redirect(w, r, nextPath+separator+"rated=1", http.StatusFound)
	return nil
}

func (a *App) getHotelsAPI(w http.ResponseWriter, r *http.Request) error {
	query := r.URL.Query()
	pagination := utils.GetPagination(query.Get("page"), query.Get("limit"), a.Env.HotelsPageSize, a.Env.HotelsPageMax)
	filter := models.BuildHotelFilterFromQuery(query)
	sortQuery := models.BuildHotelSortFromQuery(query.Get("sort"))
	projection := models.BuildHotelProjectionFromQuery(query.Get("fields"))

	items, total, err := a.Store.FindHotels(r.Context(), filter, sortQuery, projection, pagination.Skip, int64(pagination.Limit))
	if err != nil {
		return err
	}

	meta := utils.GetPaginationMeta(total, pagination.Page, pagination.Limit)
	a.writeJSON(w, http.StatusOK, map[string]any{"items": items, "meta": meta})
	return nil
}

func (a *App) getHotelByIDAPI(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		a.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid ID"})
		return nil
	}

	projection := models.BuildHotelProjectionFromQuery(r.URL.Query().Get("fields"))
	hotel, err := a.Store.FindHotelByID(r.Context(), id, projection)
	if err != nil {
		return err
	}
	if hotel == nil {
		a.writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return nil
	}

	a.writeJSON(w, http.StatusOK, hotel)
	return nil
}

func (a *App) createHotelAPI(w http.ResponseWriter, r *http.Request) error {
	payload, err := a.parsePayload(r)
	if err != nil {
		return err
	}

	validationErrors, hotel := utils.ValidateHotelPayload(payload, false)
	if len(validationErrors) > 0 {
		a.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid hotel data"})
		return nil
	}

	user := session.CurrentUser(r)
	createdBy := ""
	if user != nil {
		createdBy = user.ID
	}

	insertedID, err := a.Store.CreateHotel(r.Context(), hotel, createdBy)
	if err != nil {
		return err
	}

	a.writeJSON(w, http.StatusCreated, map[string]string{"_id": insertedID})
	return nil
}

func (a *App) updateHotelAPI(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		a.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid ID"})
		return nil
	}

	payload, err := a.parsePayload(r)
	if err != nil {
		return err
	}

	validationErrors, hotel := utils.ValidateHotelPayload(payload, true)
	if len(validationErrors) > 0 {
		a.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid hotel data"})
		return nil
	}

	matched, err := a.Store.UpdateHotelByID(r.Context(), id, hotel)
	if err != nil {
		return err
	}
	if matched == 0 {
		a.writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return nil
	}

	a.writeJSON(w, http.StatusOK, map[string]string{"message": "Updated"})
	return nil
}

func (a *App) deleteHotelAPI(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		a.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid ID"})
		return nil
	}

	deleted, err := a.Store.DeleteHotelByID(r.Context(), id)
	if err != nil {
		return err
	}
	if deleted == 0 {
		a.writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return nil
	}

	a.writeJSON(w, http.StatusOK, map[string]string{"message": "Deleted"})
	return nil
}

func (a *App) rateHotelAPI(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		a.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid ID"})
		return nil
	}

	payload, err := a.parsePayload(r)
	if err != nil {
		return err
	}

	score, parseErr := strconv.Atoi(strings.TrimSpace(fmt.Sprint(payload["score"])))
	if parseErr != nil || score < 1 || score > 5 {
		a.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid rating score"})
		return nil
	}

	matched, rating, ratingVotes, err := a.Store.RateHotelByID(r.Context(), id, score)
	if err != nil {
		return err
	}
	if matched == 0 {
		a.writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return nil
	}

	a.writeJSON(w, http.StatusOK, map[string]any{
		"message":     "Rating saved",
		"rating":      rating,
		"ratingVotes": ratingVotes,
	})
	return nil
}

func formatNumber(value float64) string {
	if math.Mod(value, 1) == 0 {
		return strconv.Itoa(int(value))
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func formatInt(value int) string {
	return strconv.Itoa(value)
}
