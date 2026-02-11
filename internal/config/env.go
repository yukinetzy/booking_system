package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Env struct {
	NodeEnv                    string
	IsProduction               bool
	Port                       int
	MongoURI                   string
	DBName                     string
	DNSServers                 []string
	SessionSecret              string
	HotelsPageSize             int
	HotelsPageMax              int
	BookingsPageSize           int
	BookingsPageMax            int
	PresenceEnabled            bool
	PresenceTTLSeconds         int
	PresenceCapacity           int
	PresenceMinIntervalSeconds int
}

func parseNumber(value string, fallback int) int {
	number, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return number
}

func splitAndTrimCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func parseBool(value string, fallback bool) bool {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return fallback
	}

	switch trimmed {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func Load() (Env, error) {
	_ = godotenv.Load()

	nodeEnv := strings.TrimSpace(os.Getenv("NODE_ENV"))
	if nodeEnv == "" {
		nodeEnv = "development"
	}

	env := Env{
		NodeEnv:                    nodeEnv,
		IsProduction:               nodeEnv == "production",
		Port:                       parseNumber(os.Getenv("PORT"), 3000),
		MongoURI:                   strings.TrimSpace(os.Getenv("MONGO_URI")),
		DBName:                     strings.TrimSpace(os.Getenv("DB_NAME")),
		DNSServers:                 splitAndTrimCSV(defaultString(os.Getenv("DNS_SERVERS"), "8.8.8.8,1.1.1.1")),
		SessionSecret:              os.Getenv("SESSION_SECRET"),
		HotelsPageSize:             parseNumber(os.Getenv("HOTELS_PAGE_SIZE"), 6),
		HotelsPageMax:              parseNumber(os.Getenv("HOTELS_PAGE_MAX_SIZE"), 20),
		BookingsPageSize:           parseNumber(os.Getenv("BOOKINGS_PAGE_SIZE"), 8),
		BookingsPageMax:            parseNumber(os.Getenv("BOOKINGS_PAGE_MAX_SIZE"), 25),
		PresenceEnabled:            parseBool(defaultString(os.Getenv("PRESENCE_ENABLED"), "true"), true),
		PresenceTTLSeconds:         parseNumber(os.Getenv("PRESENCE_TTL_SECONDS"), 60),
		PresenceCapacity:           parseNumber(os.Getenv("PRESENCE_CAPACITY"), 1),
		PresenceMinIntervalSeconds: parseNumber(os.Getenv("PRESENCE_MIN_INTERVAL_SECONDS"), 2),
	}

	if env.DBName == "" {
		env.DBName = "easybook_final"
	}

	var validationErrors []string
	if env.MongoURI == "" {
		validationErrors = append(validationErrors, "MONGO_URI is required.")
	}
	if len(env.SessionSecret) < 12 {
		validationErrors = append(validationErrors, "SESSION_SECRET is required and must be at least 12 characters.")
	}
	if env.PresenceTTLSeconds <= 0 {
		validationErrors = append(validationErrors, "PRESENCE_TTL_SECONDS must be greater than 0.")
	}
	if env.PresenceCapacity <= 0 {
		validationErrors = append(validationErrors, "PRESENCE_CAPACITY must be greater than 0.")
	}
	if env.PresenceMinIntervalSeconds <= 0 {
		validationErrors = append(validationErrors, "PRESENCE_MIN_INTERVAL_SECONDS must be greater than 0.")
	}
	if len(validationErrors) > 0 {
		return Env{}, fmt.Errorf("environment validation failed: %s", strings.Join(validationErrors, " "))
	}

	return env, nil
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
