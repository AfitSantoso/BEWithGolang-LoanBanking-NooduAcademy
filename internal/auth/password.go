package auth

import (
	"errors"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const passwordCost = 12

func HashPassword(password string) (string, error) {
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 12 || len(password) > 72 {
		return "", errors.New("password must contain at least 12 characters and at most 72 bytes")
	}
	var hasNonSpace bool
	for _, character := range password {
		if !unicode.IsSpace(character) {
			hasNonSpace = true
			break
		}
	}
	if !hasNonSpace {
		return "", errors.New("password cannot contain only whitespace")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), passwordCost)
	return string(hash), err
}
