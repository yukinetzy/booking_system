package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"easybook/internal/config"
	"easybook/internal/db"
	"easybook/internal/models"
	"easybook/internal/session"
	"easybook/internal/view"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func TestCreateBookingAPIConcurrentConflict(t *testing.T) {
	mongoURI := strings.TrimSpace(os.Getenv("MONGO_URI"))
	if mongoURI == "" {
		t.Skip("MONGO_URI is not set; skipping integration test")
	}

	dbName := "easybook_handlers_test_" + primitive.NewObjectID().Hex()
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

	if err := ensureTransactionsSupportedForHandlers(ctx, client); err != nil {
		t.Skipf("transactions are not supported in this Mongo deployment: %v", err)
	}

	if err := db.EnsureStartupMaintenance(ctx, database); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}

	roomID := primitive.NewObjectID()
	userID := primitive.NewObjectID()
	_, err = database.Collection("hotels").InsertOne(ctx, bson.M{
		"_id":       roomID,
		"title":     "Test Room",
		"createdAt": time.Now().UTC(),
		"updatedAt": time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("insert test room: %v", err)
	}

	sessions, err := session.NewManager(ctx, database, false, "test-session-secret-123")
	if err != nil {
		t.Fatalf("init sessions: %v", err)
	}

	store := models.NewStore(database)
	env := config.Env{
		HotelsPageSize:   6,
		HotelsPageMax:    20,
		BookingsPageSize: 8,
		BookingsPageMax:  25,
	}
	app := NewApp(env, store, sessions, view.NewRenderer("../../views"), "../../views")
	server := httptest.NewServer(app.Router())
	defer server.Close()

	sessionCookie := createSessionCookieForTests(t, sessions, userID.Hex(), "race@example.com", "user")

	payload := map[string]any{
		"room_id":   roomID.Hex(),
		"check_in":  "2030-08-10",
		"check_out": "2030-08-12",
		"guests":    2,
		"notes":     "race condition check",
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	type result struct {
		statusCode int
		body       map[string]any
		err        error
	}

	results := make([]result, 0, 2)
	startGate := make(chan struct{})
	var (
		mutex     sync.Mutex
		waitGroup sync.WaitGroup
	)

	for i := 0; i < 2; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-startGate

			request, requestErr := http.NewRequest(http.MethodPost, server.URL+"/api/bookings", bytes.NewReader(bodyBytes))
			if requestErr != nil {
				mutex.Lock()
				results = append(results, result{err: requestErr})
				mutex.Unlock()
				return
			}
			request.Header.Set("Content-Type", "application/json")
			request.AddCookie(sessionCookie)

			response, responseErr := http.DefaultClient.Do(request)
			if responseErr != nil {
				mutex.Lock()
				results = append(results, result{err: responseErr})
				mutex.Unlock()
				return
			}
			defer response.Body.Close()

			responseBody := map[string]any{}
			_ = json.NewDecoder(response.Body).Decode(&responseBody)

			mutex.Lock()
			results = append(results, result{
				statusCode: response.StatusCode,
				body:       responseBody,
			})
			mutex.Unlock()
		}()
	}

	close(startGate)
	waitGroup.Wait()

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	successCount := 0
	conflictCount := 0
	for _, item := range results {
		if item.err != nil {
			t.Fatalf("request failed: %v", item.err)
		}

		switch item.statusCode {
		case http.StatusCreated:
			successCount++
		case http.StatusConflict:
			conflictCount++
			if fmt.Sprint(item.body["error"]) != "booking_conflict" {
				t.Fatalf("expected booking_conflict error payload, got %v", item.body)
			}
		default:
			t.Fatalf("unexpected status code %d with body %v", item.statusCode, item.body)
		}
	}

	if successCount != 1 {
		t.Fatalf("expected exactly one successful request, got %d", successCount)
	}
	if conflictCount != 1 {
		t.Fatalf("expected exactly one conflict request, got %d", conflictCount)
	}

	totalBookings, err := database.Collection("bookings").CountDocuments(ctx, bson.M{
		"$or": bson.A{
			bson.M{"roomId": roomID},
			bson.M{"hotelId": roomID},
		},
		"checkIn":  "2030-08-10",
		"checkOut": "2030-08-12",
	})
	if err != nil {
		t.Fatalf("count bookings: %v", err)
	}
	if totalBookings != 1 {
		t.Fatalf("expected exactly one booking persisted, got %d", totalBookings)
	}
}

func createSessionCookieForTests(t *testing.T, sessions *session.Manager, userID, email, role string) *http.Cookie {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()
	if err := sessions.StartSession(recorder, request, userID, email, role); err != nil {
		t.Fatalf("start session: %v", err)
	}

	response := recorder.Result()
	defer response.Body.Close()
	for _, cookie := range response.Cookies() {
		if cookie.Name == "easybook.sid" {
			return cookie
		}
	}

	t.Fatal("session cookie was not created")
	return nil
}

func ensureTransactionsSupportedForHandlers(ctx context.Context, client *mongo.Client) error {
	sessionHandle, err := client.StartSession()
	if err != nil {
		return err
	}
	defer sessionHandle.EndSession(ctx)

	_, err = sessionHandle.WithTransaction(ctx, func(txCtx mongo.SessionContext) (any, error) {
		return nil, nil
	})
	return err
}
