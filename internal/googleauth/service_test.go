package googleauth

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"backend-loanbanking/internal/auth"
)

type memoryRepository struct {
	flows        map[string]Flow
	user         auth.User
	identity     Identity
	resolveCalls int
	linkID       int64
	err          error
}

func (repo *memoryRepository) StoreFlow(_ context.Context, flow Flow) error {
	if repo.flows == nil {
		repo.flows = map[string]Flow{}
	}
	repo.flows[flow.StateHash] = flow
	return repo.err
}
func (repo *memoryRepository) ConsumeFlow(_ context.Context, state, binding string) (Flow, error) {
	flow, ok := repo.flows[state]
	if !ok || flow.BindingHash != binding || !flow.ExpiresAt.After(time.Now()) {
		return Flow{}, ErrInvalidState
	}
	delete(repo.flows, state)
	return flow, repo.err
}
func (repo *memoryRepository) ResolveUser(_ context.Context, id Identity, linkID int64) (auth.User, error) {
	repo.resolveCalls++
	repo.identity = id
	repo.linkID = linkID
	return repo.user, repo.err
}

type fakeProvider struct {
	identity        Identity
	calls           int
	nonce, verifier string
	err             error
}

func (provider *fakeProvider) AuthorizationURL(state, nonce, verifier string) string {
	provider.nonce = nonce
	provider.verifier = verifier
	return "https://accounts.google.com/auth?state=" + url.QueryEscape(state)
}
func (provider *fakeProvider) Verify(_ context.Context, _ string, nonce, verifier string) (Identity, error) {
	provider.calls++
	if nonce != provider.nonce || verifier != provider.verifier {
		return Identity{}, ErrInvalidIdentity
	}
	return provider.identity, provider.err
}

func fixture() (*Service, *memoryRepository, *fakeProvider) {
	repo := &memoryRepository{user: auth.User{ID: 42, Email: "alice@gmail.com", Role: auth.RoleUser, IsActive: true, PasswordHash: "never-expose"}}
	provider := &fakeProvider{identity: Identity{Subject: "123456789", Email: " Alice@Gmail.com ", EmailVerified: true}}
	return NewService(repo, provider, auth.NewTokenManager(strings.Repeat("s", 32), "test-api", "test-web", 15*time.Minute)), repo, provider
}
func start(t *testing.T, service *Service, linkID int64) (string, string) {
	t.Helper()
	address, binding, err := service.Start(context.Background(), linkID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(address)
	return parsed.Query().Get("state"), binding
}

func TestGoogleCompletionIsBrowserBoundAndOneUse(t *testing.T) {
	service, repo, provider := fixture()
	state, binding := start(t, service, 0)
	wrong, _ := randomToken()
	if _, err := service.Complete(context.Background(), state, wrong, "code", ""); !errors.Is(err, ErrInvalidState) {
		t.Fatal("wrong browser accepted")
	}
	result, err := service.Complete(context.Background(), state, binding, "code", "")
	if err != nil || result.User.ID != 42 || result.User.PasswordHash != "" || result.AccessToken == "" || repo.identity.Email != "alice@gmail.com" {
		t.Fatalf("unexpected completion: %v", err)
	}
	if _, err := service.Complete(context.Background(), state, binding, "code", ""); !errors.Is(err, ErrInvalidState) {
		t.Fatal("replay accepted")
	}
	if provider.calls != 1 || repo.resolveCalls != 1 {
		t.Fatal("replayed state reached provider/repository")
	}
}

func TestGoogleFailuresDoNotProvisionUser(t *testing.T) {
	for _, name := range []string{"unverified", "missing subject", "invalid email", "provider error", "consent denied", "missing code", "expired state", "malformed state"} {
		t.Run(name, func(t *testing.T) {
			service, repo, provider := fixture()
			state, binding := start(t, service, 0)
			code, denied := "code", ""
			switch name {
			case "unverified":
				provider.identity.EmailVerified = false
			case "missing subject":
				provider.identity.Subject = ""
			case "invalid email":
				provider.identity.Email = "invalid"
			case "provider error":
				provider.err = ErrProvider
			case "consent denied":
				denied = "access_denied"
			case "missing code":
				code = ""
			case "expired state":
				flow := repo.flows[hash(state)]
				flow.ExpiresAt = time.Now().Add(-time.Second)
				repo.flows[hash(state)] = flow
			case "malformed state":
				state = "bad"
			}
			if _, err := service.Complete(context.Background(), state, binding, code, denied); err == nil || repo.resolveCalls != 0 {
				t.Fatalf("failed login provisioned a user: %v", err)
			}
		})
	}
}

func TestExistingIdentityKeepsRoleAndExplicitLinkOwner(t *testing.T) {
	service, repo, _ := fixture()
	repo.user.Role = auth.RoleAdmin
	state, binding := start(t, service, 42)
	result, err := service.Complete(context.Background(), state, binding, "code", "")
	if err != nil || repo.linkID != 42 || result.User.Role != auth.RoleAdmin {
		t.Fatal("existing role/explicit link not preserved")
	}
	state, binding = start(t, service, 0)
	repo.user.IsActive = false
	if _, err := service.Complete(context.Background(), state, binding, "code", ""); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatal("inactive account accepted")
	}
}
