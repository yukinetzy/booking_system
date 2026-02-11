package models

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"easybook/internal/db"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func TestCreateBookingConcurrentConflict(t *testing.T) {
	mongoURI := strings.TrimSpace(os.Getenv("MONGO_URI"))
	if mongoURI == "" {
		t.Skip("MONGO_URI is not set; skipping integration test")
	}

	dbName := "easybook_test_" + primitive.NewObjectID().Hex()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		t.Fatalf("connect mongo: %v", err)
	}
	defer func() {
		_ = client.Disconnect(context.Background())
	}()

	database := client.Database(dbName)
	defer func() {
		_ = database.Drop(context.Background())
	}()

	if err := ensureTransactionsSupported(ctx, client); err != nil {
		t.Skipf("transactions are not supported in this Mongo deployment: %v", err)
	}

	if err := db.EnsureStartupMaintenance(ctx, database); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}

	store := NewStore(database)
	roomID := primitive.NewObjectID()

	basePayload := bson.M{
		"roomId":   roomID,
		"hotelId":  roomID,
		"checkIn":  "2030-06-10",
		"checkOut": "2030-06-12",
		"guests":   2,
		"notes":    "parallel booking test",
	}

	var (
		successCount  int
		conflictCount int
		mutex         sync.Mutex
		waitGroup     sync.WaitGroup
		startGate     = make(chan struct{})
	)

	for i := 0; i < 2; i++ {
		waitGroup.Add(1)
		userID := primitive.NewObjectID().Hex()

		go func(uid string) {
			defer waitGroup.Done()
			<-startGate

			payload := bson.M{
				"roomId":   basePayload["roomId"],
				"hotelId":  basePayload["hotelId"],
				"checkIn":  basePayload["checkIn"],
				"checkOut": basePayload["checkOut"],
				"guests":   basePayload["guests"],
				"notes":    basePayload["notes"],
			}

			_, createErr := store.CreateBooking(ctx, payload, uid)

			mutex.Lock()
			defer mutex.Unlock()

			if createErr == nil {
				successCount++
				return
			}
			if errors.Is(createErr, ErrBookingConflict) {
				conflictCount++
				return
			}

			t.Errorf("unexpected error: %v", createErr)
		}(userID)
	}

	close(startGate)
	waitGroup.Wait()

	if successCount != 1 {
		t.Fatalf("expected exactly one successful booking, got %d", successCount)
	}
	if conflictCount != 1 {
		t.Fatalf("expected exactly one conflict booking, got %d", conflictCount)
	}
}

func ensureTransactionsSupported(ctx context.Context, client *mongo.Client) error {
	session, err := client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(txCtx mongo.SessionContext) (any, error) {
		return nil, nil
	})
	return err
}
