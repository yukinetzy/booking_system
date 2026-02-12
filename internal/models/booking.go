package models

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"easybook/internal/types"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	bookingsCollection     = "bookings"
	roomCalendarCollection = "room_calendar"
)

func BuildBookingFilterFromQuery(query url.Values, currentUser *types.CurrentUser, includeAll bool) bson.M {
	filter := bson.M{}

	if !includeAll && currentUser != nil {
		if userID, err := primitive.ObjectIDFromHex(currentUser.ID); err == nil {
			filter["userId"] = userID
		}
	}

	roomID := firstNonEmpty(
		strings.TrimSpace(query.Get("room_id")),
		strings.TrimSpace(query.Get("roomId")),
		strings.TrimSpace(query.Get("hotelId")),
	)
	if roomID != "" {
		if parsed, err := primitive.ObjectIDFromHex(roomID); err == nil {
			filter["$or"] = bson.A{
				bson.M{"roomId": parsed},
				bson.M{"hotelId": parsed},
			}
		}
	}

	return filter
}

func (s *Store) ListBookingsWithDetails(ctx context.Context, filter bson.M, skip int64, limit int64) ([]bson.M, int64, error) {
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: filter}},
		bson.D{{Key: "$sort", Value: bson.D{{Key: "createdAt", Value: -1}}}},
		bson.D{{Key: "$addFields", Value: bson.M{
			"hotelRef": bson.M{"$ifNull": bson.A{"$hotelId", "$roomId"}},
			"roomId":   bson.M{"$ifNull": bson.A{"$roomId", "$hotelId"}},
		}}},
		bson.D{{Key: "$facet", Value: bson.M{
			"items": bson.A{
				bson.D{{Key: "$skip", Value: skip}},
				bson.D{{Key: "$limit", Value: limit}},
				bson.D{{Key: "$lookup", Value: bson.M{
					"from":         "hotels",
					"localField":   "hotelRef",
					"foreignField": "_id",
					"as":           "hotel",
				}}},
				bson.D{{Key: "$lookup", Value: bson.M{
					"from":         "users",
					"localField":   "userId",
					"foreignField": "_id",
					"as":           "user",
				}}},
				bson.D{{Key: "$unwind", Value: bson.M{"path": "$hotel", "preserveNullAndEmptyArrays": true}}},
				bson.D{{Key: "$unwind", Value: bson.M{"path": "$user", "preserveNullAndEmptyArrays": true}}},
				bson.D{{Key: "$project", Value: bson.M{
					"roomId":        1,
					"hotelId":       "$hotelRef",
					"userId":        1,
					"checkIn":       1,
					"checkOut":      1,
					"guests":        1,
					"notes":         1,
					"createdAt":     1,
					"updatedAt":     1,
					"groupId":       1,
					"hotelTitle":    "$hotel.title",
					"hotelLocation": "$hotel.location",
					"userEmail":     "$user.email",
				}}},
			},
			"totalCount": bson.A{
				bson.D{{Key: "$count", Value: "count"}},
			},
		}}},
	}

	cursor, err := s.collection(bookingsCollection).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	type counter struct {
		Count int64 `bson:"count"`
	}
	type result struct {
		Items      []bson.M  `bson:"items"`
		TotalCount []counter `bson:"totalCount"`
	}

	results := make([]result, 0, 1)
	if err := cursor.All(ctx, &results); err != nil {
		return nil, 0, err
	}

	if len(results) == 0 {
		return []bson.M{}, 0, nil
	}

	total := int64(0)
	if len(results[0].TotalCount) > 0 {
		total = results[0].TotalCount[0].Count
	}

	return results[0].Items, total, nil
}

