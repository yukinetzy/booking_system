package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
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

func TestHotelPresenceConcurrentAccessCapacity(t *testing.T) {
	server, hotelID, cleanup := setupPresenceHandlerIntegration(t, true, 3, 1)
	defer cleanup()

	type result struct {
		status   int
		location string
		err      error
	}

	results := make([]result, 0, 3)
	startGate := make(chan struct{})
	var (
		waitGroup sync.WaitGroup
		mutex     sync.Mutex
	)

	for i := 0; i < 3; i++ {
		waitGroup.Add(1)
		client := newPresenceTestHTTPClient()
		go func(httpClient *http.Client) {
			defer waitGroup.Done()
			<-startGate

			response, err := httpClient.Get(server.URL + "/hotels/" + hotelID)
			if err != nil {
				mutex.Lock()
				results = append(results, result{err: err})
				mutex.Unlock()
				return
			}
			defer response.Body.Close()

			mutex.Lock()
			results = append(results, result{
				status:   response.StatusCode,
				location: response.Header.Get("Location"),
			})
			mutex.Unlock()
		}(client)
	}

	close(startGate)
	waitGroup.Wait()

	if len(results) != 3 {
		t.Fatalf("expected 3 responses, got %d", len(results))
	}

	okCount := 0
	waitCount := 0
	for _, item := range results {
		if item.err != nil {
			t.Fatalf("request failed: %v", item.err)
		}

		switch item.status {
		case http.StatusOK:
			okCount++
		case http.StatusFound:
			if strings.Contains(item.location, "/hotel-wait?hotelId="+hotelID) {
				waitCount++
			} else {
				t.Fatalf("unexpected redirect location: %s", item.location)
			}
		default:
			t.Fatalf("unexpected status: %d", item.status)
		}
	}

	if okCount != 1 {
		t.Fatalf("expected 1 user to enter hotel page, got %d", okCount)
	}
	if waitCount != 2 {
		t.Fatalf("expected 2 users to be redirected to wait page, got %d", waitCount)
	}
}

func TestHotelPresenceAccessAfterTTLExpiry(t *testing.T) {
	server, hotelID, cleanup := setupPresenceHandlerIntegration(t, true, 1, 1)
	defer cleanup()

	client1 := newPresenceTestHTTPClient()
	client2 := newPresenceTestHTTPClient()

	response1, err := client1.Get(server.URL + "/hotels/" + hotelID)
	if err != nil {
		t.Fatalf("first user request failed: %v", err)
	}
	response1.Body.Close()
	if response1.StatusCode != http.StatusOK {
		t.Fatalf("expected first user to enter hotel page, got %d", response1.StatusCode)
	}

	response2, err := client2.Get(server.URL + "/hotels/" + hotelID)
	if err != nil {
		t.Fatalf("second user request failed: %v", err)
	}
	response2.Body.Close()
	if response2.StatusCode != http.StatusFound {
		t.Fatalf("expected second user to be redirected to wait page, got %d", response2.StatusCode)
	}

	time.Sleep(1300 * time.Millisecond)

	response3, err := client2.Get(server.URL + "/hotels/" + hotelID)
	if err != nil {
		t.Fatalf("second user retry failed: %v", err)
	}
	response3.Body.Close()
	if response3.StatusCode != http.StatusOK {
		t.Fatalf("expected second user to enter after ttl expiry, got %d", response3.StatusCode)
	}
}

