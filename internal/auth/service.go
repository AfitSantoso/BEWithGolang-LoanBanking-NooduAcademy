package auth

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

type LoginResult struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	User        User   `json:"user"`
}

type Service struct {
	repository UserRepository
	tokens     *TokenManager
	dummyHash  []byte
}

func NewService(repository UserRepository, tokens *TokenManager) (*Service, error) {
	// Unknown users still incur the same bcrypt work as a normal login attempt.
	dummyHash, err := bcrypt.GenerateFromPassword([]byte("dummy-password-never-used-for-login"), passwordCost)
	if err != nil {
		return nil, fmt.Errorf("initialize password verifier: %w", err)
	}
	return &Service{repository: repository, tokens: tokens, dummyHash: dummyHash}, nil
}

func (service *Service) Login(ctx context.Context, email, password string) (LoginResult, error) {
	email, err := NormalizeEmail(email)
	if err != nil || len(password) == 0 || len(password) > 72 {
		return LoginResult{}, ErrInvalidInput
	}
	user, err := service.repository.FindByEmail(ctx, email)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return LoginResult{}, err
	}
	hash := []byte(user.PasswordHash)
	if err != nil || !user.IsActive || len(hash) == 0 {
		hash = service.dummyHash
	}
	passwordError := bcrypt.CompareHashAndPassword(hash, []byte(password))
	if err != nil || !user.IsActive || passwordError != nil {
		return LoginResult{}, ErrInvalidCredentials
	}
	token, expiresIn, err := service.tokens.Issue(user.ID)
	if err != nil {
		return LoginResult{}, fmt.Errorf("issue access token: %w", err)
	}
	user.PasswordHash = ""
	return LoginResult{AccessToken: token, TokenType: "Bearer", ExpiresIn: expiresIn, User: user}, nil
}

func (service *Service) CurrentUser(ctx context.Context, accessToken string) (User, error) {
	userID, err := service.tokens.Verify(accessToken)
	if err != nil {
		return User{}, ErrInvalidCredentials
	}
	user, err := service.repository.FindByID(ctx, userID)
	if errors.Is(err, ErrUserNotFound) {
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, err
	}
	if !user.IsActive {
		return User{}, ErrInvalidCredentials
	}
	user.PasswordHash = ""
	return user, nil
}