func (s *Store) FindBookingByIDWithDetails(ctx context.Context, id string) (bson.M, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, nil
	}

	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.M{"_id": objectID}}},
		bson.D{{Key: "$addFields", Value: bson.M{
			"hotelRef": bson.M{"$ifNull": bson.A{"$hotelId", "$roomId"}},
			"roomId":   bson.M{"$ifNull": bson.A{"$roomId", "$hotelId"}},
		}}},
		bson.D{{Key: "$lookup", Value: bson.M{
			"from":         "hotels",
			"localField":   "hotelRef",
			"foreignField": "_id",
			"as":           "hotel",
		}}},
		bson.D{{Key: "$lookup", Value: bson.M{
			"from":         "users",
			"localField":   "userId",
			"foreignField": "_id",
			"as":           "user",
		}}},
		bson.D{{Key: "$unwind", Value: bson.M{"path": "$hotel", "preserveNullAndEmptyArrays": true}}},
		bson.D{{Key: "$unwind", Value: bson.M{"path": "$user", "preserveNullAndEmptyArrays": true}}},
		bson.D{{Key: "$project", Value: bson.M{
			"roomId":        1,
			"hotelId":       "$hotelRef",
			"userId":        1,
			"checkIn":       1,
			"checkOut":      1,
			"guests":        1,
			"notes":         1,
			"createdAt":     1,
			"updatedAt":     1,
			"groupId":       1,
			"hotelTitle":    "$hotel.title",
			"hotelLocation": "$hotel.location",
			"userEmail":     "$user.email",
		}}},
	}

	cursor, err := s.collection(bookingsCollection).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	items := make([]bson.M, 0, 1)
	if err := cursor.All(ctx, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}

	return items[0], nil
}

func (s *Store) CheckRoomAvailability(ctx context.Context, roomIDText, checkIn, checkOut string, excludeBookingIDText string) (bool, error) {
	roomID, err := primitive.ObjectIDFromHex(strings.TrimSpace(roomIDText))
	if err != nil {
		return false, fmt.Errorf("%w: invalid room id", ErrInvalidBookingPayload)
	}

	if _, _, err := parseBookingDateRange(checkIn, checkOut); err != nil {
		return false, fmt.Errorf("%w: %v", ErrInvalidBookingPayload, err)
	}

	var excludeID *primitive.ObjectID
	if strings.TrimSpace(excludeBookingIDText) != "" {
		parsed, parseErr := primitive.ObjectIDFromHex(strings.TrimSpace(excludeBookingIDText))
		if parseErr != nil {
			return false, fmt.Errorf("%w: invalid booking id", ErrInvalidBookingPayload)
		}
		excludeID = &parsed
	}

	conflict, err := s.hasBookingConflict(ctx, roomID, strings.TrimSpace(checkIn), strings.TrimSpace(checkOut), excludeID)
	if err != nil {
		return false, err
	}

	return !conflict, nil
}

func (s *Store) CreateBooking(ctx context.Context, booking bson.M, userID string) (string, error) {
	ownerID, err := primitive.ObjectIDFromHex(strings.TrimSpace(userID))
	if err != nil {
		return "", fmt.Errorf("%w: invalid user id", ErrInvalidBookingPayload)
	}

	roomID, checkIn, checkOut, err := extractBookingCoreFromMap(booking)
	if err != nil {
		return "", err
	}

	checkInDate, _, err := parseBookingDateRange(checkIn, checkOut)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidBookingPayload, err)
	}
	if isBeforeToday(checkInDate) {
		return "", fmt.Errorf("%w: check-in date must be today or later", ErrInvalidBookingPayload)
	}

	bookingID := primitive.NewObjectID()
	now := time.Now().UTC()
	doc := bson.M{}
	for key, value := range booking {
		doc[key] = value
	}
	doc["_id"] = bookingID
	doc["userId"] = ownerID
	doc["roomId"] = roomID
	doc["hotelId"] = roomID
	doc["checkIn"] = checkIn
	doc["checkOut"] = checkOut
	doc["status"] = "confirmed"
	doc["createdAt"] = now
	doc["updatedAt"] = now

	if groupRaw, ok := booking["groupId"]; ok {
		if gid, err := primitive.ObjectIDFromHex(strings.TrimSpace(fmt.Sprint(groupRaw))); err == nil {
			doc["groupId"] = gid
		}
	}

	err = s.runAtomically(ctx, func(txCtx context.Context) error {
		conflict, conflictErr := s.hasBookingConflict(txCtx, roomID, checkIn, checkOut, nil)
		if conflictErr != nil {
			return conflictErr
		}
		if conflict {
			return ErrBookingConflict
		}

		if reserveErr := s.reserveRoomCalendar(txCtx, roomID, bookingID, checkIn, checkOut); reserveErr != nil {
			if IsDuplicateKeyError(reserveErr, "roomId") {
				return ErrBookingConflict
			}
			return reserveErr
		}

		if _, insertErr := s.collection(bookingsCollection).InsertOne(txCtx, doc); insertErr != nil {
			return insertErr
		}

		return nil
	})
	if err != nil {
		return "", err
	}

	return bookingID.Hex(), nil
}

