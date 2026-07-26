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

// PharmacyLocation loads the one timezone the whole system reasons about days
// in, from APP_TIMEZONE. There is deliberately no per-service variable: the
// dashboard, the WhatsApp digest and the bot all answer questions about "hoje",
// "esta semana" and "dias restantes", and separate knobs would let them
// disagree about what day it is.
//
// NOTIFIER_TIMEZONE is still honoured as a fallback because it is the name that
// shipped first — a deploy that has not picked up APP_TIMEZONE yet keeps its
// configured timezone instead of silently jumping to UTC.
//
// It never fails: a clock that is wrong by hours beats a service that refuses
// to start. An unloadable zone (no zoneinfo in the Lambda image — import
// _ "time/tzdata" to embed it) degrades to UTC with a warning.
func PharmacyLocation() *time.Location {
	name := Getenv("APP_TIMEZONE", Getenv("NOTIFIER_TIMEZONE", DefaultTimezone))
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
