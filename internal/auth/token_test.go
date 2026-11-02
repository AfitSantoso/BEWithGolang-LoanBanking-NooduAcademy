package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestTokenValidation(t *testing.T) {
	secret := strings.Repeat("s", 32)
	manager := NewTokenManager(secret, "test-api", "test-web", 15*time.Minute)
	rawToken, _, err := manager.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	if userID, err := manager.Verify(rawToken); err != nil || userID != 42 {
		t.Fatalf("valid token rejected: %v", err)
	}
	for _, testCase := range []struct {
		name   string
		change func(*jwt.RegisteredClaims)
		method jwt.SigningMethod
		key    any
	}{
		{"expired", func(claims *jwt.RegisteredClaims) {
			claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
		}, jwt.SigningMethodHS256, []byte(secret)},
		{"missing expiration", func(claims *jwt.RegisteredClaims) { claims.ExpiresAt = nil }, jwt.SigningMethodHS256, []byte(secret)},
		{"wrong issuer", func(claims *jwt.RegisteredClaims) { claims.Issuer = "other-api" }, jwt.SigningMethodHS256, []byte(secret)},
		{"wrong audience", func(claims *jwt.RegisteredClaims) { claims.Audience = jwt.ClaimStrings{"other-web"} }, jwt.SigningMethodHS256, []byte(secret)},
		{"future issued at", func(claims *jwt.RegisteredClaims) { claims.IssuedAt = jwt.NewNumericDate(time.Now().Add(time.Hour)) }, jwt.SigningMethodHS256, []byte(secret)},
		{"future not before", func(claims *jwt.RegisteredClaims) { claims.NotBefore = jwt.NewNumericDate(time.Now().Add(time.Hour)) }, jwt.SigningMethodHS256, []byte(secret)},
		{"missing subject", func(claims *jwt.RegisteredClaims) { claims.Subject = "" }, jwt.SigningMethodHS256, []byte(secret)},
		{"negative subject", func(claims *jwt.RegisteredClaims) { claims.Subject = "-1" }, jwt.SigningMethodHS256, []byte(secret)},
		{"wrong secret", func(claims *jwt.RegisteredClaims) {}, jwt.SigningMethodHS256, []byte(strings.Repeat("x", 32))},
		{"wrong algorithm", func(claims *jwt.RegisteredClaims) {}, jwt.SigningMethodHS384, []byte(secret)},
		{"unsigned", func(claims *jwt.RegisteredClaims) {}, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			claims := jwt.RegisteredClaims{Issuer: "test-api", Audience: jwt.ClaimStrings{"test-web"}, Subject: "42", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))}
			testCase.change(&claims)
			rawToken, err := jwt.NewWithClaims(testCase.method, claims).SignedString(testCase.key)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := manager.Verify(rawToken); err == nil {
				t.Fatal("invalid token accepted")
			}
		})
	}
}