func (s *Store) UpdateBookingByID(ctx context.Context, id string, booking bson.M) (int64, error) {
	objectID, err := primitive.ObjectIDFromHex(strings.TrimSpace(id))
	if err != nil {
		return 0, nil
	}

	matchedCount := int64(0)
	err = s.runAtomically(ctx, func(txCtx context.Context) error {
		var existing bson.M
		findErr := s.collection(bookingsCollection).FindOne(txCtx, bson.M{"_id": objectID}).Decode(&existing)
		if errors.Is(findErr, mongo.ErrNoDocuments) {
			matchedCount = 0
			return nil
		}
		if findErr != nil {
			return findErr
		}
		matchedCount = 1

		existingRoomID, existingCheckIn, existingCheckOut, extractErr := extractBookingCoreFromMap(existing)
		if extractErr != nil {
			return extractErr
		}

		nextRoomID := existingRoomID
		nextCheckIn := existingCheckIn
		nextCheckOut := existingCheckOut
		enforceToday := false

		if hasAnyKey(booking, "roomId", "room_id", "hotelId") {
			parsedRoomID, roomErr := extractRoomIDFromMap(booking)
			if roomErr != nil {
				return roomErr
			}
			nextRoomID = parsedRoomID
		}

		if value, ok := booking["checkIn"]; ok {
			nextCheckIn = strings.TrimSpace(fmt.Sprint(value))
			enforceToday = true
		}
		if value, ok := booking["checkOut"]; ok {
			nextCheckOut = strings.TrimSpace(fmt.Sprint(value))
		}

		checkInDate, _, rangeErr := parseBookingDateRange(nextCheckIn, nextCheckOut)
		if rangeErr != nil {
			return fmt.Errorf("%w: %v", ErrInvalidBookingPayload, rangeErr)
		}
		if enforceToday && isBeforeToday(checkInDate) {
			return fmt.Errorf("%w: check-in date must be today or later", ErrInvalidBookingPayload)
		}

		conflict, conflictErr := s.hasBookingConflict(txCtx, nextRoomID, nextCheckIn, nextCheckOut, &objectID)
		if conflictErr != nil {
			return conflictErr
		}
		if conflict {
			return ErrBookingConflict
		}

		slotChanged := existingRoomID != nextRoomID || existingCheckIn != nextCheckIn || existingCheckOut != nextCheckOut
		if slotChanged {
			if releaseErr := s.releaseRoomCalendar(txCtx, objectID); releaseErr != nil {
				return releaseErr
			}
			if reserveErr := s.reserveRoomCalendar(txCtx, nextRoomID, objectID, nextCheckIn, nextCheckOut); reserveErr != nil {
				if IsDuplicateKeyError(reserveErr, "roomId") {
					return ErrBookingConflict
				}
				return reserveErr
			}
		}

		updateFields := bson.M{}
		for key, value := range booking {
			updateFields[key] = value
		}
		if groupRaw, ok := booking["groupId"]; ok {
			if gid, err := primitive.ObjectIDFromHex(strings.TrimSpace(fmt.Sprint(groupRaw))); err == nil {
				updateFields["groupId"] = gid
			}
		}

		delete(updateFields, "room_id")
		delete(updateFields, "status")
		updateFields["roomId"] = nextRoomID
		updateFields["hotelId"] = nextRoomID
		updateFields["checkIn"] = nextCheckIn
		updateFields["checkOut"] = nextCheckOut
		updateFields["status"] = "confirmed"
		updateFields["updatedAt"] = time.Now().UTC()

		_, updateErr := s.collection(bookingsCollection).UpdateOne(
			txCtx,
			bson.M{"_id": objectID},
			bson.M{"$set": updateFields},
		)
		return updateErr
	})
	if err != nil {
		return 0, err
	}

	return matchedCount, nil

}

