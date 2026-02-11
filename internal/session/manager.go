package session

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"easybook/internal/types"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	cookieName        = "easybook.sid"
	defaultSessionTTL = 24 * time.Hour
)

type contextKey struct{}

type sessionDocument struct {
	ID        string    `bson:"_id"`
	UserID    string    `bson:"userId"`
	Email     string    `bson:"email"`
	Role      string    `bson:"role"`
	CreatedAt time.Time `bson:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt"`
	ExpiresAt time.Time `bson:"expiresAt"`
}

type Manager struct {
	collection *mongo.Collection
	secure     bool
	ttl        time.Duration
	secret     []byte
}

func NewManager(ctx context.Context, db *mongo.Database, secure bool, sessionSecret string) (*Manager, error) {
	if len(sessionSecret) < 12 {
		return nil, errors.New("session secret must be at least 12 characters")
	}

	collection := db.Collection("sessions")
	_, err := collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "expiresAt", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(0),
	})
	if err != nil {
		return nil, err
	}

	return &Manager{
		collection: collection,
		secure:     secure,
		ttl:        defaultSessionTTL,
		secret:     []byte(sessionSecret),
	}, nil
}

func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := m.loadUser(r)
		ctx := context.WithValue(r.Context(), contextKey{}, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func CurrentUser(r *http.Request) *types.CurrentUser {
	if r == nil {
		return nil
	}
	value := r.Context().Value(contextKey{})
	if value == nil {
		return nil
	}
	user, _ := value.(*types.CurrentUser)
	return user
}

func (m *Manager) StartSession(w http.ResponseWriter, r *http.Request, userID, email, role string) error {
	if role == "" {
		role = "user"
	}

	if existing, err := r.Cookie(cookieName); err == nil && existing.Value != "" {
		if token, ok := m.decodeCookieValue(existing.Value); ok {
			_, _ = m.collection.DeleteOne(r.Context(), bson.M{"_id": token})
		}
	}

	token, err := generateToken()
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	doc := sessionDocument{
		ID:        token,
		UserID:    userID,
		Email:     email,
		Role:      role,
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: now.Add(m.ttl),
	}

	if _, err := m.collection.InsertOne(r.Context(), doc); err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    m.encodeCookieValue(token),
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(m.ttl.Seconds()),
		Expires:  now.Add(m.ttl),
	})

	return nil
}

func (m *Manager) DestroySession(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(cookieName); err == nil && cookie.Value != "" {
		if token, ok := m.decodeCookieValue(cookie.Value); ok {
			_, _ = m.collection.DeleteOne(r.Context(), bson.M{"_id": token})
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func (m *Manager) loadUser(r *http.Request) *types.CurrentUser {
	cookie, err := r.Cookie(cookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}
	token, ok := m.decodeCookieValue(cookie.Value)
	if !ok {
		return nil
	}

	var doc sessionDocument
	err = m.collection.FindOne(r.Context(), bson.M{"_id": token}).Decode(&doc)
	if err != nil {
		return nil
	}

	if doc.ExpiresAt.Before(time.Now().UTC()) {
		_, _ = m.collection.DeleteOne(r.Context(), bson.M{"_id": doc.ID})
		return nil
	}

	role := doc.Role
	if role == "" {
		role = "user"
	}

	return &types.CurrentUser{
		ID:    doc.UserID,
		Email: doc.Email,
		Role:  role,
	}
}

func generateToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func (m *Manager) encodeCookieValue(token string) string {
	return token + "." + m.sign(token)
}

func (m *Manager) decodeCookieValue(value string) (string, bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return "", false
	}

	token := strings.TrimSpace(parts[0])
	signature := strings.TrimSpace(parts[1])
	if token == "" || signature == "" {
		return "", false
	}

	expected := m.sign(token)
	if subtle.ConstantTimeCompare([]byte(signature), []byte(expected)) != 1 {
		return "", false
	}

	return token, true
}

func (m *Manager) sign(token string) string {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(token))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
