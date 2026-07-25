package pagbank

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/payments"
)

// Assembling the envelope lives here, next to the parser, because both encode
// the same knowledge of PagBank's wire format — the shape of an EDI response and
// which of its fields are dates. Splitting them would let the producer and the
// consumer of an envelope drift apart.

// ExtractKind identifies which EDI extract a response body came from.
type ExtractKind string

const (
	KindTransactional ExtractKind = "transactional"
	KindFinancial     ExtractKind = "financial"
	KindCashouts      ExtractKind = "cashouts"
)

// ClassifyFile infers an extract kind from an EDI response filename, matching
// how PagBank's own test scenarios name them
// ("cenario1_response_transactional_1.json"). It reports false for a name that
// matches none, so callers can skip unrelated files rather than guess.
func ClassifyFile(name string) (ExtractKind, bool) {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "transactional"):
		return KindTransactional, true
	case strings.Contains(lower, "financial"):
		return KindFinancial, true
	case strings.Contains(lower, "cashout"):
		return KindCashouts, true
	default:
		return "", false
	}
}

// Extract is one raw EDI API response body plus the extract it came from.
type Extract struct {
	Kind ExtractKind
	Name string // for error messages only
	Data []byte
}

// AssembleOptions controls envelope assembly.
type AssembleOptions struct {
	// SourceDate stamps the envelope's import day, which is the unit the
	// repository replaces. Zero means "derive it from the data" (the latest
	// business date present), so assembling the same files twice is idempotent.
	SourceDate domain.CalendarDate

	// RebaseTo shifts every date in the payload so the newest one lands in the
	// given "YYYY-MM" month, preserving day-of-month and every relative gap
	// between dates. This exists for demos and manual testing: the recorded
	// scenarios are pinned to 2024, and a dashboard that only ever shows the
	// current month would render them as an empty page. Empty means no shift.
	RebaseTo string

	// ImportedAt is audit metadata on the envelope. Zero means time.Now().
	ImportedAt time.Time
}

// envelopeOut is the combined envelope the importer consumes. It mirrors the
// `envelope` type the parser decodes — see parser.go.
type envelopeOut struct {
	Provider      string          `json:"provider"`
	ImportedAt    time.Time       `json:"importedAt"`
	Date          string          `json:"date"`
	Transactional detalhesWrapper `json:"transactional"`
	Financial     detalhesWrapper `json:"financial"`
	// Cashouts is carried through untouched. Nothing parses it yet (cash
	// withdrawals are not part of the canonical domain), but the envelope is the
	// archived record of an import, so dropping data on the floor here would
	// make that record incomplete.
	Cashouts detalhesWrapper `json:"cashouts"`
}

type detalhesWrapper struct {
	Detalhes []map[string]any `json:"detalhes"`
}

// Assemble merges EDI response bodies into one combined envelope.
func Assemble(extracts []Extract, opts AssembleOptions) ([]byte, error) {
	byKind := map[ExtractKind][]map[string]any{}
	for _, e := range extracts {
		records, err := decodeDetalhes(e.Data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name, err)
		}
		byKind[e.Kind] = append(byKind[e.Kind], records...)
	}

	if opts.RebaseTo != "" {
		if err := rebaseRecords(byKind, opts.RebaseTo); err != nil {
			return nil, err
		}
	}

	sourceDate := opts.SourceDate
	if !sourceDate.Valid() {
		latest, ok := latestDate(byKind)
		if !ok {
			return nil, fmt.Errorf("no dated records found: cannot derive the envelope's source date (pass it explicitly)")
		}
		sourceDate = latest
	}

	importedAt := opts.ImportedAt
	if importedAt.IsZero() {
		importedAt = time.Now().UTC()
	}

	env := envelopeOut{
		Provider:      string(payments.ProviderPagBank),
		ImportedAt:    importedAt.UTC(),
		Date:          sourceDate.String(),
		Transactional: detalhesWrapper{Detalhes: orEmpty(byKind[KindTransactional])},
		Financial:     detalhesWrapper{Detalhes: orEmpty(byKind[KindFinancial])},
		Cashouts:      detalhesWrapper{Detalhes: orEmpty(byKind[KindCashouts])},
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(env); err != nil {
		return nil, fmt.Errorf("encode envelope: %w", err)
	}
	return buf.Bytes(), nil
}

