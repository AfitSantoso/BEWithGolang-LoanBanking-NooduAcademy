package customer

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	nikPattern     = regexp.MustCompile(`^[0-9]{16}$`)
	phonePattern   = regexp.MustCompile(`^\+?[0-9]{8,15}$`)
	accountPattern = regexp.MustCompile(`^[0-9]{1,50}$`)
	incomePattern  = regexp.MustCompile(`^(0|[1-9][0-9]{0,15})(\.[0-9]{1,2})?$`)
	wib            = time.FixedZone("WIB", 7*60*60)
)

type Service struct {
	repository Repository
	now        func() time.Time
	uploader   ImageUploader
}

func NewService(repository Repository, options ...Option) *Service {
	service := &Service{repository: repository, now: time.Now}
	for _, option := range options {
		option(service)
	}
	return service
}

func (service *Service) Get(ctx context.Context, userID int64) (Profile, error) {
	if userID <= 0 {
		return Profile{}, ErrUnauthenticated
	}
	return service.repository.FindByUserID(ctx, userID)
}

func (service *Service) Save(ctx context.Context, userID int64, input SaveInput) (Profile, error) {
	if userID <= 0 {
		return Profile{}, ErrUnauthenticated
	}
	input, err := normalizeAndValidate(input, service.now())
	if err != nil {
		return Profile{}, err
	}
	return service.repository.Save(ctx, userID, input)
}

func normalizeAndValidate(input SaveInput, now time.Time) (SaveInput, error) {
	fields := make(map[string]string)
	text := func(name string, value *string, max int, required bool) {
		*value = strings.TrimSpace(*value)
		if (required && *value == "") || !utf8.ValidString(*value) || utf8.RuneCountInString(*value) > max || strings.ContainsFunc(*value, unicode.IsControl) {
			fields[name] = "Use valid text within the documented length, without control characters"
		}
	}
	text("nik", &input.NIK, 16, true)
	if !nikPattern.MatchString(input.NIK) {
		fields["nik"] = "Must contain exactly 16 digits"
	}
	text("address", &input.Address, 255, true)
	text("current_address", &input.CurrentAddress, 255, true)
	text("mother_maiden_name", &input.MotherMaidenName, 150, true)
	text("occupation", &input.Occupation, 100, true)
	text("place_of_birth", &input.PlaceOfBirth, 100, true)
	text("phone", &input.Phone, 20, true)
	if !phonePattern.MatchString(input.Phone) {
		fields["phone"] = "Use 8 to 15 digits, optionally prefixed with +"
	}
	input.DateOfBirth = strings.TrimSpace(input.DateOfBirth)
	birth, err := time.Parse("2006-01-02", input.DateOfBirth)
	today, _ := time.Parse("2006-01-02", now.In(wib).Format("2006-01-02"))
	if err != nil || birth.Year() < 1900 || birth.After(today) {
		fields["date_of_birth"] = "Use YYYY-MM-DD from 1900-01-01 through today (WIB)"
	}
	input.MonthlyIncome = strings.TrimSpace(input.MonthlyIncome)
	if !incomePattern.MatchString(input.MonthlyIncome) {
		fields["monthly_income"] = "Use a nonnegative decimal string with up to 16 integer digits and 2 decimal places"
	} else {
		whole, fraction, _ := strings.Cut(input.MonthlyIncome, ".")
		input.MonthlyIncome = whole + "." + (fraction + "00")[:2]
	}
	text("account_number", &input.AccountNumber, 50, false)
	text("account_holder_name", &input.AccountHolderName, 150, false)
	if input.AccountNumber != "" && !accountPattern.MatchString(input.AccountNumber) {
		fields["account_number"] = "Use 1 to 50 digits"
	}
	if (input.AccountNumber == "") != (input.AccountHolderName == "") {
		fields["account_number"] = "Provide account_number and account_holder_name together, or leave both empty"
		fields["account_holder_name"] = fields["account_number"]
	}
	if len(fields) > 0 {
		return SaveInput{}, &ValidationError{Fields: fields}
	}
	return input, nil
}
