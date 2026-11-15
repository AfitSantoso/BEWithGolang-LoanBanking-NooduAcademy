package auth

import (
	"context"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"strings"
	"testing"
	"time"
)

func TestGoogleOnlyUserCanEstablishLocalPassword(t *testing.T) {
	repo := &stubRepository{user: User{ID: 42, Email: "alice@gmail.com", IsActive: true, Role: RoleUser}}
	manager := NewTokenManager(strings.Repeat("s", 32), "api", "web", 15*time.Minute)
	service, err := NewService(repo, manager)
	if err != nil {
		t.Fatal(err)
	}
	token, _, _ := manager.Issue(42)
	if _, err := service.Login(context.Background(), repo.user.Email, "new-password-123"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("Google-only account accepted local login before setting password")
	}
	if err := service.SetPassword(context.Background(), token, "", "new-password-123"); err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(repo.createdHash), []byte("new-password-123")) != nil {
		t.Fatal("new password not hashed")
	}
	repo.user.PasswordHash = repo.createdHash
	result, err := service.Login(context.Background(), repo.user.Email, "new-password-123")
	if err != nil || result.User.ID != 42 {
		t.Fatalf("local login did not reuse same account: %v", err)
	}
	before := repo.createdHash
	if err := service.SetPassword(context.Background(), token, "wrong", "another-password-123"); !errors.Is(err, ErrInvalidCredentials) || repo.createdHash != before {
		t.Fatal("existing password changed without current password")
	}
	if err := service.SetPassword(context.Background(), token, "new-password-123", "short"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatal(err)
	}
	if err := service.SetPassword(context.Background(), "bad-token", "new-password-123", "another-password-123"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal(err)
	}
	repo.user.IsActive = false
	if err := service.SetPassword(context.Background(), token, "new-password-123", "another-password-123"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("inactive account changed password")
	}
}