func (s *Store) DeleteBookingByID(ctx context.Context, id string) (int64, error) {
	objectID, err := primitive.ObjectIDFromHex(strings.TrimSpace(id))
	if err != nil {
		return 0, nil
	}

	deletedCount := int64(0)
	err = s.runAtomically(ctx, func(txCtx context.Context) error {
		result, deleteErr := s.collection(bookingsCollection).DeleteOne(txCtx, bson.M{"_id": objectID})
		if deleteErr != nil {
			return deleteErr
		}
		deletedCount = result.DeletedCount
		if deletedCount == 0 {
			return nil
		}

		_, calendarErr := s.collection(roomCalendarCollection).DeleteMany(txCtx, bson.M{"bookingId": objectID})
		return calendarErr
	})
	if err != nil {
		return 0, err
	}

	return deletedCount, nil
}

func (s *Store) hasBookingConflict(ctx context.Context, roomID primitive.ObjectID, checkIn, checkOut string, excludeBookingID *primitive.ObjectID) (bool, error) {
	newCheckInDate, newCheckOutDate, err := parseBookingDateRange(checkIn, checkOut)
	if err != nil {
		return false, err
	}

	days, err := buildDateSlots(checkIn, checkOut)
	if err != nil {
		return false, err
	}
	if len(days) > 0 {
		calendarFilter := bson.M{
			"roomId": roomID,
			"day":    bson.M{"$in": days},
		}
		if excludeBookingID != nil {
			calendarFilter["bookingId"] = bson.M{"$ne": *excludeBookingID}
		}

		calendarCount, calendarErr := s.collection(roomCalendarCollection).CountDocuments(ctx, calendarFilter)
		if calendarErr != nil {
			return false, calendarErr
		}
		if calendarCount > 0 {
			return true, nil
		}
	}

	roomIDHex := roomID.Hex()
	cursor, err := s.collection(bookingsCollection).Find(
		ctx,
		bson.M{
			"$or": bson.A{
				bson.M{"roomId": roomID},
				bson.M{"roomId": roomIDHex},
				bson.M{"hotelId": roomID},
				bson.M{"hotelId": roomIDHex},
			},
		},
		options.Find().SetProjection(bson.M{
			"_id":       1,
			"roomId":    1,
			"hotelId":   1,
			"status":    1,
			"checkIn":   1,
			"checkOut":  1,
			"check_in":  1,
			"check_out": 1,
		}).SetLimit(6000),
	)
	if err != nil {
		return false, err
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var existing bson.M
		if decodeErr := cursor.Decode(&existing); decodeErr != nil {
			return false, decodeErr
		}

		existingID, _ := existing["_id"].(primitive.ObjectID)
		if excludeBookingID != nil && existingID == *excludeBookingID {
			continue
		}

		if isCancelledStatus(existing["status"]) {
			continue
		}

		existingCheckInValue, hasCheckIn := existing["checkIn"]
		if !hasCheckIn {
			existingCheckInValue, hasCheckIn = existing["check_in"]
		}
		existingCheckOutValue, hasCheckOut := existing["checkOut"]
		if !hasCheckOut {
			existingCheckOutValue, hasCheckOut = existing["check_out"]
		}
		if !hasCheckIn || !hasCheckOut {
			continue
		}

		existingCheckInDate, checkInErr := parseBookingDateValue(existingCheckInValue)
		if checkInErr != nil {
			continue
		}
		existingCheckOutDate, checkOutErr := parseBookingDateValue(existingCheckOutValue)
		if checkOutErr != nil {
			continue
		}
		if !existingCheckOutDate.After(existingCheckInDate) {
			continue
		}

		if newCheckInDate.Before(existingCheckOutDate) && newCheckOutDate.After(existingCheckInDate) {
			return true, nil
		}
	}
	if err := cursor.Err(); err != nil {
		return false, err
	}

	return false, nil
}

