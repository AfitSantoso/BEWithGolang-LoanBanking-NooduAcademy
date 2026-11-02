package config

import (
	"strings"
	"testing"
)

func TestConfigValidation(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgresql://user:secret@localhost:5432/test?sslmode=disable")
	t.Setenv("JWT_SECRET", strings.Repeat("s", 32))
	t.Setenv("JWT_TTL", "15m")
	t.Setenv("CORS_ORIGIN", "http://localhost:3000")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct{ key, value string }{
		{"DATABASE_URL", ""},
		{"DATABASE_URL", "postgresql://user:private-password@host"},
		{"DATABASE_URL", "postgresql://user:private-password@ep-example.neon.tech/db?sslmode=require"},
		{"JWT_SECRET", "short"},
		{"JWT_TTL", "24h"},
		{"JWT_TTL", "invalid"},
		{"CORS_ORIGIN", "*"},
		{"CORS_ORIGIN", "http://localhost:3000/path"},
	} {
		t.Run(testCase.key+"="+testCase.value, func(t *testing.T) {
			t.Setenv(testCase.key, testCase.value)
			_, err := Load()
			if err == nil {
				t.Fatal("invalid configuration accepted")
			}
			if strings.Contains(err.Error(), "private-password") {
				t.Fatal("connection password leaked")
			}
		})
	}
}
