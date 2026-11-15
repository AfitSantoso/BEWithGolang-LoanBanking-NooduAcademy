package googleauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"backend-loanbanking/internal/auth"
)

var (
	ErrInvalidState    = errors.New("invalid or expired Google login state")
	ErrInvalidIdentity = errors.New("invalid Google identity")
	ErrProvider        = errors.New("Google authentication unavailable")
	ErrAccountConflict = errors.New("Google identity already linked to another account")
	ErrLinkRequired    = errors.New("sign in to the existing account before linking Google")
	ErrConsentDenied   = errors.New("Google login was cancelled")
)

const FlowLifetime = 10 * time.Minute

type Identity struct {
	Subject       string
	Email         string
	EmailVerified bool
	HostedDomain  string
}

// Automatic email linking is safe only where Google is authoritative.
func (identity Identity) authoritativeEmail() bool {
	_, domain, _ := strings.Cut(identity.Email, "@")
	return domain == "gmail.com" || (identity.HostedDomain != "" && strings.EqualFold(domain, identity.HostedDomain))
}

type Flow struct {
	StateHash   string
	BindingHash string
	Nonce       string
	Verifier    string
	ExpiresAt   time.Time
	LinkUserID  int64
}

type Repository interface {
	StoreFlow(context.Context, Flow) error
	ConsumeFlow(context.Context, string, string) (Flow, error)
	ResolveUser(context.Context, Identity, int64) (auth.User, error)
}

type Provider interface {
	AuthorizationURL(state, nonce, verifier string) string
	Verify(context.Context, string, string, string) (Identity, error)
}

type TokenIssuer interface {
	Issue(int64) (string, int64, error)
}

type Service struct {
	repository Repository
	provider   Provider
	tokens     TokenIssuer
}

func NewService(repository Repository, provider Provider, tokens TokenIssuer) *Service {
	return &Service{repository: repository, provider: provider, tokens: tokens}
}

func randomToken() (string, error) {
	var data [32]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data[:]), nil
}
func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func validToken(value string) bool {
	data, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(data) == 32 && len(value) == 43
}

func (service *Service) Start(ctx context.Context, linkUserID int64) (authorizationURL, binding string, err error) {
	state, err := randomToken()
	if err != nil {
		return "", "", err
	}
	binding, err = randomToken()
	if err != nil {
		return "", "", err
	}
	nonce, err := randomToken()
	if err != nil {
		return "", "", err
	}
	verifier, err := randomToken()
	if err != nil {
		return "", "", err
	}
	flow := Flow{StateHash: hash(state), BindingHash: hash(binding), Nonce: nonce, Verifier: verifier, ExpiresAt: time.Now().Add(FlowLifetime), LinkUserID: linkUserID}
	if err := service.repository.StoreFlow(ctx, flow); err != nil {
		return "", "", err
	}
	return service.provider.AuthorizationURL(state, nonce, verifier), binding, nil
}

func (service *Service) Complete(ctx context.Context, state, binding, code, providerError string) (auth.LoginResult, error) {
	if !validToken(state) || !validToken(binding) {
		return auth.LoginResult{}, ErrInvalidState
	}
	flow, err := service.repository.ConsumeFlow(ctx, hash(state), hash(binding))
	if err != nil {
		return auth.LoginResult{}, err
	}
	if providerError != "" {
		return auth.LoginResult{}, ErrConsentDenied
	}
	if code == "" || len(code) > 4096 {
		return auth.LoginResult{}, ErrInvalidIdentity
	}
	identity, err := service.provider.Verify(ctx, code, flow.Nonce, flow.Verifier)
	if err != nil {
		return auth.LoginResult{}, err
	}
	email, err := auth.NormalizeEmail(identity.Email)
	if err != nil || !identity.EmailVerified || identity.Subject == "" || len(identity.Subject) > 255 {
		return auth.LoginResult{}, ErrInvalidIdentity
	}
	identity.Email = email
	user, err := service.repository.ResolveUser(ctx, identity, flow.LinkUserID)
	if err != nil {
		return auth.LoginResult{}, err
	}
	if !user.IsActive {
		return auth.LoginResult{}, auth.ErrInvalidCredentials
	}
	token, expires, err := service.tokens.Issue(user.ID)
	if err != nil {
		return auth.LoginResult{}, err
	}
	user.PasswordHash = ""
	return auth.LoginResult{AccessToken: token, TokenType: "Bearer", ExpiresIn: expires, User: user}, nil
}
