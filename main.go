package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"backend-loanbanking/internal/auth"
	"backend-loanbanking/internal/config"
	"backend-loanbanking/internal/customer"
	"backend-loanbanking/internal/database"
	"backend-loanbanking/internal/httpapi"
	"backend-loanbanking/internal/imagehost/imgbb"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "error", err.Error())
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := database.Open(ctx, settings.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	logger.Info("database connected", "database", pool.Config().ConnConfig.Database)
	tokens := auth.NewTokenManager(settings.JWTSecret, settings.JWTIssuer, settings.JWTAudience, settings.JWTTTL)
	service, err := auth.NewService(auth.NewPostgresRepository(pool), tokens)
	if err != nil {
		return err
	}
	var customerOptions []customer.Option
	if settings.ImgBBAPIKey != "" {
		customerOptions = append(customerOptions, customer.WithImageUploader(imgbb.NewClient(settings.ImgBBAPIKey)))
	}
	customerService := customer.NewService(customer.NewPostgresRepository(pool), customerOptions...)
	server := &http.Server{
		Addr:              settings.HTTPAddress,
		Handler:           httpapi.NewRouter(service, logger, pool.Ping, settings.CORSOrigin, httpapi.WithCustomerService(customerService)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("HTTP server starting", "address", settings.HTTPAddress)
		serverErrors <- server.ListenAndServe()
	}()
	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		stop()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			_ = server.Close()
			return err
		}
		logger.Info("HTTP server stopped")
		return nil
	}
}