func (s *Store) reserveRoomCalendar(ctx context.Context, roomID primitive.ObjectID, bookingID primitive.ObjectID, checkIn, checkOut string) error {
	days, err := buildDateSlots(checkIn, checkOut)
	if err != nil {
		return err
	}
	if len(days) == 0 {
		return fmt.Errorf("%w: booking period must be at least one night", ErrInvalidBookingPayload)
	}

	now := time.Now().UTC()
	documents := make([]any, 0, len(days))
	for _, day := range days {
		documents = append(documents, bson.M{
			"roomId":    roomID,
			"day":       day,
			"bookingId": bookingID,
			"createdAt": now,
		})
	}

	_, err = s.collection(roomCalendarCollection).InsertMany(ctx, documents)
	return err
}

func (s *Store) releaseRoomCalendar(ctx context.Context, bookingID primitive.ObjectID) error {
	_, err := s.collection(roomCalendarCollection).DeleteMany(ctx, bson.M{"bookingId": bookingID})
	return err
}

func (s *Store) runAtomically(ctx context.Context, fn func(context.Context) error) error {
	session, err := s.db.Client().StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(txCtx mongo.SessionContext) (any, error) {
		if callbackErr := fn(txCtx); callbackErr != nil {
			return nil, callbackErr
		}
		return nil, nil
	})
	return err
}

func extractBookingCoreFromMap(document bson.M) (primitive.ObjectID, string, string, error) {
	roomID, err := extractRoomIDFromMap(document)
	if err != nil {
		return primitive.NilObjectID, "", "", err
	}

	checkIn := strings.TrimSpace(fmt.Sprint(document["checkIn"]))
	checkOut := strings.TrimSpace(fmt.Sprint(document["checkOut"]))
	if _, _, rangeErr := parseBookingDateRange(checkIn, checkOut); rangeErr != nil {
		return primitive.NilObjectID, "", "", fmt.Errorf("%w: %v", ErrInvalidBookingPayload, rangeErr)
	}

	return roomID, checkIn, checkOut, nil
}

func extractRoomIDFromMap(document bson.M) (primitive.ObjectID, error) {
	roomRaw, roomFound := document["roomId"]
	if !roomFound {
		roomRaw, roomFound = document["room_id"]
	}
	if !roomFound {
		roomRaw, roomFound = document["hotelId"]
	}
	if !roomFound {
		return primitive.NilObjectID, fmt.Errorf("%w: room id is required", ErrInvalidBookingPayload)
	}

	switch typed := roomRaw.(type) {
	case primitive.ObjectID:
		return typed, nil
	case string:
		parsed, err := primitive.ObjectIDFromHex(strings.TrimSpace(typed))
		if err != nil {
			return primitive.NilObjectID, fmt.Errorf("%w: invalid room id", ErrInvalidBookingPayload)
		}
		return parsed, nil
	default:
		text := strings.TrimSpace(fmt.Sprint(roomRaw))
		parsed, err := primitive.ObjectIDFromHex(text)
		if err != nil {
			return primitive.NilObjectID, fmt.Errorf("%w: invalid room id", ErrInvalidBookingPayload)
		}
		return parsed, nil
	}
}

