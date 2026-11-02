package auth

import (
	"errors"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type TokenManager struct {
	secret   []byte
	issuer   string
	audience string
	ttl      time.Duration
}

func NewTokenManager(secret, issuer, audience string, ttl time.Duration) *TokenManager {
	return &TokenManager{secret: []byte(secret), issuer: issuer, audience: audience, ttl: ttl}
}

func (manager *TokenManager) Issue(userID int64) (string, int64, error) {
	now := time.Now().UTC()
	claims := jwt.RegisteredClaims{
		Issuer:    manager.issuer,
		Subject:   strconv.FormatInt(userID, 10),
		Audience:  jwt.ClaimStrings{manager.audience},
		ExpiresAt: jwt.NewNumericDate(now.Add(manager.ttl)),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(manager.secret)
	return token, int64(manager.ttl.Seconds()), err
}

func (manager *TokenManager) Verify(rawToken string) (int64, error) {
	var claims jwt.RegisteredClaims
	token, err := jwt.ParseWithClaims(rawToken, &claims, func(token *jwt.Token) (any, error) {
		return manager.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer(manager.issuer), jwt.WithAudience(manager.audience), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil || !token.Valid {
		return 0, errors.New("invalid access token")
	}
	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || userID <= 0 {
		return 0, errors.New("invalid access token subject")
	}
	return userID, nil
}
