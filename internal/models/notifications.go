package models

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	waitlistCollection      = "waitlist"
	notificationsCollection = "notifications"
)

func (s *Store) SubscribeToWaitlist(
	ctx context.Context,
	userIDText,
	roomIDText,
	checkIn,
	checkOut,
	waitlistType string,
) (string, string, error) {
	waitlistType = strings.ToLower(strings.TrimSpace(waitlistType))
	if waitlistType == "" {
		waitlistType = WaitlistMain
	}
	if waitlistType != WaitlistMain && waitlistType != WaitlistPriority {
		return "", "", ErrInvalidWaitlistPayload
	}

	userID, err := primitive.ObjectIDFromHex(strings.TrimSpace(userIDText))
	if err != nil {
		return "", "", fmt.Errorf("%w: invalid user id", ErrInvalidWaitlistPayload)
	}

	roomID, err := primitive.ObjectIDFromHex(strings.TrimSpace(roomIDText))
	if err != nil {
		return "", "", fmt.Errorf("%w: invalid room id", ErrInvalidWaitlistPayload)
	}

	checkIn = strings.TrimSpace(checkIn)
	checkOut = strings.TrimSpace(checkOut)
	checkInDate, _, err := parseBookingDateRange(checkIn, checkOut)
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrInvalidWaitlistPayload, err)
	}
	if isBeforeToday(checkInDate) {
		return "", "", fmt.Errorf("%w: cannot subscribe for past dates", ErrInvalidWaitlistPayload)
	}

	dupFilter := bson.M{
		"userId":   userID,
		"roomId":   roomID,
		"checkIn":  checkIn,
		"checkOut": checkOut,
		"type":     waitlistType,
		"isActive": true,
	}
	dupCount, err := s.collection(waitlistCollection).CountDocuments(ctx, dupFilter)
	if err != nil {
		return "", "", err
	}
	if dupCount > 0 {
		return "", "", ErrDuplicateWaitlist
	}

	if waitlistType == WaitlistPriority {
		takenCount, err := s.collection(waitlistCollection).CountDocuments(ctx, bson.M{
			"roomId":   roomID,
			"checkIn":  checkIn,
			"checkOut": checkOut,
			"type":     WaitlistPriority,
			"isActive": true,
		})
		if err != nil {
			return "", "", err
		}
		if takenCount > 0 {
			return "", "", ErrPriorityAlreadyTaken
		}
	}

	now := time.Now().UTC()

	var groupID primitive.ObjectID
	if waitlistType == WaitlistPriority {
		groupID = primitive.NewObjectID()
	}

	doc := bson.M{
		"userId":    userID,
		"roomId":    roomID,
		"checkIn":   checkIn,
		"checkOut":  checkOut,
		"type":      waitlistType,
		"isActive":  true,
		"createdAt": now,
		"updatedAt": now,
	}

	if waitlistType == WaitlistPriority {
		doc["groupId"] = groupID
	}

	result, err := s.collection(waitlistCollection).InsertOne(ctx, doc)

	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			if waitlistType == WaitlistPriority {
				return "", "", ErrPriorityAlreadyTaken
			}
			return "", "", ErrDuplicateWaitlist
		}
		return "", "", err
	}

	insertedID, ok := result.InsertedID.(primitive.ObjectID)
	if !ok {
		return "", "", fmt.Errorf("%w: invalid waitlist id", ErrInvalidWaitlistPayload)
	}

	gid := ""
	if waitlistType == WaitlistPriority {
		gid = groupID.Hex()
	}
	return insertedID.Hex(), gid, nil

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

	createdNotifications := int64(0)

	processType := func(waitlistType string, stopAfterFirst bool) (int64, error) {
		cursor, err := s.collection(waitlistCollection).Find(
			ctx,
			bson.M{"roomId": roomID, "isActive": true, "type": waitlistType},
			options.Find().SetSort(bson.D{
				{Key: "tyoe", Value: -1},
				{Key: "createdAt", Value: 1},
			}).SetLimit(300),
		)
		if err != nil {
			return createdNotifications, err
		}
		defer cursor.Close(ctx)

		subscriptions := make([]bson.M, 0)
		if err := cursor.All(ctx, &subscriptions); err != nil {
			return createdNotifications, err
		}

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

			link := "/bookings/new?hotelId=" + roomID.Hex() +
				"&checkIn=" + url.QueryEscape(checkIn) +
				"&checkOut=" + url.QueryEscape(checkOut)

			notificationDoc := bson.M{
				"userId":    userID,
				"title":     "Room is available now",
				"text":      fmt.Sprintf("Room is now available for %s to %s.", checkIn, checkOut),
				"link":      link,
				"isRead":    false,
				"createdAt": time.Now().UTC(),
			}

			if gid, ok := subscription["groupId"].(primitive.ObjectID); ok {
				notificationDoc["groupId"] = gid
			}

			_, notificationErr := s.collection(notificationsCollection).InsertOne(ctx, notificationDoc)

			if notificationErr != nil {

				_, _ = s.collection(waitlistCollection).UpdateOne(
					ctx,
					bson.M{"_id": subscriptionID},
					bson.M{"$set": bson.M{"isActive": true, "updatedAt": time.Now().UTC()}},
				)
				return createdNotifications, notificationErr
			}

			createdNotifications++

			if stopAfterFirst {
				return createdNotifications, nil
			}
		}

		return createdNotifications, nil
	}

	if _, err := processType(WaitlistPriority, true); err != nil {
		return createdNotifications, err
	}
	if createdNotifications > 0 {
		return createdNotifications, nil
	}

	_, err = processType(WaitlistMain, false)
	return createdNotifications, err
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
