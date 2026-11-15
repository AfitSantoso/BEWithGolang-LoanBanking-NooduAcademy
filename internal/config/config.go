package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	HTTPAddress        string
	DatabaseURL        string
	JWTSecret          string
	JWTIssuer          string
	JWTAudience        string
	JWTTTL             time.Duration
	CORSOrigin         string
	ImgBBAPIKey        string
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string
}

// LoadDatabaseURL also supports administrative commands that do not need JWT settings.
func LoadDatabaseURL() (string, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("cannot load .env file")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	parsed, err := url.Parse(databaseURL)
	if err != nil || parsed == nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Hostname() == "" || parsed.Path == "" || parsed.Path == "/" {
		return "", errors.New("DATABASE_URL must be a PostgreSQL connection URL with a database name")
	}
	if strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".neon.tech") && parsed.Query().Get("sslmode") != "verify-full" {
		return "", errors.New("Neon DATABASE_URL must use sslmode=verify-full")
	}
	return databaseURL, nil
}

func Load() (Config, error) {
	databaseURL, err := LoadDatabaseURL()
	if err != nil {
		return Config{}, err
	}
	secret := os.Getenv("JWT_SECRET")
	if len(secret) < 32 || strings.TrimSpace(secret) != secret {
		return Config{}, errors.New("JWT_SECRET must contain at least 32 bytes without surrounding whitespace")
	}
	tokenTTL, err := time.ParseDuration(valueOrDefault("JWT_TTL", "15m"))
	if err != nil || tokenTTL < time.Second || tokenTTL > time.Hour {
		return Config{}, errors.New("JWT_TTL must be between 1s and 1h")
	}
	origin := strings.TrimSpace(os.Getenv("CORS_ORIGIN"))
	if origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return Config{}, fmt.Errorf("CORS_ORIGIN must be an exact HTTP(S) origin without a path")
		}
	}
	imageKey := os.Getenv("IMGBB_API_KEY")
	if strings.TrimSpace(imageKey) != imageKey || strings.ContainsAny(imageKey, "\r\n\t ") {
		return Config{}, errors.New("IMGBB_API_KEY must not contain whitespace")
	}
	googleID, googleSecret, googleRedirect := os.Getenv("GOOGLE_CLIENT_ID"), os.Getenv("GOOGLE_CLIENT_SECRET"), os.Getenv("GOOGLE_REDIRECT_URL")
	if googleID != "" || googleSecret != "" || googleRedirect != "" {
		if googleID == "" || googleSecret == "" || googleRedirect == "" || strings.ContainsAny(googleID+googleSecret, " \r\n\t") {
			return Config{}, errors.New("Set GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET and GOOGLE_REDIRECT_URL together without whitespace")
		}
		callback, err := url.Parse(googleRedirect)
		if err != nil || callback.Host == "" || callback.User != nil || callback.RawQuery != "" || callback.Fragment != "" || callback.Path != "/auth/google/callback" ||
			(callback.Scheme != "https" && (callback.Scheme != "http" || (callback.Hostname() != "127.0.0.1" && callback.Hostname() != "localhost" && callback.Hostname() != "::1"))) {
			return Config{}, errors.New("GOOGLE_REDIRECT_URL must be HTTPS (HTTP allowed on loopback only) with path /auth/google/callback, without credentials, query or fragment")
		}
	}
	return Config{
		HTTPAddress:    valueOrDefault("HTTP_ADDRESS", "127.0.0.1:8080"),
		DatabaseURL:    databaseURL,
		JWTSecret:      secret,
		JWTIssuer:      valueOrDefault("JWT_ISSUER", "loanbanking-api"),
		JWTAudience:    valueOrDefault("JWT_AUDIENCE", "loanbanking-web"),
		JWTTTL:         tokenTTL,
		CORSOrigin:     origin,
		ImgBBAPIKey:    imageKey,
		GoogleClientID: googleID, GoogleClientSecret: googleSecret, GoogleRedirectURL: googleRedirect,
	}, nil
}

func valueOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
