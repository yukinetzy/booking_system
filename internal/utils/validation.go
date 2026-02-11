package utils

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var (
	emailRegex    = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	imageURLRegex = regexp.MustCompile(`^https?://\S+$`)
	phoneRegex    = regexp.MustCompile(`^\+?[0-9\s()-]{8,20}$`)
	fragmentRegex = regexp.MustCompile(`^[a-z0-9]{3}$`)
	isoDateRegex  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

type RegisterUser struct {
	Email    string
	Password string
}

type PasswordRules struct {
	LengthRule  bool
	LowerRule   bool
	UpperRule   bool
	DigitRule   bool
	SpecialRule bool
	OverlapRule bool
}

func ToTrimmedString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func ValidateHotelPayload(payload map[string]any, partial bool) ([]string, bson.M) {
	errors := make([]string, 0)
	hotel := bson.M{}
	shouldValidate := func(field string) bool {
		return !partial || hasOwn(payload, field)
	}

	if shouldValidate("title") {
		title := ToTrimmedString(payload["title"])
		if len(title) < 3 || len(title) > 120 {
			errors = append(errors, "Invalid title")
		} else {
			hotel["title"] = title
		}
	} else if !partial {
		errors = append(errors, "Missing title")
	}

	if shouldValidate("description") {
		description := ToTrimmedString(payload["description"])
		if len(description) < 10 || len(description) > 1200 {
			errors = append(errors, "Invalid description")
		} else {
			hotel["description"] = description
		}
	} else if !partial {
		errors = append(errors, "Missing description")
	}

	if shouldValidate("location") {
		location := ToTrimmedString(payload["location"])
		if len(location) < 2 || len(location) > 80 {
			errors = append(errors, "Invalid location")
		} else {
			hotel["location"] = location
		}
	} else if !partial {
		errors = append(errors, "Missing location")
	}

	if shouldValidate("address") {
		address := ToTrimmedString(payload["address"])
		if len(address) < 5 || len(address) > 180 {
			errors = append(errors, "Invalid address")
		} else {
			hotel["address"] = address
		}
	} else if !partial {
		errors = append(errors, "Missing address")
	}

	if shouldValidate("price_per_night") {
		price, ok := numberFromAny(payload["price_per_night"])
		if !ok || price <= 0 || price > 1000000 {
			errors = append(errors, "Invalid price")
		} else {
			hotel["price_per_night"] = price
		}
	} else if !partial {
		errors = append(errors, "Missing price")
	}

	if shouldValidate("rating") {
		rating, ok := numberFromAny(payload["rating"])
		if !ok || rating < 1 || rating > 5 {
			errors = append(errors, "Invalid rating")
		} else {
			hotel["rating"] = math.Round(rating*10) / 10
		}
	} else if !partial {
		errors = append(errors, "Missing rating")
	}

	if shouldValidate("available_rooms") {
		rooms, ok := intFromAny(payload["available_rooms"])
		if !ok || rooms < 0 || rooms > 1000 {
			errors = append(errors, "Invalid available rooms")
		} else {
			hotel["available_rooms"] = rooms
		}
	} else if !partial {
		errors = append(errors, "Missing available rooms")
	}

	if shouldValidate("amenities") {
		amenities := normalizeAmenities(payload["amenities"])
		tooLong := false
		for _, amenity := range amenities {
			if len(amenity) > 40 {
				tooLong = true
				break
			}
		}
		if len(amenities) < 1 || len(amenities) > 10 || tooLong {
			errors = append(errors, "Invalid amenities")
		} else {
			hotel["amenities"] = amenities
		}
	} else if !partial {
		errors = append(errors, "Missing amenities")
	}

	if shouldValidate("imageUrl") {
		imageURL := ToTrimmedString(payload["imageUrl"])
		if imageURL != "" && (!imageURLRegex.MatchString(imageURL) || len(imageURL) > 400) {
			errors = append(errors, "Invalid image URL")
		} else {
			hotel["imageUrl"] = imageURL
		}
	}

	if partial && len(hotel) == 0 {
		errors = append(errors, "No valid fields provided")
	}

	return errors, hotel
}

func ValidateContactPayload(payload map[string]any) (map[string]string, []string) {
	clean := map[string]string{
		"name":    ToTrimmedString(payload["name"]),
		"phone":   ToTrimmedString(payload["phone"]),
		"city":    ToTrimmedString(payload["city"]),
		"email":   strings.ToLower(ToTrimmedString(payload["email"])),
		"message": ToTrimmedString(payload["message"]),
	}

	errors := make([]string, 0)
	if len(clean["name"]) < 2 || len(clean["name"]) > 80 {
		errors = append(errors, "Please provide a valid full name.")
	}
	if !phoneRegex.MatchString(clean["phone"]) {
		errors = append(errors, "Please provide a valid phone number.")
	}
	if len(clean["city"]) < 2 || len(clean["city"]) > 80 {
		errors = append(errors, "Please provide a valid city.")
	}
	if !emailRegex.MatchString(clean["email"]) {
		errors = append(errors, "Please provide a valid email.")
	}
	if len(clean["message"]) < 5 || len(clean["message"]) > 1000 {
		errors = append(errors, "Message should be between 5 and 1000 characters.")
	}

	return clean, errors
}

func ValidateRegisterPayload(payload map[string]any) ([]string, RegisterUser) {
	email := strings.ToLower(ToTrimmedString(payload["email"]))
	password := ToTrimmedString(payload["password"])
	confirmPassword := ToTrimmedString(payload["confirmPassword"])

	termsRaw := ToTrimmedString(payload["terms"])
	termsAccepted := termsRaw == "on" || termsRaw == "true" || payload["terms"] == true

	errors := make([]string, 0)
	if !emailRegex.MatchString(email) {
		errors = append(errors, "Valid email is required.")
	}

	rules := EvaluatePasswordRules(password, email)
	if !rules.LengthRule || !rules.LowerRule || !rules.UpperRule || !rules.DigitRule || !rules.SpecialRule || !rules.OverlapRule {
		errors = append(errors, "Password does not meet security requirements.")
	}

	if password != confirmPassword {
		errors = append(errors, "Password confirmation does not match.")
	}

	if !termsAccepted {
		errors = append(errors, "You must accept the terms to continue.")
	}

	return errors, RegisterUser{Email: email, Password: password}
}

func EvaluatePasswordRules(password string, email string) PasswordRules {
	specialCount := 0
	for _, char := range password {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')) {
			specialCount++
		}
	}

	referenceToken := extractReferenceToken(email)

	return PasswordRules{
		LengthRule:  len(password) >= 8 && len(password) <= 50,
		LowerRule:   regexp.MustCompile(`[a-z]`).MatchString(password),
		UpperRule:   regexp.MustCompile(`[A-Z]`).MatchString(password),
		DigitRule:   regexp.MustCompile(`[0-9]`).MatchString(password),
		SpecialRule: specialCount >= 1 && specialCount <= 10,
		OverlapRule: !hasThreeCharOverlap(password, referenceToken),
	}
}

