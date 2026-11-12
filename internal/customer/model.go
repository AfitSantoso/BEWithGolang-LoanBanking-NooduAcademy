package customer

import (
	"errors"
	"time"
)

var (
	ErrNotFound         = errors.New("customer profile not found")
	ErrIdentityConflict = errors.New("customer identity already registered")
	ErrUnauthenticated  = errors.New("authenticated user required")
)

// Profile follows public.customers. Nullable legacy values remain JSON null.
// Debt and document locations are not accepted by the JSON profile input.
// Dedicated file uploads update document metadata through the service.
type Profile struct {
	ID                int64     `json:"id"`
	UserID            int64     `json:"user_id"`
	NIK               *string   `json:"nik"`
	Address           *string   `json:"address"`
	DateOfBirth       *string   `json:"date_of_birth"`
	MonthlyIncome     *string   `json:"monthly_income"`
	Phone             *string   `json:"phone"`
	CurrentAddress    *string   `json:"current_address"`
	MotherMaidenName  *string   `json:"mother_maiden_name"`
	Occupation        *string   `json:"occupation"`
	PlaceOfBirth      *string   `json:"place_of_birth"`
	AccountNumber     *string   `json:"account_number"`
	AccountHolderName *string   `json:"account_holder_name"`
	CurrentTotalDebt  string    `json:"current_total_debt"`
	CreatedAt         time.Time `json:"created_at"`
	KTPImagePath      *string   `json:"ktp_image_path"`
	KTPImageID        *string   `json:"ktp_image_id"`
	SelfieImagePath   *string   `json:"selfie_image_path"`
	SelfieImageID     *string   `json:"selfie_image_id"`
	PayslipImagePath  *string   `json:"payslip_image_path"`
	PayslipImageID    *string   `json:"payslip_image_id"`
}

// SaveInput is a complete replacement of editable profile fields, not a PATCH.
type SaveInput struct {
	NIK               string `json:"nik"`
	Address           string `json:"address"`
	DateOfBirth       string `json:"date_of_birth"`
	MonthlyIncome     string `json:"monthly_income"`
	Phone             string `json:"phone"`
	CurrentAddress    string `json:"current_address"`
	MotherMaidenName  string `json:"mother_maiden_name"`
	Occupation        string `json:"occupation"`
	PlaceOfBirth      string `json:"place_of_birth"`
	AccountNumber     string `json:"account_number"`
	AccountHolderName string `json:"account_holder_name"`
}

type ValidationError struct{ Fields map[string]string }

func (err *ValidationError) Error() string { return "invalid customer profile" }
