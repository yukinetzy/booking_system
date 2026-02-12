package models

import (
	"errors"
	"strings"

	"go.mongodb.org/mongo-driver/mongo"
)

var (
	ErrBookingConflict            = errors.New("booking conflict")
	ErrInvalidBookingPayload      = errors.New("invalid booking payload")
	ErrDuplicateWaitlist          = errors.New("duplicate waitlist subscription")
	ErrInvalidWaitlistPayload     = errors.New("invalid waitlist payload")
	ErrNotificationNotFound       = errors.New("notification not found")
	ErrUnauthorizedNotificationOp = errors.New("unauthorized notification operation")
	ErrInvalidPresencePayload     = errors.New("invalid presence payload")
	ErrPriorityAlreadyTaken       = errors.New("priority waitlist already taken")
)

func IsDuplicateKeyError(err error, key string) bool {
	if !mongo.IsDuplicateKeyError(err) {
		return false
	}
	if key == "" {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), strings.ToLower(key))
}