func TestHotelPresenceHeartbeatExtendsTTL(t *testing.T) {
	server, hotelID, cleanup := setupPresenceHandlerIntegration(t, true, 2, 1)
	defer cleanup()

	client1 := newPresenceTestHTTPClient()
	client2 := newPresenceTestHTTPClient()

	enterResponse, err := client1.Get(server.URL + "/hotels/" + hotelID)
	if err != nil {
		t.Fatalf("first user request failed: %v", err)
	}
	enterResponse.Body.Close()
	if enterResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected first user to enter hotel page, got %d", enterResponse.StatusCode)
	}

	time.Sleep(1100 * time.Millisecond)

	heartbeatResponse, err := client1.Post(server.URL+"/api/hotels/"+hotelID+"/presence/heartbeat", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("heartbeat request failed: %v", err)
	}
	defer heartbeatResponse.Body.Close()
	if heartbeatResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected heartbeat status 200, got %d", heartbeatResponse.StatusCode)
	}

	heartbeatPayload := map[string]any{}
	if err := json.NewDecoder(heartbeatResponse.Body).Decode(&heartbeatPayload); err != nil {
		t.Fatalf("decode heartbeat payload: %v", err)
	}
	if heartbeatPayload["ok"] != true {
		t.Fatalf("expected heartbeat ok=true, got %v", heartbeatPayload)
	}

	time.Sleep(1100 * time.Millisecond)

	waitResponse, err := client2.Get(server.URL + "/hotels/" + hotelID)
	if err != nil {
		t.Fatalf("second user request while heartbeat active failed: %v", err)
	}
	waitResponse.Body.Close()
	if waitResponse.StatusCode != http.StatusFound {
		t.Fatalf("expected second user to wait while heartbeat extends ttl, got %d", waitResponse.StatusCode)
	}

	time.Sleep(1200 * time.Millisecond)

	enterAfterExpiry, err := client2.Get(server.URL + "/hotels/" + hotelID)
	if err != nil {
		t.Fatalf("second user request after heartbeat ttl expiry failed: %v", err)
	}
	enterAfterExpiry.Body.Close()
	if enterAfterExpiry.StatusCode != http.StatusOK {
		t.Fatalf("expected second user to enter after extended ttl expiry, got %d", enterAfterExpiry.StatusCode)
	}
}

func setupPresenceHandlerIntegration(t *testing.T, presenceEnabled bool, ttlSeconds int, capacity int) (*httptest.Server, string, func()) {
	t.Helper()

	mongoURI := strings.TrimSpace(os.Getenv("MONGO_URI"))
	if mongoURI == "" {
		t.Skip("MONGO_URI is not set; skipping integration test")
	}

	dbName := "easybook_presence_handlers_" + primitive.NewObjectID().Hex()
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

	hotelID := primitive.NewObjectID()
	_, err = database.Collection("hotels").InsertOne(ctx, bson.M{
		"_id":             hotelID,
		"title":           "Presence Test Hotel",
		"description":     "Presence integration test hotel",
		"location":        "Test City",
		"address":         "Test Address",
		"price_per_night": 10000,
		"rating":          5,
		"available_rooms": 1,
		"amenities":       []string{"wifi"},
		"createdAt":       time.Now().UTC(),
		"updatedAt":       time.Now().UTC(),
	})
	if err != nil {
		cancel()
		_ = database.Drop(context.Background())
		_ = client.Disconnect(context.Background())
		t.Fatalf("insert test hotel: %v", err)
	}

	sessions, err := session.NewManager(ctx, database, false, "presence-integration-secret-123")
	if err != nil {
		cancel()
		_ = database.Drop(context.Background())
		_ = client.Disconnect(context.Background())
		t.Fatalf("init sessions: %v", err)
	}

	env := config.Env{
		HotelsPageSize:             6,
		HotelsPageMax:              20,
		BookingsPageSize:           8,
		BookingsPageMax:            25,
		PresenceEnabled:            presenceEnabled,
		PresenceTTLSeconds:         ttlSeconds,
		PresenceCapacity:           capacity,
		PresenceMinIntervalSeconds: 1,
	}

	app := NewApp(env, models.NewStore(database), sessions, view.NewRenderer("../../views"), "../../views")
	server := httptest.NewServer(app.Router())

	cleanup := func() {
		server.Close()
		_ = database.Drop(context.Background())
		_ = client.Disconnect(context.Background())
		cancel()
	}

	return server, hotelID.Hex(), cleanup
}

func newPresenceTestHTTPClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 15 * time.Second,
	}
}