func ValidateBookingPayload(payload map[string]any, partial bool) ([]string, bson.M) {
	errors := make([]string, 0)
	booking := bson.M{}
	shouldValidate := func(field string) bool {
		return !partial || hasOwn(payload, field)
	}

	hasRoomKey := hasOwn(payload, "roomId") || hasOwn(payload, "room_id") || hasOwn(payload, "hotelId")
	if !partial || hasRoomKey {
		roomIDText := firstNonEmptyString(
			ToTrimmedString(payload["roomId"]),
			ToTrimmedString(payload["room_id"]),
			ToTrimmedString(payload["hotelId"]),
		)
		objectID, err := primitive.ObjectIDFromHex(roomIDText)
		if err != nil {
			errors = append(errors, "Invalid room ID")
		} else {
			booking["roomId"] = objectID
			booking["hotelId"] = objectID
		}
	} else if !partial {
		errors = append(errors, "Missing room ID")
	}

	checkInText, checkInDate, checkInOK := "", time.Time{}, false
	checkOutText, checkOutDate, checkOutOK := "", time.Time{}, false

	checkInRaw, hasCheckIn := payload["checkIn"]
	if !hasCheckIn {
		checkInRaw, hasCheckIn = payload["check_in"]
	}
	checkOutRaw, hasCheckOut := payload["checkOut"]
	if !hasCheckOut {
		checkOutRaw, hasCheckOut = payload["check_out"]
	}

	if !partial || hasCheckIn {
		checkInText, checkInDate, checkInOK = parseISODate(checkInRaw)
		if !checkInOK {
			errors = append(errors, "Invalid check-in date")
		} else {
			booking["checkIn"] = checkInText
		}
	} else if !partial {
		errors = append(errors, "Missing check-in date")
	}

	if !partial || hasCheckOut {
		checkOutText, checkOutDate, checkOutOK = parseISODate(checkOutRaw)
		if !checkOutOK {
			errors = append(errors, "Invalid check-out date")
		} else {
			booking["checkOut"] = checkOutText
		}
	} else if !partial {
		errors = append(errors, "Missing check-out date")
	}

	if checkInOK && checkOutOK && !checkOutDate.After(checkInDate) {
		errors = append(errors, "Check-out must be after check-in")
	}
	if checkInOK {
		now := time.Now().In(time.Local)
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
		checkInLocal := time.Date(checkInDate.Year(), checkInDate.Month(), checkInDate.Day(), 0, 0, 0, 0, time.Local)
		if checkInLocal.Before(today) {
			errors = append(errors, "Check-in date must be today or later")
		}
	}

	if shouldValidate("guests") {
		guests, ok := intFromAny(payload["guests"])
		if !ok || guests < 1 || guests > 10 {
			errors = append(errors, "Invalid guest count")
		} else {
			booking["guests"] = guests
		}
	} else if !partial {
		errors = append(errors, "Missing guest count")
	}

	if shouldValidate("notes") {
		notes := ToTrimmedString(payload["notes"])
		if len(notes) > 400 {
			errors = append(errors, "Notes too long")
		} else {
			booking["notes"] = notes
		}
	}

	if partial && len(booking) == 0 {
		errors = append(errors, "No valid fields provided")
	}

	return errors, booking
}

