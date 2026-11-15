package googleauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"backend-loanbanking/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Database interface {
	Begin(context.Context) (pgx.Tx, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type PostgresRepository struct{ db Database }

func NewPostgresRepository(db Database) *PostgresRepository { return &PostgresRepository{db: db} }

func (repo *PostgresRepository) StoreFlow(ctx context.Context, flow Flow) error {
	_, err := repo.db.Exec(ctx, `WITH expired AS (DELETE FROM public.google_oauth_states WHERE expires_at<=CURRENT_TIMESTAMP)
 INSERT INTO public.google_oauth_states(state_hash,binding_hash,nonce,verifier,expires_at,link_user_id) VALUES($1,$2,$3,$4,$5,NULLIF($6,0))`, flow.StateHash, flow.BindingHash, flow.Nonce, flow.Verifier, flow.ExpiresAt, flow.LinkUserID)
	return err
}

func (repo *PostgresRepository) ConsumeFlow(ctx context.Context, stateHash, bindingHash string) (Flow, error) {
	var flow Flow
	err := repo.db.QueryRow(ctx, `DELETE FROM public.google_oauth_states WHERE state_hash=$1 AND binding_hash=$2 AND expires_at>CURRENT_TIMESTAMP RETURNING nonce,verifier,COALESCE(link_user_id,0)`, stateHash, bindingHash).Scan(&flow.Nonce, &flow.Verifier, &flow.LinkUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Flow{}, ErrInvalidState
	}
	return flow, err
}

const accountColumns = `id, username, email, COALESCE(password_hash,''), COALESCE(is_active,false), role, COALESCE(google_subject,'')`

func scanAccount(row pgx.Row) (auth.User, string, error) {
	var user auth.User
	var subject string
	err := row.Scan(&user.ID, &user.Username, &user.Email, &user.PasswordHash, &user.IsActive, &user.Role, &subject)
	return user, subject, err
}

func (repo *PostgresRepository) ResolveUser(ctx context.Context, identity Identity, linkUserID int64) (auth.User, error) {
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return auth.User{}, err
	}
	defer tx.Rollback(ctx)
	// Serialize initial linking for the same Google subject across instances.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "google:"+identity.Subject); err != nil {
		return auth.User{}, err
	}
	user, _, err := scanAccount(tx.QueryRow(ctx, `SELECT `+accountColumns+` FROM public.users WHERE google_subject=$1 FOR UPDATE`, identity.Subject))
	if err == nil {
		if !user.IsActive {
			return auth.User{}, auth.ErrInvalidCredentials
		}
		if linkUserID != 0 && linkUserID != user.ID {
			return auth.User{}, ErrAccountConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return auth.User{}, err
		}
		return user, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return auth.User{}, err
	}
	if linkUserID == 0 {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return auth.User{}, err
		}
		username := "google_" + hex.EncodeToString(random[:])
		// An existing email is not overwritten; password registration can race safely.
		user, _, err = scanAccount(tx.QueryRow(ctx, `INSERT INTO public.users(username,email,password_hash,is_active,role,google_subject,created_at,updated_at)
 VALUES($1,$2,NULL,true,'user',$3,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)
 ON CONFLICT DO NOTHING RETURNING `+accountColumns, username, identity.Email, identity.Subject))
		if err == nil {
			if err := tx.Commit(ctx); err != nil {
				return auth.User{}, err
			}
			return user, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return auth.User{}, err
		}
	}
	var subject string
	user, subject, err = scanAccount(tx.QueryRow(ctx, `SELECT `+accountColumns+` FROM public.users WHERE lower(btrim(email))=$1 FOR UPDATE`, identity.Email))
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.User{}, ErrAccountConflict
	}
	if err != nil {
		return auth.User{}, err
	}
	if !user.IsActive {
		return auth.User{}, auth.ErrInvalidCredentials
	}
	if linkUserID != 0 && linkUserID != user.ID {
		return auth.User{}, ErrAccountConflict
	}
	if subject != "" && subject != identity.Subject {
		return auth.User{}, ErrAccountConflict
	}
	if subject == "" && linkUserID == 0 && !identity.authoritativeEmail() {
		return auth.User{}, ErrLinkRequired
	}
	if _, err := tx.Exec(ctx, `UPDATE public.users SET google_subject=$1,updated_at=CURRENT_TIMESTAMP WHERE id=$2`, identity.Subject, user.ID); err != nil {
		return auth.User{}, fmt.Errorf("link Google account: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return auth.User{}, err
	}
	return user, nil
}