func parseBookingDateRange(checkInText, checkOutText string) (time.Time, time.Time, error) {
	checkInText = strings.TrimSpace(checkInText)
	checkOutText = strings.TrimSpace(checkOutText)
	if checkInText == "" || checkOutText == "" {
		return time.Time{}, time.Time{}, errors.New("check-in and check-out are required")
	}

	checkIn, err := time.ParseInLocation("2006-01-02", checkInText, time.Local)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("invalid check-in date")
	}

	checkOut, err := time.ParseInLocation("2006-01-02", checkOutText, time.Local)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("invalid check-out date")
	}

	if !checkOut.After(checkIn) {
		return time.Time{}, time.Time{}, errors.New("check-out must be after check-in")
	}

	return toLocalDate(checkIn), toLocalDate(checkOut), nil
}

func parseBookingDateValue(value any) (time.Time, error) {
	switch typed := value.(type) {
	case time.Time:
		return toLocalDate(typed), nil
	case primitive.DateTime:
		return toLocalDate(typed.Time()), nil
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return time.Time{}, errors.New("empty date")
		}

		date, err := time.ParseInLocation("2006-01-02", text, time.Local)
		if err == nil {
			return toLocalDate(date), nil
		}

		parsedRFC, rfcErr := time.Parse(time.RFC3339, text)
		if rfcErr == nil {
			return toLocalDate(parsedRFC), nil
		}
		return time.Time{}, errors.New("invalid date value")
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" || text == "<nil>" {
			return time.Time{}, errors.New("empty date")
		}

		date, err := time.ParseInLocation("2006-01-02", text, time.Local)
		if err == nil {
			return toLocalDate(date), nil
		}

		parsedRFC, rfcErr := time.Parse(time.RFC3339, text)
		if rfcErr == nil {
			return toLocalDate(parsedRFC), nil
		}
		return time.Time{}, errors.New("invalid date value")
	}
}

func toLocalDate(date time.Time) time.Time {
	local := date.In(time.Local)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
}

func isCancelledStatus(value any) bool {
	status := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
	return status == "cancelled" || status == "canceled"
}

func buildDateSlots(checkInText, checkOutText string) ([]string, error) {
	checkIn, checkOut, err := parseBookingDateRange(checkInText, checkOutText)
	if err != nil {
		return nil, err
	}

	days := make([]string, 0)
	for day := checkIn; day.Before(checkOut); day = day.AddDate(0, 0, 1) {
		days = append(days, day.Format("2006-01-02"))
	}

	return days, nil
}

func isBeforeToday(date time.Time) bool {
	now := time.Now().In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	current := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.Local)
	return current.Before(today)
}

func hasAnyKey(document bson.M, keys ...string) bool {
	for _, key := range keys {
		if _, ok := document[key]; ok {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// FindBookingByGroupIDAndUserID finds active booking by groupId + userId
func (s *Store) FindBookingByGroupIDAndUserID(
	ctx context.Context,
	groupIDText string,
	userIDText string,
) (bson.M, error) {
	groupID, err := primitive.ObjectIDFromHex(strings.TrimSpace(groupIDText))
	if err != nil {
		return nil, nil
	}
	userID, err := primitive.ObjectIDFromHex(strings.TrimSpace(userIDText))
	if err != nil {
		return nil, nil
	}

	var result bson.M
	err = s.collection(bookingsCollection).FindOne(ctx, bson.M{
		"groupId": groupID,
		"userId":  userID,
		"status":  bson.M{"$ne": "canceled"},
	}).Decode(&result)

	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}
