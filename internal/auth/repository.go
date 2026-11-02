package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct{ pool *pgxpool.Pool }

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

const userColumns = `id, username, email, COALESCE(password_hash, ''), COALESCE(is_active, false)`

func (repository *PostgresRepository) FindByEmail(ctx context.Context, email string) (User, error) {
	rows, err := repository.pool.Query(ctx, `SELECT `+userColumns+` FROM public.users WHERE lower(btrim(email)) = $1 LIMIT 2`, email)
	if err != nil {
		return User{}, fmt.Errorf("find user: %w", err)
	}
	defer rows.Close()
	users, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (User, error) { return scanUser(row) })
	if err != nil {
		return User{}, fmt.Errorf("scan user: %w", err)
	}
	// Older schemas may lack a unique index. Ambiguous identities must fail closed.
	if len(users) != 1 {
		return User{}, ErrUserNotFound
	}
	return users[0], nil
}

func (repository *PostgresRepository) FindByID(ctx context.Context, userID int64) (User, error) {
	user, err := scanUser(repository.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM public.users WHERE id = $1`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("find user: %w", err)
	}
	return user, nil
}

func scanUser(row pgx.Row) (User, error) {
	var user User
	err := row.Scan(&user.ID, &user.Username, &user.Email, &user.PasswordHash, &user.IsActive)
	return user, err
}

func (repository *PostgresRepository) Create(ctx context.Context, username, email, passwordHash string) (User, error) {
	user, err := scanUser(repository.pool.QueryRow(ctx, `
		INSERT INTO public.users (username, email, password_hash, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING `+userColumns, username, email, passwordHash))
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return User{}, ErrUserExists
	}
	if err != nil {
		return User{}, errors.New("cannot create user; verify the users table and its unique indexes")
	}
	return user, nil
}
