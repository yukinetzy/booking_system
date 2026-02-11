package models

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func (s *Store) CreateContactRequest(ctx context.Context, payload map[string]string) (string, error) {
	now := time.Now().UTC()

	result, err := s.collection("contact_requests").InsertOne(ctx, bson.M{
		"name":      payload["name"],
		"phone":     payload["phone"],
		"city":      payload["city"],
		"email":     payload["email"],
		"message":   payload["message"],
		"status":    "new",
		"createdAt": now,
		"updatedAt": now,
	})
	if err != nil {
		return "", err
	}

	insertedID, ok := result.InsertedID.(primitive.ObjectID)
	if !ok {
		return "", errors.New("invalid inserted id type")
	}

	return insertedID.Hex(), nil
}
