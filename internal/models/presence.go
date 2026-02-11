package models

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const hotelPresenceCollection = "hotel_presence"

type HotelPresenceStatus struct {
	Active   int64
	Capacity int
	CanEnter bool
}

func (s *Store) AcquireHotelPresence(
	ctx context.Context,
	hotelIDText string,
	token string,
	userIDText string,
	ttl time.Duration,
	capacity int,
) (bool, int, error) {
	hotelID, err := parsePresenceHotelID(hotelIDText)
	if err != nil {
		return false, 0, err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return false, 0, fmt.Errorf("%w: token is required", ErrInvalidPresencePayload)
	}

	ttl = normalizePresenceTTL(ttl)
	capacity = normalizePresenceCapacity(capacity)
	now := time.Now().UTC()
	expiresAt := now.Add(ttl)
	userIDText = strings.TrimSpace(userIDText)
	refreshFields := bson.M{
		"expiresAt": expiresAt,
		"updatedAt": now,
		"userId":    userIDText,
	}

	// Reuse the current slot if the same token already has one for this hotel.
	for slot := 1; slot <= capacity; slot++ {
		updateResult, updateErr := s.collection(hotelPresenceCollection).UpdateOne(
			ctx,
			bson.M{"hotelId": hotelID, "slot": slot, "token": token},
			bson.M{"$set": refreshFields},
		)
		if updateErr != nil {
			return false, 0, updateErr
		}
		if updateResult.MatchedCount > 0 {
			return true, slot, nil
		}
	}

	for slot := 1; slot <= capacity; slot++ {
		filter := bson.M{
			"hotelId": hotelID,
			"slot":    slot,
			"$or": bson.A{
				bson.M{"token": token},
				bson.M{"expiresAt": bson.M{"$lte": now}},
				bson.M{"expiresAt": bson.M{"$exists": false}},
			},
		}
		update := bson.M{
			"$set": bson.M{
				"hotelId":   hotelID,
				"slot":      slot,
				"token":     token,
				"userId":    userIDText,
				"expiresAt": expiresAt,
				"updatedAt": now,
			},
			"$setOnInsert": bson.M{
				"createdAt": now,
			},
		}
		opts := options.FindOneAndUpdate().
			SetUpsert(true).
			SetReturnDocument(options.After)

		var doc bson.M
		updateErr := s.collection(hotelPresenceCollection).FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc)
		if updateErr == nil {
			currentToken := strings.TrimSpace(fmt.Sprint(doc["token"]))
			if currentToken == token {
				return true, slot, nil
			}
			continue
		}

		if errors.Is(updateErr, mongo.ErrNoDocuments) {
			continue
		}
		if IsDuplicateKeyError(updateErr, "slot") || IsDuplicateKeyError(updateErr, "hotelId") {
			recheckResult, recheckErr := s.collection(hotelPresenceCollection).UpdateOne(
				ctx,
				bson.M{"hotelId": hotelID, "slot": slot, "token": token},
				bson.M{"$set": refreshFields},
			)
			if recheckErr != nil {
				return false, 0, recheckErr
			}
			if recheckResult.MatchedCount > 0 {
				return true, slot, nil
			}
			continue
		}
		return false, 0, updateErr
	}

	return false, 0, nil
}

func (s *Store) HeartbeatHotelPresence(
	ctx context.Context,
	hotelIDText string,
	token string,
	userIDText string,
	ttl time.Duration,
) (bool, error) {
	hotelID, err := parsePresenceHotelID(hotelIDText)
	if err != nil {
		return false, err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return false, fmt.Errorf("%w: token is required", ErrInvalidPresencePayload)
	}

	ttl = normalizePresenceTTL(ttl)
	now := time.Now().UTC()
	expiresAt := now.Add(ttl)
	userIDText = strings.TrimSpace(userIDText)

	result, err := s.collection(hotelPresenceCollection).UpdateOne(
		ctx,
		bson.M{
			"hotelId":   hotelID,
			"token":     token,
			"expiresAt": bson.M{"$gt": now},
		},
		bson.M{"$set": bson.M{
			"expiresAt": expiresAt,
			"updatedAt": now,
			"userId":    userIDText,
		}},
	)
	if err != nil {
		return false, err
	}

	return result.MatchedCount > 0, nil
}

func (s *Store) GetHotelPresenceStatus(ctx context.Context, hotelIDText string, capacity int) (HotelPresenceStatus, error) {
	hotelID, err := parsePresenceHotelID(hotelIDText)
	if err != nil {
		return HotelPresenceStatus{}, err
	}

	capacity = normalizePresenceCapacity(capacity)
	now := time.Now().UTC()
	activeCount, err := s.collection(hotelPresenceCollection).CountDocuments(
		ctx,
		bson.M{
			"hotelId":   hotelID,
			"expiresAt": bson.M{"$gt": now},
			"slot":      bson.M{"$gte": 1, "$lte": capacity},
		},
	)
	if err != nil {
		return HotelPresenceStatus{}, err
	}

	return HotelPresenceStatus{
		Active:   activeCount,
		Capacity: capacity,
		CanEnter: activeCount < int64(capacity),
	}, nil
}

func parsePresenceHotelID(hotelIDText string) (primitive.ObjectID, error) {
	hotelID, err := primitive.ObjectIDFromHex(strings.TrimSpace(hotelIDText))
	if err != nil {
		return primitive.NilObjectID, fmt.Errorf("%w: invalid hotel id", ErrInvalidPresencePayload)
	}
	return hotelID, nil
}

func normalizePresenceCapacity(capacity int) int {
	if capacity <= 0 {
		return 1
	}
	if capacity > 20 {
		return 20
	}
	return capacity
}

func normalizePresenceTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return 60 * time.Second
	}
	return ttl
}
