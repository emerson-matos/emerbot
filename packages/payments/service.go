package payments

import (
	"context"
	"errors"
	"fmt"

	"github.com/emerson/emerbot/packages/domain"
)

// ImportResult is the canonical output of parsing one envelope. Its header
// (Provider + SourceDate) makes "a single logical import" a first-class value:
// every item below belongs to this one provider and this one source day.
type ImportResult struct {
	Provider    Provider
	SourceDate  domain.CalendarDate
	Sales       []Sale
	Receivables []ExpectedReceivable
	Payments    []Payment
}

// Parser translates one provider's raw envelope bytes into the canonical model.
// It is intentionally context-free: a parser is a pure, deterministic,
// cancel-nothing translator — no clock, DB, network, or business logic. Each
// provider owns its own envelope type, so adding a provider never changes this
// signature or ImportService.
type Parser interface {
	Parse(raw []byte) (ImportResult, error)
}

// Repository persists canonical data. It owns the storage layout and the
// replace strategy entirely — callers never see PK/SK/BatchWrite. Save is
// idempotent: it replaces exactly the prior (Provider, SourceDate) set.
type Repository interface {
	Save(ctx context.Context, result ImportResult) error
	ListSales(ctx context.Context, from, to domain.CalendarDate) ([]Sale, error)
	ListReceivables(ctx context.Context, from, to domain.CalendarDate) ([]ExpectedReceivable, error)
	ListPayments(ctx context.Context, from, to domain.CalendarDate) ([]Payment, error)
}

var (
	// ErrProviderMismatch is returned when the provider used to route to a
	// parser disagrees with the provider the parser read from the envelope body.
	ErrProviderMismatch = errors.New("payments: envelope provider does not match routing provider")
	// ErrUnknownProvider is returned when no parser is registered for a provider.
	ErrUnknownProvider = errors.New("payments: no parser registered for provider")
)

// ValidateImportResult enforces the single-logical-import invariant: exactly one
// provider (matching the header) and one SourceDate across every item. A parser
// bug that mixes providers or source days is rejected here, before any write.
func ValidateImportResult(r ImportResult) error {
	if !r.Provider.Valid() {
		return fmt.Errorf("invalid provider %q", r.Provider)
	}
	if !r.SourceDate.Valid() {
		return errors.New("import result source date is required")
	}
	for _, s := range r.Sales {
		if s.Provider != r.Provider {
			return fmt.Errorf("sale %q provider %q != import provider %q", s.ID, s.Provider, r.Provider)
		}
		if err := s.Validate(); err != nil {
			return err
		}
	}
	for _, rc := range r.Receivables {
		if rc.Provider != r.Provider {
			return fmt.Errorf("receivable %q provider %q != import provider %q", rc.SaleID, rc.Provider, r.Provider)
		}
		if err := rc.Validate(); err != nil {
			return err
		}
	}
	for _, p := range r.Payments {
		if p.Provider != r.Provider {
			return fmt.Errorf("payment %q provider %q != import provider %q", p.SaleID, p.Provider, r.Provider)
		}
		if err := p.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// ImportService orchestrates a single import: route to the provider's parser,
// parse, then persist. It contains no provider-specific logic and no I/O beyond
// the injected Repository — the raw bytes may arrive from S3, HTTP, EventBridge
// or SQS without changing anything here.
type ImportService struct {
	parsers map[Provider]Parser
	repo    Repository
}

// NewImportService wires a provider→parser registry and a repository.
func NewImportService(parsers map[Provider]Parser, repo Repository) *ImportService {
	return &ImportService{parsers: parsers, repo: repo}
}

// Process parses raw for the routing provider and persists the result. It
// guards against a malformed envelope or a wrong parser by asserting the parsed
// provider matches the routing provider before anything is persisted.
func (s *ImportService) Process(ctx context.Context, provider Provider, raw []byte) error {
	parser, ok := s.parsers[provider]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownProvider, provider)
	}
	result, err := parser.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse %s envelope: %w", provider, err)
	}
	if result.Provider != provider {
		return fmt.Errorf("%w: routed %q, envelope %q", ErrProviderMismatch, provider, result.Provider)
	}
	if err := s.repo.Save(ctx, result); err != nil {
		return fmt.Errorf("save %s import: %w", provider, err)
	}
	return nil
}
