package models

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"easybook/internal/db"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func TestAcquireHotelPresenceConcurrentCapacity(t *testing.T) {
	store, cleanup := setupPresenceIntegrationTest(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	hotelID := primitive.NewObjectID().Hex()
	ttl := 60 * time.Second
	capacity := 1

	var (
		successCount int
		deniedCount  int
		mutex        sync.Mutex
		waitGroup    sync.WaitGroup
		startGate    = make(chan struct{})
	)

	for i := 0; i < 3; i++ {
		waitGroup.Add(1)
		token := primitive.NewObjectID().Hex()

		go func(presenceToken string) {
			defer waitGroup.Done()
			<-startGate

			acquired, _, err := store.AcquireHotelPresence(ctx, hotelID, presenceToken, "", ttl, capacity)
			if err != nil {
				t.Errorf("acquire presence failed: %v", err)
				return
			}

			mutex.Lock()
			defer mutex.Unlock()
			if acquired {
				successCount++
			} else {
				deniedCount++
			}
		}(token)
	}

	close(startGate)
	waitGroup.Wait()

	if successCount != 1 {
		t.Fatalf("expected 1 successful slot acquisition, got %d", successCount)
	}
	if deniedCount != 2 {
		t.Fatalf("expected 2 denied slot acquisitions, got %d", deniedCount)
	}

	status, err := store.GetHotelPresenceStatus(ctx, hotelID, capacity)
	if err != nil {
		t.Fatalf("get presence status: %v", err)
	}
	if status.Active != 1 {
		t.Fatalf("expected 1 active slot, got %d", status.Active)
	}
	if status.CanEnter {
		t.Fatalf("expected can_enter=false when capacity reached")
	}
}

func TestAcquireHotelPresenceAfterTTLExpiry(t *testing.T) {
	store, cleanup := setupPresenceIntegrationTest(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	hotelID := primitive.NewObjectID().Hex()
	ttl := 1 * time.Second
	capacity := 1
	token1 := primitive.NewObjectID().Hex()
	token2 := primitive.NewObjectID().Hex()

	acquired, _, err := store.AcquireHotelPresence(ctx, hotelID, token1, "", ttl, capacity)
	if err != nil {
		t.Fatalf("acquire first slot: %v", err)
	}
	if !acquired {
		t.Fatalf("expected first slot to be acquired")
	}

	acquired, _, err = store.AcquireHotelPresence(ctx, hotelID, token2, "", ttl, capacity)
	if err != nil {
		t.Fatalf("acquire second token before ttl: %v", err)
	}
	if acquired {
		t.Fatalf("expected second token acquisition to fail before ttl expiry")
	}

	time.Sleep(1300 * time.Millisecond)

	acquired, _, err = store.AcquireHotelPresence(ctx, hotelID, token2, "", ttl, capacity)
	if err != nil {
		t.Fatalf("acquire second token after ttl: %v", err)
	}
	if !acquired {
		t.Fatalf("expected second token acquisition to succeed after ttl expiry")
	}
}

func TestHeartbeatHotelPresenceExtendsTTL(t *testing.T) {
	store, cleanup := setupPresenceIntegrationTest(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	hotelID := primitive.NewObjectID().Hex()
	ttl := 2 * time.Second
	capacity := 1
	token1 := primitive.NewObjectID().Hex()
	token2 := primitive.NewObjectID().Hex()

	acquired, _, err := store.AcquireHotelPresence(ctx, hotelID, token1, "", ttl, capacity)
	if err != nil {
		t.Fatalf("acquire initial slot: %v", err)
	}
	if !acquired {
		t.Fatalf("expected initial slot to be acquired")
	}

	time.Sleep(1100 * time.Millisecond)
	alive, err := store.HeartbeatHotelPresence(ctx, hotelID, token1, "", ttl)
	if err != nil {
		t.Fatalf("heartbeat failed: %v", err)
	}
	if !alive {
		t.Fatalf("expected heartbeat to keep slot alive")
	}

	time.Sleep(1100 * time.Millisecond)
	acquired, _, err = store.AcquireHotelPresence(ctx, hotelID, token2, "", ttl, capacity)
	if err != nil {
		t.Fatalf("acquire second token while heartbeat active: %v", err)
	}
	if acquired {
		t.Fatalf("expected second token to be denied while heartbeat extends ttl")
	}

	time.Sleep(1200 * time.Millisecond)
	acquired, _, err = store.AcquireHotelPresence(ctx, hotelID, token2, "", ttl, capacity)
	if err != nil {
		t.Fatalf("acquire second token after extended ttl expiry: %v", err)
	}
	if !acquired {
		t.Fatalf("expected second token to acquire slot after extended ttl expiry")
	}
}

func setupPresenceIntegrationTest(t *testing.T) (*Store, func()) {
	t.Helper()

	mongoURI := strings.TrimSpace(os.Getenv("MONGO_URI"))
	if mongoURI == "" {
		t.Skip("MONGO_URI is not set; skipping integration test")
	}

	dbName := "easybook_presence_test_" + primitive.NewObjectID().Hex()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		cancel()
		t.Fatalf("connect mongo: %v", err)
	}

	database := client.Database(dbName)
	if err := db.EnsureStartupMaintenance(ctx, database); err != nil {
		cancel()
		_ = client.Disconnect(context.Background())
		t.Fatalf("ensure indexes: %v", err)
	}

	cleanup := func() {
		_ = database.Drop(context.Background())
		_ = client.Disconnect(context.Background())
		cancel()
	}

	return NewStore(database), cleanup
}
