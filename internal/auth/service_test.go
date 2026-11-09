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
	createdRole   string
	createdHash   string
	setRoleCalls  int
}

func (repository *stubRepository) Create(ctx context.Context, username, email, hash, role string) (User, error) {
	repository.createdRole, repository.createdHash = role, hash
	return User{ID: 43, Username: username, Email: email, Role: role, PasswordHash: hash, IsActive: true}, repository.err
}

func (repository *stubRepository) SetRole(ctx context.Context, actorID, userID int64, role string) (User, error) {
	repository.setRoleCalls++
	return User{ID: userID, Role: role, PasswordHash: "must-not-leak"}, repository.err
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

func TestRegisterAndRBAC(t *testing.T) {
	repository := &stubRepository{user: User{ID: 1, IsActive: true, Role: RoleAdmin}}
	manager := NewTokenManager(strings.Repeat("s", 32), "test-api", "test-web", 15*time.Minute)
	service, err := NewService(repository, manager)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	user, err := service.Register(ctx, " alice ", " Alice@Example.com ", "valid-password-123")
	if err != nil || user.Role != RoleUser || user.Username != "alice" || user.Email != "alice@example.com" || user.PasswordHash != "" {
		t.Fatalf("registration failed: %#v, %v", user, err)
	}
	if repository.createdRole != RoleUser || bcrypt.CompareHashAndPassword([]byte(repository.createdHash), []byte("valid-password-123")) != nil {
		t.Fatal("registration must persist user role and bcrypt hash")
	}
	for _, input := range []struct{ username, email, password string }{
		{"", "a@example.com", "valid-password-123"},
		{strings.Repeat("a", 51), "a@example.com", "valid-password-123"},
		{"a\nb", "a@example.com", "valid-password-123"},
		{"alice", "invalid", "valid-password-123"},
		{"alice", "a@example.com", "short"},
		{"alice", "a@example.com", strings.Repeat(" ", 12)},
	} {
		if _, err := service.Register(ctx, input.username, input.email, input.password); !errors.Is(err, ErrInvalidRegistration) {
			t.Fatalf("invalid registration accepted: %v", err)
		}
	}
	repository.err = ErrUserExists
	if _, err := service.Register(ctx, "alice", "alice@example.com", "valid-password-123"); !errors.Is(err, ErrUserExists) {
		t.Fatal(err)
	}
	repository.err = nil
	token, _, err := manager.Issue(1)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{RoleUser, RoleAdmin} {
		updated, err := service.SetUserRole(ctx, token, 43, role)
		if err != nil || updated.Role != role || updated.PasswordHash != "" {
			t.Fatalf("admin update failed: %#v, %v", updated, err)
		}
	}
	for _, input := range []struct {
		id   int64
		role string
	}{{0, RoleAdmin}, {-1, RoleAdmin}, {43, "owner"}, {43, ""}} {
		if _, err := service.SetUserRole(ctx, token, input.id, input.role); !errors.Is(err, ErrInvalidRole) {
			t.Fatalf("invalid role accepted: %v", err)
		}
	}
	calls := repository.setRoleCalls
	// Reuse the same token after a database role change: authorization must change immediately.
	repository.user.Role = RoleUser
	if _, err := service.SetUserRole(ctx, token, 43, RoleAdmin); !errors.Is(err, ErrForbidden) {
		t.Fatalf("user escalated privileges: %v", err)
	}
	repository.user.Role = "unknown"
	if _, err := service.SetUserRole(ctx, token, 43, RoleAdmin); !errors.Is(err, ErrForbidden) {
		t.Fatal(err)
	}
	repository.user.Role, repository.user.IsActive = RoleAdmin, false
	if _, err := service.SetUserRole(ctx, token, 43, RoleAdmin); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal(err)
	}
	if _, err := service.SetUserRole(ctx, "broken-token", 43, RoleAdmin); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal(err)
	}
	if repository.setRoleCalls != calls {
		t.Fatal("unauthorized updates reached repository")
	}
}
