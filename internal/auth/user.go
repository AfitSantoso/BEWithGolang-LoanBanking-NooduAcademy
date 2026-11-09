package auth

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	ErrUserNotFound        = errors.New("user not found")
	ErrInvalidCredentials  = errors.New("invalid email or password")
	ErrInvalidInput        = errors.New("invalid input")
	ErrUserExists          = errors.New("email or username already exists")
	ErrForbidden           = errors.New("admin role required")
	ErrInvalidRegistration = errors.New("invalid registration input")
	ErrInvalidRole         = errors.New("invalid role or user ID")
)

const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

func ValidRole(role string) bool { return role == RoleUser || role == RoleAdmin }

func NormalizeUsername(username string) (string, error) {
	username = strings.TrimSpace(username)
	if username == "" || !utf8.ValidString(username) || utf8.RuneCountInString(username) > 50 || strings.ContainsFunc(username, unicode.IsControl) {
		return "", ErrInvalidRegistration
	}
	return username, nil
}

// User follows the existing public.users table. Password hashes never enter JSON responses.
type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	PasswordHash string `json:"-"`
	IsActive     bool   `json:"-"`
}

type UserRepository interface {
	FindByEmail(context.Context, string) (User, error)
	FindByID(context.Context, int64) (User, error)
	Create(context.Context, string, string, string, string) (User, error)
	SetRole(context.Context, int64, int64, string) (User, error)
}

func NormalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 100 {
		return "", ErrInvalidInput
	}
	return email, nil
}
