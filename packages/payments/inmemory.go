package payments

import (
	"context"
	"sort"
	"sync"

	"github.com/emerson/emerbot/packages/domain"
)

// InMemoryRepository implements Repository for tests and local development
// without DynamoDB. It keeps the same replace semantics as DynamoDBStore so the
// two behave identically.
type InMemoryRepository struct {
	mu          sync.RWMutex
	sales       []Sale
	receivables []ExpectedReceivable
	payments    []Payment
	sourceDate  map[any]domain.CalendarDate // item pointer identity → its import's SourceDate
}

// NewInMemoryRepository creates an empty in-memory repository.
func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{sourceDate: make(map[any]domain.CalendarDate)}
}

// Save validates the single-logical-import invariant, then replaces exactly the
// prior (Provider, SourceDate) set with result's items — so re-importing a day
// (even one that drops a previously-imported future receivable) is idempotent.
func (r *InMemoryRepository) Save(_ context.Context, result ImportResult) error {
	if err := ValidateImportResult(result); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	keep := func(p Provider, sd domain.CalendarDate) bool {
		return p != result.Provider || !sd.Equal(result.SourceDate)
	}

	sales := r.sales[:0:0]
	for _, s := range r.sales {
		if keep(s.Provider, r.sourceDate[saleKey(s)]) {
			sales = append(sales, s)
		} else {
			delete(r.sourceDate, saleKey(s))
		}
	}
	recv := r.receivables[:0:0]
	for _, rc := range r.receivables {
		if keep(rc.Provider, r.sourceDate[recvKey(rc)]) {
			recv = append(recv, rc)
		} else {
			delete(r.sourceDate, recvKey(rc))
		}
	}
	pays := r.payments[:0:0]
	for _, p := range r.payments {
		if keep(p.Provider, r.sourceDate[payKey(p)]) {
			pays = append(pays, p)
		} else {
			delete(r.sourceDate, payKey(p))
		}
	}

	for _, s := range result.Sales {
		sales = append(sales, s)
		r.sourceDate[saleKey(s)] = result.SourceDate
	}
	for _, rc := range result.Receivables {
		recv = append(recv, rc)
		r.sourceDate[recvKey(rc)] = result.SourceDate
	}
	for _, p := range result.Payments {
		pays = append(pays, p)
		r.sourceDate[payKey(p)] = result.SourceDate
	}

	r.sales, r.receivables, r.payments = sales, recv, pays
	return nil
}

func (r *InMemoryRepository) ListSales(_ context.Context, from, to domain.CalendarDate) ([]Sale, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Sale
	for _, s := range r.sales {
		if inRange(s.SaleDate, from, to) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SaleDate.Before(out[j].SaleDate) })
	return out, nil
}

func (r *InMemoryRepository) ListReceivables(_ context.Context, from, to domain.CalendarDate) ([]ExpectedReceivable, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []ExpectedReceivable
	for _, rc := range r.receivables {
		if inRange(rc.ExpectedDate, from, to) {
			out = append(out, rc)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ExpectedDate.Before(out[j].ExpectedDate) })
	return out, nil
}

func (r *InMemoryRepository) ListPayments(_ context.Context, from, to domain.CalendarDate) ([]Payment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Payment
	for _, p := range r.payments {
		if inRange(p.PaymentDate, from, to) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PaymentDate.Before(out[j].PaymentDate) })
	return out, nil
}

// inRange reports whether d falls within [from, to] inclusive.
func inRange(d, from, to domain.CalendarDate) bool {
	return !d.Before(from) && !d.After(to)
}

// The *Key helpers produce a value identity for an item so the in-memory store
// can track which import each item came from, mirroring the DynamoDB SK.
func saleKey(s Sale) string { return saleSK(s.SaleDate, s.ID) }

func recvKey(r ExpectedReceivable) string {
	return recvSK(r.ExpectedDate, r.SaleID, r.InstallmentNumber)
}
func payKey(p Payment) string { return paySK(p.PaymentDate, p.SaleID) }
