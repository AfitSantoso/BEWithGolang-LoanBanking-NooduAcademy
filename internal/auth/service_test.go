package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type stubRepository struct {
	user          User
	err           error
	lookedUpEmail string
}

func (repository *stubRepository) FindByEmail(ctx context.Context, email string) (User, error) {
	repository.lookedUpEmail = email
	return repository.user, repository.err
}

func (repository *stubRepository) FindByID(ctx context.Context, userID int64) (User, error) {
	return repository.user, repository.err
}

func TestLoginAndCurrentUser(t *testing.T) {
	password := "valid-password-123"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	user := User{ID: 42, Username: "alice", Email: "alice@example.com", PasswordHash: string(hash), IsActive: true}
	repository := &stubRepository{user: user}
	manager := NewTokenManager(strings.Repeat("s", 32), "test-api", "test-web", 15*time.Minute)
	service, err := NewService(repository, manager)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.Login(context.Background(), " Alice@Example.com ", password)
	if err != nil {
		t.Fatal(err)
	}
	if repository.lookedUpEmail != "alice@example.com" {
		t.Fatalf("email not normalized: %q", repository.lookedUpEmail)
	}
	if result.TokenType != "Bearer" || result.ExpiresIn != 900 || result.User.PasswordHash != "" {
		t.Fatalf("unexpected login response: %#v", result.User)
	}
	currentUser, err := service.CurrentUser(context.Background(), result.AccessToken)
	if err != nil || currentUser.ID != 42 || currentUser.PasswordHash != "" {
		t.Fatalf("current user failed: %v", err)
	}

	testCases := []struct {
		name            string
		user            User
		repositoryError error
		email           string
		password        string
		wantError       error
	}{
		{"wrong password", user, nil, user.Email, "wrong-password", ErrInvalidCredentials},
		{"unknown user", User{}, ErrUserNotFound, user.Email, password, ErrInvalidCredentials},
		{"inactive user", User{ID: 42, PasswordHash: string(hash)}, nil, user.Email, password, ErrInvalidCredentials},
		{"null password hash", User{ID: 42, IsActive: true}, nil, user.Email, password, ErrInvalidCredentials},
		{"malformed password hash", User{ID: 42, IsActive: true, PasswordHash: "plaintext-is-not-supported"}, nil, user.Email, password, ErrInvalidCredentials},
		{"invalid email", user, nil, "invalid", password, ErrInvalidInput},
		{"display-name email", user, nil, "Alice <alice@example.com>", password, ErrInvalidInput},
		{"empty password", user, nil, user.Email, "", ErrInvalidInput},
		{"oversized bcrypt input", user, nil, user.Email, strings.Repeat("a", 73), ErrInvalidInput},
		{"database failure", User{}, context.DeadlineExceeded, user.Email, password, context.DeadlineExceeded},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			repository.user, repository.err = testCase.user, testCase.repositoryError
			_, err := service.Login(context.Background(), testCase.email, testCase.password)
			if !errors.Is(err, testCase.wantError) {
				t.Fatalf("got %v, want %v", err, testCase.wantError)
			}
		})
	}

	repository.err = nil
	repository.user = user
	repository.user.IsActive = false
	if _, err := service.CurrentUser(context.Background(), result.AccessToken); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("disabled user must lose access immediately: %v", err)
	}
	repository.err = ErrUserNotFound
	if _, err := service.CurrentUser(context.Background(), result.AccessToken); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("deleted user must lose access: %v", err)
	}
	repository.err = context.DeadlineExceeded
	if _, err := service.CurrentUser(context.Background(), result.AccessToken); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("database error must be preserved: %v", err)
	}
	if _, err := service.CurrentUser(context.Background(), "broken-token"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("invalid token accepted: %v", err)
	}
}

func TestHashPassword(t *testing.T) {
	for _, password := range []string{"short", strings.Repeat("a", 73), strings.Repeat(" ", 12), strings.Repeat("é", 37)} {
		if _, err := HashPassword(password); err == nil {
			t.Fatalf("invalid password accepted (%d bytes)", len(password))
		}
	}
	password := "long-enough-password"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		t.Fatal(err)
	}
	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil || cost != passwordCost {
		t.Fatalf("unexpected bcrypt cost: %d, %v", cost, err)
	}
}
