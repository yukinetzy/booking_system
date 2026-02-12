package models

import (
	"go.mongodb.org/mongo-driver/mongo"
)

type Store struct {
	db *mongo.Database
}

func NewStore(db *mongo.Database) *Store {
	return &Store{db: db}
}

func (s *Store) collection(name string) *mongo.Collection {
	return s.db.Collection(name)
}

const (
	WaitlistMain     = "main"
	WaitlistPriority = "priority"
)
