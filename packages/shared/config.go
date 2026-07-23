package shared

import (
	"log/slog"
	"os"
	"strconv"
)

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
