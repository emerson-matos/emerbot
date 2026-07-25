// Package importer wires the import pipeline: peek the envelope's provider, then
// hand the raw bytes to ImportService, which parses and persists. It holds no
// provider-specific logic.
//
// It lives in packages/ rather than under an app's internal/ on purpose. Both
// entry points — the Lambda triggered by S3 and the pagbank-import script run
// from a laptop — build this same Importer, so "local" and "deployed" cannot
// drift into two subtly different import behaviours. Anything that would only be
// true on one of those paths does not belong here.
package importer

import (
	"context"

	"github.com/emerson/emerbot/packages/payments"
	"github.com/emerson/emerbot/packages/payments/pagbank"
)

// Importer holds the import service backed by a repository.
type Importer struct {
	svc *payments.ImportService
}

// New builds an Importer over the given repository, registering every provider parser.
func New(repo payments.Repository) *Importer {
	parsers := map[payments.Provider]payments.Parser{
		payments.ProviderPagBank: pagbank.New(),
	}
	return &Importer{svc: payments.NewImportService(parsers, repo)}
}

// ProcessRaw peeks the envelope's provider header, then parses and persists it.
func (a *Importer) ProcessRaw(ctx context.Context, raw []byte) error {
	meta, err := ReadMetadata(raw)
	if err != nil {
		return err
	}
	return a.svc.Process(ctx, meta.Provider, raw)
}
