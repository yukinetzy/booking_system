package models

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"_id"`
	Email        string             `bson:"email" json:"email"`
	PasswordHash string             `bson:"passwordHash" json:"passwordHash,omitempty"`
	Role         string             `bson:"role" json:"role"`
	CreatedAt    time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt    time.Time          `bson:"updatedAt" json:"updatedAt"`
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (s *Store) FindUserByEmail(ctx context.Context, email string) (*User, error) {
	cleanEmail := normalizeEmail(email)
	if cleanEmail == "" {
		return nil, nil
	}

	var user User
	err := s.collection("users").FindOne(ctx, bson.M{"email": cleanEmail}).Decode(&user)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &user, nil
}

func (s *Store) CreateUser(ctx context.Context, email, password, role string) (string, error) {
	cleanEmail := normalizeEmail(email)
	if cleanEmail == "" {
		return "", errors.New("email is required")
	}
	if role == "" {
		role = "user"
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	result, err := s.collection("users").InsertOne(ctx, bson.M{
		"email":        cleanEmail,
		"passwordHash": string(passwordHash),
		"role":         role,
		"createdAt":    now,
		"updatedAt":    now,
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
