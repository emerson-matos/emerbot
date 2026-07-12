package main

import (
	"context"
	"log"

	"golang.org/x/crypto/bcrypt"

	"github.com/emerson/emerbot/packages/auth"
	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/shared"
)

// seedUsers ensures the two hardcoded users exist in the store.
// Passwords are read from env vars so they are never in source code.
func seedUsers(ctx context.Context, store auth.Store) {
	users := []struct {
		id       string
		email    string
		name     string
		envPwd   string
		fallback string
	}{
		{"emerson", shared.Getenv("USER_EMERSON_EMAIL", "emerson@farmacia.local"), "Emerson", "USER_EMERSON_PASSWORD", "senha123"},
		{"pai", shared.Getenv("USER_PAI_EMAIL", "pai@farmacia.local"), "Pai", "USER_PAI_PASSWORD", "senha123"},
	}

	for _, u := range users {
		if _, err := store.GetUserByID(ctx, u.id); err == nil {
			continue // already exists
		}
		pwd := shared.Getenv(u.envPwd, u.fallback)
		hash, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("warn: could not hash password for %s: %v", u.id, err)
			continue
		}
		if err := store.SaveUser(ctx, domain.User{
			UserID:       u.id,
			Email:        u.email,
			PasswordHash: string(hash),
			Name:         u.name,
		}); err != nil {
			log.Printf("warn: could not seed user %s: %v", u.id, err)
		} else {
			log.Printf("seeded user: %s (%s)", u.name, u.email)
		}
	}
}
