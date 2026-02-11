package models

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	waitlistCollection      = "waitlist"
	notificationsCollection = "notifications"
)

func (s *Store) SubscribeToWaitlist(ctx context.Context, userIDText, roomIDText, checkIn, checkOut string) (string, error) {
	userID, err := primitive.ObjectIDFromHex(strings.TrimSpace(userIDText))
	if err != nil {
		return "", fmt.Errorf("%w: invalid user id", ErrInvalidWaitlistPayload)
	}

	roomID, err := primitive.ObjectIDFromHex(strings.TrimSpace(roomIDText))
	if err != nil {
		return "", fmt.Errorf("%w: invalid room id", ErrInvalidWaitlistPayload)
	}

	checkIn = strings.TrimSpace(checkIn)
	checkOut = strings.TrimSpace(checkOut)
	checkInDate, _, err := parseBookingDateRange(checkIn, checkOut)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidWaitlistPayload, err)
	}
	if isBeforeToday(checkInDate) {
		return "", fmt.Errorf("%w: cannot subscribe for past dates", ErrInvalidWaitlistPayload)
	}

	now := time.Now().UTC()
	filter := bson.M{
		"userId":   userID,
		"roomId":   roomID,
		"checkIn":  checkIn,
		"checkOut": checkOut,
		"isActive": true,
	}

	existingCount, err := s.collection(waitlistCollection).CountDocuments(ctx, filter)
	if err != nil {
		return "", err
	}
	if existingCount > 0 {
		return "", ErrDuplicateWaitlist
	}

	result, err := s.collection(waitlistCollection).InsertOne(ctx, bson.M{
		"userId":    userID,
		"roomId":    roomID,
		"checkIn":   checkIn,
		"checkOut":  checkOut,
		"isActive":  true,
		"createdAt": now,
		"updatedAt": now,
	})
	if err != nil {
		if IsDuplicateKeyError(err, "roomId") {
			return "", ErrDuplicateWaitlist
		}
		return "", err
	}

	insertedID, ok := result.InsertedID.(primitive.ObjectID)
	if !ok {
		return "", fmt.Errorf("%w: invalid waitlist id", ErrInvalidWaitlistPayload)
	}

	return insertedID.Hex(), nil
}

func (s *Store) ProcessWaitlistForRoom(ctx context.Context, roomIDText string) (int64, error) {
	roomIDText = strings.TrimSpace(roomIDText)
	if roomIDText == "" {
		return 0, nil
	}

	roomID, err := primitive.ObjectIDFromHex(roomIDText)
	if err != nil {
		return 0, nil
	}

	cursor, err := s.collection(waitlistCollection).Find(
		ctx,
		bson.M{"roomId": roomID, "isActive": true},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}}).SetLimit(300),
	)
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)

	subscriptions := make([]bson.M, 0)
	if err := cursor.All(ctx, &subscriptions); err != nil {
		return 0, err
	}

	createdNotifications := int64(0)
	for _, subscription := range subscriptions {
		subscriptionID, ok := subscription["_id"].(primitive.ObjectID)
		if !ok {
			continue
		}
		userID, ok := subscription["userId"].(primitive.ObjectID)
		if !ok {
			continue
		}

		checkIn := strings.TrimSpace(fmt.Sprint(subscription["checkIn"]))
		checkOut := strings.TrimSpace(fmt.Sprint(subscription["checkOut"]))
		if _, _, rangeErr := parseBookingDateRange(checkIn, checkOut); rangeErr != nil {
			continue
		}

		conflict, conflictErr := s.hasBookingConflict(ctx, roomID, checkIn, checkOut, nil)
		if conflictErr != nil {
			return createdNotifications, conflictErr
		}
		if conflict {
			continue
		}

		deactivateResult, deactivateErr := s.collection(waitlistCollection).UpdateOne(
			ctx,
			bson.M{"_id": subscriptionID, "isActive": true},
			bson.M{"$set": bson.M{"isActive": false, "updatedAt": time.Now().UTC()}},
		)
		if deactivateErr != nil {
			return createdNotifications, deactivateErr
		}
		if deactivateResult.ModifiedCount == 0 {
			continue
		}

		link := "/bookings/new?hotelId=" + roomID.Hex() + "&checkIn=" + url.QueryEscape(checkIn) + "&checkOut=" + url.QueryEscape(checkOut)
		_, notificationErr := s.collection(notificationsCollection).InsertOne(ctx, bson.M{
			"userId":    userID,
			"title":     "Room is available now",
			"text":      fmt.Sprintf("Room is now available for %s to %s.", checkIn, checkOut),
			"link":      link,
			"isRead":    false,
			"createdAt": time.Now().UTC(),
		})
		if notificationErr != nil {
			_, _ = s.collection(waitlistCollection).UpdateOne(
				ctx,
				bson.M{"_id": subscriptionID},
				bson.M{"$set": bson.M{"isActive": true, "updatedAt": time.Now().UTC()}},
			)
			return createdNotifications, notificationErr
		}

		createdNotifications++
	}

	return createdNotifications, nil
}

func (s *Store) ListNotifications(ctx context.Context, userIDText string, limit int64) ([]bson.M, int64, error) {
	userID, err := primitive.ObjectIDFromHex(strings.TrimSpace(userIDText))
	if err != nil {
		return nil, 0, fmt.Errorf("%w: invalid user id", ErrUnauthorizedNotificationOp)
	}

	if limit <= 0 || limit > 200 {
		limit = 100
	}

	cursor, err := s.collection(notificationsCollection).Find(
		ctx,
		bson.M{"userId": userID},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}).SetLimit(limit),
	)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	items := make([]bson.M, 0)
	if err := cursor.All(ctx, &items); err != nil {
		return nil, 0, err
	}

	unreadCount, err := s.collection(notificationsCollection).CountDocuments(ctx, bson.M{
		"userId": userID,
		"isRead": false,
	})
	if err != nil {
		return nil, 0, err
	}

	return items, unreadCount, nil
}

func (s *Store) MarkNotificationRead(ctx context.Context, userIDText, notificationIDText string) (bool, error) {
	userID, err := primitive.ObjectIDFromHex(strings.TrimSpace(userIDText))
	if err != nil {
		return false, fmt.Errorf("%w: invalid user id", ErrUnauthorizedNotificationOp)
	}
	notificationID, err := primitive.ObjectIDFromHex(strings.TrimSpace(notificationIDText))
	if err != nil {
		return false, ErrNotificationNotFound
	}

	result, err := s.collection(notificationsCollection).UpdateOne(
		ctx,
		bson.M{"_id": notificationID, "userId": userID},
		bson.M{"$set": bson.M{
			"isRead": true,
			"readAt": time.Now().UTC(),
		}},
	)
	if err != nil {
		return false, err
	}
	if result.MatchedCount == 0 {
		return false, ErrNotificationNotFound
	}

	return true, nil
}

func (s *Store) MarkAllNotificationsRead(ctx context.Context, userIDText string) (int64, error) {
	userID, err := primitive.ObjectIDFromHex(strings.TrimSpace(userIDText))
	if err != nil {
		return 0, fmt.Errorf("%w: invalid user id", ErrUnauthorizedNotificationOp)
	}

	result, err := s.collection(notificationsCollection).UpdateMany(
		ctx,
		bson.M{"userId": userID, "isRead": false},
		bson.M{"$set": bson.M{"isRead": true, "readAt": time.Now().UTC()}},
	)
	if err != nil {
		return 0, err
	}

	return result.ModifiedCount, nil
}
