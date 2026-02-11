package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func EnsureStartupMaintenance(ctx context.Context, database *mongo.Database) error {
	if err := dropLegacyUsernameIndexes(ctx, database); err != nil {
		return err
	}
	if err := backfillBookingRoomIDs(ctx, database); err != nil {
		return err
	}

	indexTasks := []struct {
		collection string
		model      mongo.IndexModel
	}{
		{
			collection: "users",
			model: mongo.IndexModel{
				Keys:    bson.D{{Key: "email", Value: 1}},
				Options: options.Index().SetUnique(true).SetSparse(true),
			},
		},
		{collection: "hotels", model: mongo.IndexModel{Keys: bson.D{{Key: "location", Value: 1}}}},
		{collection: "hotels", model: mongo.IndexModel{Keys: bson.D{{Key: "price_per_night", Value: 1}}}},
		{collection: "contact_requests", model: mongo.IndexModel{Keys: bson.D{{Key: "createdAt", Value: -1}}}},
		{collection: "bookings", model: mongo.IndexModel{Keys: bson.D{{Key: "userId", Value: 1}, {Key: "createdAt", Value: -1}}}},
		{collection: "bookings", model: mongo.IndexModel{Keys: bson.D{{Key: "roomId", Value: 1}, {Key: "checkIn", Value: 1}, {Key: "checkOut", Value: 1}}}},
		{
			collection: "room_calendar",
			model: mongo.IndexModel{
				Keys:    bson.D{{Key: "roomId", Value: 1}, {Key: "day", Value: 1}},
				Options: options.Index().SetUnique(true),
			},
		},
		{
			collection: "waitlist",
			model: mongo.IndexModel{
				Keys: bson.D{
					{Key: "userId", Value: 1},
					{Key: "roomId", Value: 1},
					{Key: "checkIn", Value: 1},
					{Key: "checkOut", Value: 1},
					{Key: "isActive", Value: 1},
				},
				Options: options.Index().
					SetUnique(true).
					SetPartialFilterExpression(bson.M{"isActive": true}),
			},
		},
		{
			collection: "waitlist",
			model: mongo.IndexModel{
				Keys: bson.D{
					{Key: "roomId", Value: 1},
					{Key: "isActive", Value: 1},
					{Key: "createdAt", Value: 1},
				},
			},
		},
		{
			collection: "notifications",
			model: mongo.IndexModel{
				Keys: bson.D{
					{Key: "userId", Value: 1},
					{Key: "createdAt", Value: -1},
				},
			},
		},
		{
			collection: "notifications",
			model: mongo.IndexModel{
				Keys: bson.D{
					{Key: "userId", Value: 1},
					{Key: "isRead", Value: 1},
				},
			},
		},
		{
			collection: "hotel_presence",
			model: mongo.IndexModel{
				Keys: bson.D{
					{Key: "hotelId", Value: 1},
					{Key: "slot", Value: 1},
				},
				Options: options.Index().SetUnique(true),
			},
		},
		{
			collection: "hotel_presence",
			model: mongo.IndexModel{
				Keys: bson.D{
					{Key: "hotelId", Value: 1},
					{Key: "token", Value: 1},
				},
			},
		},
		{
			collection: "hotel_presence",
			model: mongo.IndexModel{
				Keys: bson.D{
					{Key: "hotelId", Value: 1},
					{Key: "expiresAt", Value: 1},
				},
			},
		},
		{
			collection: "hotel_presence",
			model: mongo.IndexModel{
				Keys:    bson.D{{Key: "expiresAt", Value: 1}},
				Options: options.Index().SetExpireAfterSeconds(0),
			},
		},
	}

	for _, task := range indexTasks {
		if _, err := database.Collection(task.collection).Indexes().CreateOne(ctx, task.model); err != nil {
			return fmt.Errorf("create index on %s: %w", task.collection, err)
		}
	}
	if err := syncRoomCalendarFromActiveBookings(ctx, database); err != nil {
		return err
	}

	return nil
}

func dropLegacyUsernameIndexes(ctx context.Context, database *mongo.Database) error {
	usersCollection := database.Collection("users")
	cursor, err := usersCollection.Indexes().List(ctx)
	if err != nil {
		return fmt.Errorf("list users indexes: %w", err)
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var index bson.M
		if err := cursor.Decode(&index); err != nil {
			return fmt.Errorf("decode users index: %w", err)
		}

		name, _ := index["name"].(string)
		if name == "_id_" {
			continue
		}

		keys, _ := index["key"].(bson.M)
		if keys == nil {
			continue
		}

		if _, hasLegacyField := keys["username"]; hasLegacyField {
			if _, err := usersCollection.Indexes().DropOne(ctx, name); err != nil {
				return fmt.Errorf("drop legacy users index %s: %w", name, err)
			}
		}
	}

	if err := cursor.Err(); err != nil {
		return fmt.Errorf("iterate users indexes: %w", err)
	}

	return nil
}

func backfillBookingRoomIDs(ctx context.Context, database *mongo.Database) error {
	bookingsCollection := database.Collection("bookings")
	cursor, err := bookingsCollection.Find(
		ctx,
		bson.M{"roomId": bson.M{"$exists": false}, "hotelId": bson.M{"$exists": true}},
		options.Find().SetProjection(bson.M{"_id": 1, "hotelId": 1}),
	)
	if err != nil {
		return fmt.Errorf("find bookings without roomId: %w", err)
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var booking bson.M
		if err := cursor.Decode(&booking); err != nil {
			return fmt.Errorf("decode booking without roomId: %w", err)
		}

		bookingID, ok := booking["_id"].(primitive.ObjectID)
		if !ok {
			continue
		}

		roomID, ok := objectIDFromAny(booking["hotelId"])
		if !ok {
			continue
		}

		if _, err := bookingsCollection.UpdateOne(
			ctx,
			bson.M{"_id": bookingID, "roomId": bson.M{"$exists": false}},
			bson.M{"$set": bson.M{"roomId": roomID}},
		); err != nil {
			return fmt.Errorf("backfill roomId for booking %s: %w", bookingID.Hex(), err)
		}
	}
	if err := cursor.Err(); err != nil {
		return fmt.Errorf("iterate bookings without roomId: %w", err)
	}

	return nil
}

