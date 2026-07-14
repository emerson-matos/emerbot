// Command create-user creates (or overwrites) a dashboard user in the
// configured DynamoDB users table. Password is bcrypt-hashed; if none is given
// a random one is generated and printed once.
//
// Table names + endpoint come from the environment (same vars the apps use):
//
//	USERS_TABLE, REFRESH_TOKENS_TABLE, DYNAMODB_ENDPOINT (empty for real AWS).
//
//	Example (local): DYNAMODB_ENDPOINT=http://localhost:8000 \
//		USERS_TABLE=emerbot-local-users REFRESH_TOKENS_TABLE=emerbot-local-refresh-tokens \
//		go run ./scripts/create-user -email demo@user.com
package main

import (
	"context"
	"crypto/rand"
	"flag"
	"log"
	"math/big"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/emerson/emerbot/packages/auth"
	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/shared"
)

// passwordAlphabet excludes visually ambiguous characters (0/O, 1/l/I).
const passwordAlphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func main() {
	email := flag.String("email", "", "user email (required)")
	name := flag.String("name", "", "display name (defaults to the email local-part)")
	userID := flag.String("user-id", "", "user id / PK suffix (defaults to the email local-part)")
	password := flag.String("password", "", "password (default: generate a random one)")
	length := flag.Int("length", 8, "generated password length")
	force := flag.Bool("force", false, "overwrite if a user with this email already exists")
	flag.Parse()

	if strings.TrimSpace(*email) == "" {
		log.Fatal("-email is required")
	}

	usersTable := shared.Getenv("USERS_TABLE", "")
	tokensTable := shared.Getenv("REFRESH_TOKENS_TABLE", "")
	endpoint := shared.Getenv("DYNAMODB_ENDPOINT", "")
	if usersTable == "" || tokensTable == "" {
		log.Fatal("USERS_TABLE and REFRESH_TOKENS_TABLE must be set")
	}

	localPart, _, _ := strings.Cut(*email, "@")
	id := firstNonEmpty(*userID, sanitizeID(localPart))
	displayName := firstNonEmpty(*name, localPart)

	pwd := *password
	generated := false
	if pwd == "" {
		var err error
		if pwd, err = randomPassword(*length); err != nil {
			log.Fatalf("generate password: %v", err)
		}
		generated = true
	}

	ctx := context.Background()
	store, err := auth.NewDynamoDBStore(ctx, usersTable, tokensTable, endpoint)
	if err != nil {
		log.Fatalf("connect store: %v", err)
	}

	if !*force {
		if _, err := store.GetUserByEmail(ctx, *email); err == nil {
			log.Fatalf("user with email %q already exists (use -force to overwrite)", *email)
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("hash password: %v", err)
	}

	if err := store.SaveUser(ctx, domain.User{
		UserID:       id,
		Email:        *email,
		PasswordHash: string(hash),
		Name:         displayName,
	}); err != nil {
		log.Fatalf("save user: %v", err)
	}

	log.Printf("created user: id=%s email=%s name=%q table=%s", id, *email, displayName, usersTable)
	if generated {
		// Printed to stdout (not the log) and shown once — record it now.
		log.Printf("password for %s: %s", *email, pwd)
	}
}

func randomPassword(n int) (string, error) {
	if n < 1 {
		n = 8
	}
	b := make([]byte, n)
	max := big.NewInt(int64(len(passwordAlphabet)))
	for i := range b {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = passwordAlphabet[idx.Int64()]
	}
	return string(b), nil
}

func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' || r == '_' || r == '-' || r == '+':
			b.WriteRune('-')
		}
	}
	return b.String()
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