func orEmpty(records []map[string]any) []map[string]any {
	if records == nil {
		return []map[string]any{}
	}
	return records
}

// decodeDetalhes reads an EDI API response, accepting both the real response
// object `{"detalhes": [...]}` and a bare array. Numbers are decoded as
// json.Number so amounts round-trip through the envelope byte for byte instead
// of going via float64.
func decodeDetalhes(raw []byte) ([]map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	var wrapper struct {
		Detalhes []map[string]any `json:"detalhes"`
	}
	if err := dec.Decode(&wrapper); err == nil {
		if wrapper.Detalhes != nil {
			return wrapper.Detalhes, nil
		}
	}

	dec = json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var bare []map[string]any
	if err := dec.Decode(&bare); err != nil {
		return nil, fmt.Errorf("decode EDI response: %w", err)
	}
	return bare, nil
}

// isoDateRE matches the "YYYY-MM-DD" values EDI date fields carry.
var isoDateRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// dateFields walks every `data_*` field holding an ISO date. Matching on the
// field-name prefix rather than an explicit list means a date field this code
// has never heard of (a new extract, a new EDI version) is still shifted by
// RebaseTo, instead of being silently left in the past.
func dateFields(record map[string]any, visit func(key, value string)) {
	keys := make([]string, 0, len(record))
	for k := range record {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic iteration
	for _, k := range keys {
		if !strings.HasPrefix(k, "data_") {
			continue
		}
		s, ok := record[k].(string)
		if !ok || !isoDateRE.MatchString(s) {
			continue
		}
		visit(k, s)
	}
}

// latestDate returns the newest business date across every record.
func latestDate(byKind map[ExtractKind][]map[string]any) (domain.CalendarDate, bool) {
	var latest time.Time
	for _, records := range byKind {
		for _, rec := range records {
			dateFields(rec, func(_, value string) {
				if t, err := time.Parse("2006-01-02", value); err == nil && t.After(latest) {
					latest = t
				}
			})
		}
	}
	if latest.IsZero() {
		return domain.CalendarDate{}, false
	}
	return domain.NewCalendarDate(latest), true
}

// rebaseRecords shifts every date by a whole number of *months*, chosen so the
// newest date lands in month.
//
// Months rather than days because PagBank's installments are monthly: a sale on
// the 30th settles on the 30th of the following months. A fixed day-count shift
// would preserve exact intervals but drift the day-of-month, turning a clean
// monthly plan into something no real extract looks like.
func rebaseRecords(byKind map[ExtractKind][]map[string]any, month string) error {
	target, err := time.Parse("2006-01", month)
	if err != nil {
		return fmt.Errorf("invalid rebase month %q, expected YYYY-MM: %w", month, err)
	}
	latest, ok := latestDate(byKind)
	if !ok {
		return fmt.Errorf("no dated records found: nothing to rebase")
	}

	from := latest.UTC()
	months := (target.Year()-from.Year())*12 + int(target.Month()) - int(from.Month())

	for _, records := range byKind {
		for _, rec := range records {
			dateFields(rec, func(key, value string) {
				t, err := time.Parse("2006-01-02", value)
				if err != nil {
					return
				}
				rec[key] = addMonths(t, months).Format("2006-01-02")
			})
		}
	}
	return nil
}

// addMonths shifts t by n months, clamping the day to the target month's last
// day. time.AddDate would instead overflow — 31 January plus one month becomes
// 3 March — which would reorder installments relative to each other.
func addMonths(t time.Time, n int) time.Time {
	year, month, day := t.Date()
	firstOfTarget := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC).AddDate(0, n, 0)
	lastDay := firstOfTarget.AddDate(0, 1, -1).Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(firstOfTarget.Year(), firstOfTarget.Month(), day, 0, 0, 0, 0, time.UTC)
}