func syncRoomCalendarFromActiveBookings(ctx context.Context, database *mongo.Database) error {
	bookingsCollection := database.Collection("bookings")
	roomCalendar := database.Collection("room_calendar")

	cursor, err := bookingsCollection.Find(
		ctx,
		bson.M{"status": bson.M{"$ne": "cancelled"}},
		options.Find().SetProjection(bson.M{
			"_id":       1,
			"roomId":    1,
			"hotelId":   1,
			"status":    1,
			"checkIn":   1,
			"checkOut":  1,
			"check_in":  1,
			"check_out": 1,
		}),
	)
	if err != nil {
		return fmt.Errorf("find active bookings for room calendar sync: %w", err)
	}
	defer cursor.Close(ctx)

	now := time.Now().UTC()
	for cursor.Next(ctx) {
		var booking bson.M
		if err := cursor.Decode(&booking); err != nil {
			return fmt.Errorf("decode booking for room calendar sync: %w", err)
		}

		if isCancelledStatus(booking["status"]) {
			continue
		}

		bookingID, ok := booking["_id"].(primitive.ObjectID)
		if !ok {
			continue
		}

		roomID, ok := resolveRoomID(booking)
		if !ok {
			continue
		}

		checkInValue, hasCheckIn := booking["checkIn"]
		if !hasCheckIn {
			checkInValue, hasCheckIn = booking["check_in"]
		}
		checkOutValue, hasCheckOut := booking["checkOut"]
		if !hasCheckOut {
			checkOutValue, hasCheckOut = booking["check_out"]
		}
		if !hasCheckIn || !hasCheckOut {
			continue
		}

		checkInDate, checkInErr := parseDateAny(checkInValue)
		if checkInErr != nil {
			continue
		}
		checkOutDate, checkOutErr := parseDateAny(checkOutValue)
		if checkOutErr != nil {
			continue
		}
		if !checkOutDate.After(checkInDate) {
			continue
		}

		for day := checkInDate; day.Before(checkOutDate); day = day.AddDate(0, 0, 1) {
			dayText := day.Format("2006-01-02")
			_, err := roomCalendar.UpdateOne(
				ctx,
				bson.M{"roomId": roomID, "day": dayText},
				bson.M{"$setOnInsert": bson.M{
					"roomId":    roomID,
					"day":       dayText,
					"bookingId": bookingID,
					"createdAt": now,
				}},
				options.Update().SetUpsert(true),
			)
			if err != nil {
				return fmt.Errorf("sync room_calendar day %s for booking %s: %w", dayText, bookingID.Hex(), err)
			}
		}
	}
	if err := cursor.Err(); err != nil {
		return fmt.Errorf("iterate active bookings for room calendar sync: %w", err)
	}

	return nil
}

func resolveRoomID(document bson.M) (primitive.ObjectID, bool) {
	if roomID, ok := objectIDFromAny(document["roomId"]); ok {
		return roomID, true
	}
	if hotelID, ok := objectIDFromAny(document["hotelId"]); ok {
		return hotelID, true
	}
	return primitive.NilObjectID, false
}

func objectIDFromAny(value any) (primitive.ObjectID, bool) {
	switch typed := value.(type) {
	case primitive.ObjectID:
		return typed, true
	case string:
		parsed, err := primitive.ObjectIDFromHex(strings.TrimSpace(typed))
		if err != nil {
			return primitive.NilObjectID, false
		}
		return parsed, true
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" || text == "<nil>" {
			return primitive.NilObjectID, false
		}
		parsed, err := primitive.ObjectIDFromHex(text)
		if err != nil {
			return primitive.NilObjectID, false
		}
		return parsed, true
	}
}

func parseDateAny(value any) (time.Time, error) {
	switch typed := value.(type) {
	case time.Time:
		return normalizeDate(typed), nil
	case primitive.DateTime:
		return normalizeDate(typed.Time()), nil
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return time.Time{}, fmt.Errorf("empty date value")
		}
		date, err := time.ParseInLocation("2006-01-02", text, time.Local)
		if err == nil {
			return normalizeDate(date), nil
		}
		parsedRFC, parseErr := time.Parse(time.RFC3339, text)
		if parseErr == nil {
			return normalizeDate(parsedRFC), nil
		}
		return time.Time{}, fmt.Errorf("invalid date value")
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" || text == "<nil>" {
			return time.Time{}, fmt.Errorf("empty date value")
		}
		date, err := time.ParseInLocation("2006-01-02", text, time.Local)
		if err == nil {
			return normalizeDate(date), nil
		}
		parsedRFC, parseErr := time.Parse(time.RFC3339, text)
		if parseErr == nil {
			return normalizeDate(parsedRFC), nil
		}
		return time.Time{}, fmt.Errorf("invalid date value")
	}
}

func normalizeDate(date time.Time) time.Time {
	local := date.In(time.Local)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
}

func isCancelledStatus(value any) bool {
	status := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
	return status == "cancelled" || status == "canceled"
}