func hasOwn(payload map[string]any, key string) bool {
	_, ok := payload[key]
	return ok
}

func normalizeAmenities(value any) []string {
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			trimmed := ToTrimmedString(item)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			trimmed := ToTrimmedString(item)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case string:
		parts := strings.Split(typed, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	default:
		trimmed := ToTrimmedString(value)
		if trimmed == "" {
			return []string{}
		}
		parts := strings.Split(trimmed, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			item := strings.TrimSpace(part)
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	}
}

func numberFromAny(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case string:
		if strings.TrimSpace(typed) == "" {
			return 0, false
		}
		number, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, false
		}
		return number, true
	case fmt.Stringer:
		number, err := strconv.ParseFloat(strings.TrimSpace(typed.String()), 64)
		if err != nil {
			return 0, false
		}
		return number, true
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" {
			return 0, false
		}
		number, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return 0, false
		}
		return number, true
	}
}

func intFromAny(value any) (int, bool) {
	number, ok := numberFromAny(value)
	if !ok {
		return 0, false
	}
	if math.Mod(number, 1) != 0 {
		return 0, false
	}
	return int(number), true
}

func extractReferenceToken(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return ""
	}
	parts := strings.Split(email, "@")
	if len(parts) == 0 {
		return email
	}
	if parts[0] == "" {
		return email
	}
	return parts[0]
}

func hasThreeCharOverlap(password, reference string) bool {
	password = strings.ToLower(password)
	reference = strings.ToLower(reference)
	if len(password) < 3 || len(reference) < 3 {
		return false
	}

	for i := 0; i <= len(reference)-3; i++ {
		fragment := reference[i : i+3]
		if !fragmentRegex.MatchString(fragment) {
			continue
		}
		if strings.Contains(password, fragment) {
			return true
		}
	}

	return false
}

func parseISODate(value any) (string, time.Time, bool) {
	text := ToTrimmedString(value)
	if !isoDateRegex.MatchString(text) {
		return "", time.Time{}, false
	}

	date, err := time.ParseInLocation("2006-01-02", text, time.Local)
	if err != nil {
		return "", time.Time{}, false
	}

	return text, date, true
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
