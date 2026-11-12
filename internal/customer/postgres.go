package customer

import (
	"backend-loanbanking/internal/imagehost"
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// RowQuerier is implemented by pgxpool.Pool and pgx.Tx. Tests can use a
// transaction and roll back real SQL without leaving customer records behind.
type RowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresRepository struct{ db RowQuerier }

func NewPostgresRepository(db RowQuerier) *PostgresRepository {
	return &PostgresRepository{db: db}
}

var _ Repository = (*PostgresRepository)(nil)

const profileColumns = `id, user_id, nik, address, to_char(date_of_birth, 'YYYY-MM-DD'), monthly_income::text,
 phone, current_address, mother_maiden_name, occupation, place_of_birth, account_number,
 account_holder_name, current_total_debt::text, created_at,
 ktp_image_path, ktp_image_id, selfie_image_path, selfie_image_id, payslip_image_path, payslip_image_id`

func scanProfile(row pgx.Row) (Profile, error) {
	var profile Profile
	err := row.Scan(&profile.ID, &profile.UserID, &profile.NIK, &profile.Address, &profile.DateOfBirth,
		&profile.MonthlyIncome, &profile.Phone, &profile.CurrentAddress, &profile.MotherMaidenName,
		&profile.Occupation, &profile.PlaceOfBirth, &profile.AccountNumber, &profile.AccountHolderName,
		&profile.CurrentTotalDebt, &profile.CreatedAt,
		&profile.KTPImagePath, &profile.KTPImageID, &profile.SelfieImagePath, &profile.SelfieImageID, &profile.PayslipImagePath, &profile.PayslipImageID)
	return profile, err
}

func (repository *PostgresRepository) FindByUserID(ctx context.Context, userID int64) (Profile, error) {
	profile, err := scanProfile(repository.db.QueryRow(ctx, `SELECT `+profileColumns+` FROM public.customers WHERE user_id = $1`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, fmt.Errorf("find customer profile: %w", err)
	}
	return profile, nil
}

func (repository *PostgresRepository) SaveDocument(ctx context.Context, userID int64, kind DocumentKind, uploaded imagehost.Image) (Profile, error) {
	// Column names come only from this fixed whitelist, never client SQL input.
	var pathColumn, idColumn string
	switch kind {
	case KTPImage:
		pathColumn, idColumn = "ktp_image_path", "ktp_image_id"
	case SelfieImage:
		pathColumn, idColumn = "selfie_image_path", "selfie_image_id"
	case PayslipImage:
		pathColumn, idColumn = "payslip_image_path", "payslip_image_id"
	default:
		return Profile{}, ErrInvalidDocument
	}
	profile, err := scanProfile(repository.db.QueryRow(ctx, `UPDATE public.customers SET `+pathColumn+`=$1, `+idColumn+`=$2 WHERE user_id=$3 RETURNING `+profileColumns, uploaded.URL, uploaded.ID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, fmt.Errorf("save customer document: %w", err)
	}
	return profile, nil
}

func (repository *PostgresRepository) Save(ctx context.Context, userID int64, input SaveInput) (Profile, error) {
	// Unique user_id makes this atomic under simultaneous first submissions.
	// Ownership, debt, documents and created_at are never changed by the UPDATE.
	profile, err := scanProfile(repository.db.QueryRow(ctx, `
 INSERT INTO public.customers (user_id, nik, address, date_of_birth, monthly_income, phone,
 current_address, mother_maiden_name, occupation, place_of_birth, account_number, account_holder_name)
 VALUES ($1, $2, $3, $4::text::date, $5::text::numeric, $6, $7, $8, $9, $10, NULLIF($11, ''), NULLIF($12, ''))
 ON CONFLICT (user_id) DO UPDATE SET
 nik = EXCLUDED.nik, address = EXCLUDED.address, date_of_birth = EXCLUDED.date_of_birth,
 monthly_income = EXCLUDED.monthly_income, phone = EXCLUDED.phone,
 current_address = EXCLUDED.current_address, mother_maiden_name = EXCLUDED.mother_maiden_name,
 occupation = EXCLUDED.occupation, place_of_birth = EXCLUDED.place_of_birth,
 account_number = EXCLUDED.account_number, account_holder_name = EXCLUDED.account_holder_name
 RETURNING `+profileColumns, userID, input.NIK, input.Address, input.DateOfBirth, input.MonthlyIncome,
		input.Phone, input.CurrentAddress, input.MotherMaidenName, input.Occupation, input.PlaceOfBirth,
		input.AccountNumber, input.AccountHolderName))
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) {
		if pgerr.Code == "23505" && pgerr.ConstraintName == "customers_nik_normalized_uidx" {
			return Profile{}, ErrIdentityConflict
		}
		if pgerr.Code == "23503" && pgerr.ConstraintName == "fk_customers_users" {
			return Profile{}, ErrUnauthenticated
		}
	}
	if err != nil {
		return Profile{}, fmt.Errorf("save customer profile: %w", err)
	}
	return profile, nil
}
