package customer

import (
	"backend-loanbanking/internal/imagehost"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRepository struct {
	userID int64
	input  SaveInput
	calls  int
	err    error
}

func (repo *fakeRepository) SaveDocument(_ context.Context, id int64, _ DocumentKind, _ imagehost.Image) (Profile, error) {
	repo.userID, repo.calls = id, repo.calls+1
	return Profile{ID: 7, UserID: id}, repo.err
}

func (repo *fakeRepository) FindByUserID(_ context.Context, id int64) (Profile, error) {
	repo.userID, repo.calls = id, repo.calls+1
	return Profile{UserID: id}, repo.err
}
func (repo *fakeRepository) Save(_ context.Context, id int64, input SaveInput) (Profile, error) {
	repo.userID, repo.input, repo.calls = id, input, repo.calls+1
	return Profile{UserID: id, NIK: &input.NIK}, repo.err
}

func validInput() SaveInput {
	return SaveInput{NIK: "3173010101900001", Address: "Jl. Merdeka 1", DateOfBirth: "1990-01-01",
		MonthlyIncome: "7500000", Phone: "+6281234567890", CurrentAddress: "Jl. Merdeka 2",
		MotherMaidenName: "Ibu Contoh", Occupation: "Karyawan", PlaceOfBirth: "Jakarta",
		AccountNumber: "0012345678", AccountHolderName: "Nama Contoh"}
}

func TestSaveValidatesBeforePersistence(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		change      func(*SaveInput)
	}{
		{"missing NIK", "nik", func(i *SaveInput) { i.NIK = "" }},
		{"short NIK", "nik", func(i *SaveInput) { i.NIK = "123" }},
		{"unicode NIK", "nik", func(i *SaveInput) { i.NIK = strings.Repeat("１", 16) }},
		{"missing address", "address", func(i *SaveInput) { i.Address = " " }},
		{"long address", "address", func(i *SaveInput) { i.Address = strings.Repeat("a", 256) }},
		{"control characters", "occupation", func(i *SaveInput) { i.Occupation = "A\nB" }},
		{"invalid text", "mother_maiden_name", func(i *SaveInput) { i.MotherMaidenName = string([]byte{0xff}) }},
		{"invalid date", "date_of_birth", func(i *SaveInput) { i.DateOfBirth = "2020-02-30" }},
		{"future date", "date_of_birth", func(i *SaveInput) { i.DateOfBirth = "2026-11-13" }},
		{"ancient date", "date_of_birth", func(i *SaveInput) { i.DateOfBirth = "1899-01-01" }},
		{"phone spaces", "phone", func(i *SaveInput) { i.Phone = "0812 3456 7890" }},
		{"missing occupation", "occupation", func(i *SaveInput) { i.Occupation = "" }},
		{"missing birthplace", "place_of_birth", func(i *SaveInput) { i.PlaceOfBirth = "" }},
		{"missing mother", "mother_maiden_name", func(i *SaveInput) { i.MotherMaidenName = "" }},
		{"missing current address", "current_address", func(i *SaveInput) { i.CurrentAddress = "" }},
		{"negative income", "monthly_income", func(i *SaveInput) { i.MonthlyIncome = "-1" }},
		{"too many decimals", "monthly_income", func(i *SaveInput) { i.MonthlyIncome = "1.001" }},
		{"income overflow", "monthly_income", func(i *SaveInput) { i.MonthlyIncome = "10000000000000000" }},
		{"income exponent", "monthly_income", func(i *SaveInput) { i.MonthlyIncome = "1e6" }},
		{"missing income", "monthly_income", func(i *SaveInput) { i.MonthlyIncome = "" }},
		{"account pair", "account_holder_name", func(i *SaveInput) { i.AccountHolderName = "" }},
		{"invalid account", "account_number", func(i *SaveInput) { i.AccountNumber = "abc" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepository{}
			service := NewService(repo)
			service.now = func() time.Time { return time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC) }
			input := validInput()
			tc.change(&input)
			_, err := service.Save(context.Background(), 42, input)
			var validation *ValidationError
			if !errors.As(err, &validation) || validation.Fields[tc.field] == "" {
				t.Fatalf("expected field %s validation, got %v", tc.field, err)
			}
			if repo.calls != 0 {
				t.Fatal("invalid profile reached persistence")
			}
		})
	}
}

func TestNormalizationOwnershipAndRepositoryErrors(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo)
	input := validInput()
	input.Address = "  " + strings.Repeat("界", 255) + "  "
	input.MonthlyIncome = " 9999999999999999.99 "
	_, err := service.Save(context.Background(), 42, input)
	if err != nil || repo.userID != 42 || repo.input.MonthlyIncome != "9999999999999999.99" || strings.HasPrefix(repo.input.Address, " ") {
		t.Fatalf("normalization/decimal precision failed: %v", err)
	}
	for raw, want := range map[string]string{"0": "0.00", "12.3": "12.30", "7500000": "7500000.00"} {
		input = validInput()
		input.MonthlyIncome = raw
		input.AccountNumber = ""
		input.AccountHolderName = ""
		if _, err := service.Save(context.Background(), 42, input); err != nil || repo.input.MonthlyIncome != want {
			t.Fatalf("income %s: %v", raw, err)
		}
	}
	calls := repo.calls
	for _, id := range []int64{0, -1} {
		if _, err := service.Save(context.Background(), id, validInput()); !errors.Is(err, ErrUnauthenticated) {
			t.Fatal(err)
		}
		if _, err := service.Get(context.Background(), id); !errors.Is(err, ErrUnauthenticated) {
			t.Fatal(err)
		}
	}
	if repo.calls != calls {
		t.Fatal("unauthenticated request reached repository")
	}
	for _, want := range []error{ErrNotFound, ErrIdentityConflict, context.DeadlineExceeded} {
		repo.err = want
		if _, err := service.Save(context.Background(), 42, validInput()); !errors.Is(err, want) {
			t.Fatal(err)
		}
		if _, err := service.Get(context.Background(), 42); !errors.Is(err, want) {
			t.Fatal(err)
		}
	}
}

func TestDateValidationUsesWIB(t *testing.T) {
	input := validInput()
	input.DateOfBirth = "2026-11-13"
	// Already November 13 in Jakarta, while UTC still says November 12.
	_, err := normalizeAndValidate(input, time.Date(2026, 11, 12, 18, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
}
