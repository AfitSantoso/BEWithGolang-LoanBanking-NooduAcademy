package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"backend-loanbanking/internal/auth"
	"backend-loanbanking/internal/config"
	"backend-loanbanking/internal/database"

	"golang.org/x/term"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Create user:", err)
		os.Exit(1)
	}
}

func run() error {
	usernameFlag := flag.String("username", "", "Unique username (1 to 50 characters)")
	emailFlag := flag.String("email", "", "Unique email address")
	roleFlag := flag.String("role", auth.RoleUser, "Account role: user or admin")
	flag.Parse()
	username, err := auth.NormalizeUsername(*usernameFlag)
	if err != nil {
		return errors.New("provide --username with 1 to 50 characters and no control characters")
	}
	if !auth.ValidRole(*roleFlag) {
		return errors.New("provide --role user or admin")
	}
	email, err := auth.NormalizeEmail(*emailFlag)
	if err != nil {
		return errors.New("provide a valid --email with at most 100 bytes")
	}
	databaseURL, err := config.LoadDatabaseURL()
	if err != nil {
		return err
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("run this command in an interactive terminal to enter a hidden password")
	}
	fmt.Fprint(os.Stderr, "Password (12+ characters, at most 72 bytes): ")
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return errors.New("cannot read password")
	}
	defer clear(password)
	fmt.Fprint(os.Stderr, "Confirm password: ")
	confirmation, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return errors.New("cannot read password confirmation")
	}
	defer clear(confirmation)
	if string(password) != string(confirmation) {
		return errors.New("passwords do not match")
	}
	passwordHash, err := auth.HashPassword(string(password))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	user, err := auth.NewPostgresRepository(pool).Create(ctx, username, email, passwordHash, *roleFlag)
	if err != nil {
		return err
	}
	fmt.Printf("User created successfully (id=%d, role=%s).\n", user.ID, user.Role)
	return nil
}
