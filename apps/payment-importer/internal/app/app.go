// Package app wires the payment-importer's dependencies. It only orchestrates:
// peek the envelope's provider, then hand the raw bytes to ImportService, which
// parses and persists. No provider-specific logic lives here.
package app

import (
	"context"

	"github.com/emerson/emerbot/apps/payment-importer/internal/envelope"
	"github.com/emerson/emerbot/packages/payments"
	"github.com/emerson/emerbot/packages/payments/pagbank"
)

// App holds the import service backed by a repository.
type App struct {
	svc *payments.ImportService
}

// New builds an App over the given repository, registering every provider parser.
func New(repo payments.Repository) *App {
	parsers := map[payments.Provider]payments.Parser{
		payments.ProviderPagBank: pagbank.New(),
	}
	return &App{svc: payments.NewImportService(parsers, repo)}
}

// ProcessRaw peeks the envelope's provider header, then parses and persists it.
func (a *App) ProcessRaw(ctx context.Context, raw []byte) error {
	meta, err := envelope.ReadMetadata(raw)
	if err != nil {
		return err
	}
	return a.svc.Process(ctx, meta.Provider, raw)
}
