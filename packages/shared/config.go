package shared

import (
	"log/slog"
	"os"
	"strconv"
	"time"
)

// DefaultTimezone is the pharmacy's local calendar. Everything that reasons
// about "today", "this week" or "days remaining" has to agree on it — a UTC
// instant is a day ahead for part of every evening in Brazil, which would put
// the dashboard, the digest and the bot on different days.
const DefaultTimezone = "America/Sao_Paulo"

// Location loads the timezone from key, falling back to DefaultTimezone and,
// if even that is unavailable (no zoneinfo in the Lambda image — import
// _ "time/tzdata" to embed it), to UTC. It never fails: a wrong-by-hours clock
// is better than a service that refuses to start.
func Location(key string) *time.Location {
	name := Getenv(key, DefaultTimezone)
	loc, err := time.LoadLocation(name)
	if err != nil {
		slog.Warn("falling back to UTC", "timezone", name, "error", err)
		return time.UTC
	}
	return loc
}

// FinanceLedgerID is a TEMPORARY shared finance partition key: the WhatsApp bot
// and every dashboard user read/write this single ledger. It is a hardcoded
// constant (not env-driven) so local and Lambda cannot diverge.
// TODO: replace with real phone→account linking.
const FinanceLedgerID = "shared-ledger"

func Getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func InitSlog() {
	level := slog.LevelInfo
	if Getenv("LOG_LEVEL", "") == "debug" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}

func GetenvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
