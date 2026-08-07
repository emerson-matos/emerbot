package finance

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

// CategoryReader is the slice of the store the category-aware tools need. The
// catalog is per user — the defaults are the same for everyone, but the
// categories somebody added are theirs — so every one of them reads it rather
// than deriving the valid set from the domain definitions alone.
type CategoryReader interface {
	ListCategories(ctx context.Context, userID string) ([]domain.Category, error)
}

// CategoryStore is what create_category needs: the catalog to check against,
// and somewhere to put the new one.
type CategoryStore interface {
	CategoryReader
	SaveCategory(ctx context.Context, cat domain.Category) error
}

// catalog is the set of categories a user may file an entry under: the
// defaults defined in the codebase, plus whatever that user has created, with
// the user's own definition winning on a shared slug.
//
// The defaults are merged in rather than assumed to be stored, because they are
// seeded lazily — a user who has only ever talked to the bot has an empty
// CAT# partition, and validating against the store alone would reject
// "aluguel" for them.
type catalog struct {
	bySlug map[string]domain.Category
}

// loadCatalog reads the user's categories and merges them over the defaults.
func loadCatalog(ctx context.Context, r CategoryReader, userID string) (catalog, error) {
	stored, err := r.ListCategories(ctx, userID)
	if err != nil {
		return catalog{}, fmt.Errorf("list categories: %w", err)
	}
	c := catalog{bySlug: make(map[string]domain.Category, len(stored)+18)}
	for _, cat := range domain.DefaultCategories(userID) {
		c.bySlug[cat.Slug] = cat
	}
	for _, cat := range stored {
		c.bySlug[cat.Slug] = cat
	}
	return c, nil
}

// has reports whether slug names a category this user can file under.
func (c catalog) has(slug string) bool {
	_, ok := c.bySlug[slug]
	return ok
}

// slugs lists the categories of one entry type, alphabetically. It is what a
// rejected tool call hands back, so the model can pick a real one instead of
// guessing again.
func (c catalog) slugs(t domain.EntryType) []string {
	out := make([]string, 0, len(c.bySlug))
	for slug, cat := range c.bySlug {
		if cat.Type == t {
			out = append(out, slug)
		}
	}
	sort.Strings(out)
	return out
}

// labels indexes the catalog by slug, for the breakdowns that name a category
// to the user. A category the user renamed is named the way they renamed it.
func (c catalog) labels() map[string]string {
	labels := make(map[string]string, len(c.bySlug))
	for slug, cat := range c.bySlug {
		labels[slug] = cat.Label
	}
	return labels
}

// list returns every category, expenses first and alphabetical within each
// type — the order list_categories reports them in.
func (c catalog) list() []domain.Category {
	out := make([]domain.Category, 0, len(c.bySlug))
	for _, cat := range c.bySlug {
		out = append(out, cat)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type == domain.EntryTypeExpense
		}
		return out[i].Slug < out[j].Slug
	})
	return out
}

// validate resolves the category a tool argument names against what the user
// actually has, refusing anything they cannot file this kind of entry under.
//
// The type has to match, not just the slug: "aluguel" is a real category and an
// income entry filed under it is still wrong, and it is wrong in a way that
// shows up later as an expense category with sales in it.
func (c catalog) validate(slug string, t domain.EntryType) error {
	cat, ok := c.bySlug[slug]
	if !ok {
		return errUnknownCategory(slug, t, c)
	}
	if cat.Type != t {
		return fmt.Errorf(
			"categoria %q é de %s e este lançamento é de %s. Use uma destas: %s",
			slug, entryTypeLabel(cat.Type), entryTypeLabel(t), strings.Join(c.slugs(t), ", "),
		)
	}
	return nil
}

// entryTypeLabel names an entry type in the language the error is written in.
func entryTypeLabel(t domain.EntryType) string {
	if t == domain.EntryTypeIncome {
		return "receita"
	}
	return "despesa"
}

// errUnknownCategory refuses an entry whose category does not exist, naming the
// ones that do.
//
// It used to be a silent coercion to "outros_despesas"/"outros_receitas": the
// category enum in the tool schema listed the defaults, so anything else was a
// hallucination and filing it under Outros lost nothing. That stopped being
// true when the categories became the user's to extend — the slug the model
// sends is now just as likely to be the "venda_varejo" somebody asked for one
// message ago, and answering "lançado" while filing it under Outros is how a
// pharmacy's wholesale and retail sales end up in the same bucket with nothing
// to show for it. The error goes back to the model, which picks a real category
// or creates the one it meant (see createCategoryTool) and retries.
func errUnknownCategory(slug string, t domain.EntryType, c catalog) error {
	return fmt.Errorf(
		"categoria %q não existe. Use uma destas: %s. Se o usuário quis mesmo uma categoria nova, crie com %s antes de lançar",
		slug, strings.Join(c.slugs(t), ", "), createCategoryToolName,
	)
}

// categoryArgDescription is what replaced the category enum in the entry tool
// schemas.
//
// The enum could only ever list the defaults — a schema is built once, at
// startup, and the categories are per user — so keeping it would have meant the
// model was structurally unable to send the "venda_varejo" the user asked for
// one message earlier. What takes its place is the default list (still the
// answer to most calls), plus the two tools that reach the rest.
var categoryArgDescription = fmt.Sprintf(
	"Slug da categoria, sempre uma que exista. As padrão são: %s. "+
		"O usuário pode ter outras — chame %s para ver a lista dele. "+
		"Se ele pedir um tipo que não existe (por exemplo separar venda atacado de "+
		"venda varejo), crie com %s antes de lançar. Nunca invente um slug: "+
		"um slug desconhecido faz o lançamento ser recusado.",
	strings.Join(defaultCategorySlugs(), ", "), listCategoriesToolName, createCategoryToolName,
)

// defaultCategorySlugs is the codebase's own category list, in the order it is
// defined.
func defaultCategorySlugs() []string {
	cats := domain.DefaultCategories("")
	slugs := make([]string, len(cats))
	for i, c := range cats {
		slugs[i] = c.Slug
	}
	return slugs
}

// maxUserCategories bounds how big one user's catalog can get, defaults
// included. A model that misreads "vendi 200 reais no atacado" as a request for
// a category creates one; the cap is high enough that nobody organising a
// pharmacy ever meets it, and low enough that a loop cannot fill a partition.
const maxUserCategories = 80

// maxEntryAmountReais bounds a single entry's value. Tool args are LLM-generated
// from user text; a hallucinated absurd amount is rejected rather than saved.
const maxEntryAmountReais = 10_000_000

// originArgDescription tells the model what the origin means and, crucially,
// that only "venda" is faturamento. Without the second half it happily files a
// loan as a sale, which is the whole mistake this field exists to prevent.
const originArgDescription = "Origem do dinheiro que entrou (só para type=income). " +
	"Use \"venda\" para qualquer venda de produto ou serviço — balcão, cartão, PIX ou crediário. " +
	"Use as outras para dinheiro que entrou sem venda: \"emprestimo\", \"aporte_socio\", " +
	"\"receita_financeira\" (rendimentos), \"restituicao\", \"outros\". " +
	"Apenas \"venda\" conta como faturamento e conta para a meta; as demais são só entrada de caixa. " +
	"Padrão: \"venda\"."

// createOriginSlugs is the origin enum offered to create_financial_entry.
//
// It deliberately omits OriginRecebimentoCliente. A customer settling a
// crediário sale is the *existing* pending entry being marked paid — the sale
// was already recorded on the day it happened. Offering the model an origin
// that sounds like "customer paid me" makes it create a second, paid entry for
// the same sale, and the money gets counted twice with nothing downstream able
// to detect it. That path is edit_financial_entry; see the rule in
// packages/orchestrator/internal/agentprompt. The origin stays reachable from
// the dashboard form, where a human can mean "a receivable that predates this
// ledger".
func createOriginSlugs() []string {
	slugs := make([]string, 0, len(domain.IncomeOrigins()))
	for _, o := range domain.IncomeOrigins() {
		if o == domain.OriginRecebimentoCliente {
			continue
		}
		slugs = append(slugs, string(o))
	}
	return slugs
}

// parseDate parses a "YYYY-MM-DD" string; ok is false for empty or malformed
// input so callers fall back to their default. Tool args come from LLM output,
// where "no date given" and "a date I could not read" both mean "use the
// default" — which is why this reports a bool rather than an error.
func parseDate(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := domain.ParseDay(s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// emptySummaries seeds one zero-valued summary per requested month and returns
// the calendar span they cover, so a multi-month aggregation can be served by
// a single date-range query. Duplicate months collapse into one entry.
//
// Requesting no months is not an error — it yields an empty map and a zero
// span, which callers short-circuit on rather than querying for nothing.
func emptySummaries(yearMonths []string) (map[string]MonthlySummary, time.Time, time.Time, error) {
	summaries := make(map[string]MonthlySummary, len(yearMonths))
	var from, to time.Time

	for _, ym := range yearMonths {
		start, end, err := domain.ParseMonth(ym)
		if err != nil {
			return nil, time.Time{}, time.Time{}, err
		}
		summaries[ym] = MonthlySummary{Month: ym}

		if from.IsZero() || start.Before(from) {
			from = start
		}
		if to.IsZero() || end.After(to) {
			to = end
		}
	}
	return summaries, from, to, nil
}

// accumulateSummaries folds one basis's entries into the summaries they belong
// to. The month an entry belongs to depends on the basis — a sale is keyed by
// the month it was made, cash by the month it landed, a bill by the month it
// falls due — so the same entry can land in two different months across two
// calls, which is the whole point. Entries outside the requested months are
// ignored.
func accumulateSummaries(summaries map[string]MonthlySummary, basis DateBasis, entries []domain.FinancialEntry) {
	for _, e := range entries {
		date, ok := basisDate(basis, e)
		if !ok {
			continue
		}
		key := domain.MonthOf(date)
		summary, ok := summaries[key]
		if !ok {
			continue
		}
		addToSummary(&summary, basis, []domain.FinancialEntry{e})
		summaries[key] = summary
	}
}

// RevenueTotal sums faturamento across the given entries: what was sold, by
// domain.IsRevenue. This is the figure every performance indicator uses.
//
// It does not filter by date — callers pass the entries of the period they
// mean, and the period has to have been read on the transaction basis (see
// DateBasis) for the total to be the month's real faturamento.
func RevenueTotal(entries []domain.FinancialEntry) int64 {
	var total int64
	for _, e := range entries {
		if domain.IsRevenue(e) {
			total += e.Amount
		}
	}
	return total
}

// RevenueByCategory breaks faturamento down by category, largest first: which
// *kinds* of sale a period's sales were.
//
// Origin still decides what counts as a sale (domain.IsRevenue, ADR-016) — the
// category never does, and a category called "Empréstimos" would no more make
// an entry faturamento here than anywhere else. What the category answers is
// the question after that one: of what the pharmacy sold, how much was atacado
// and how much was balcão.
//
// Like RevenueTotal it does not filter by date: callers pass the entries of the
// period they mean, read on the transaction basis, or the split will disagree
// with the total it is meant to decompose.
func RevenueByCategory(entries []domain.FinancialEntry, labels map[string]string) []CategorySummary {
	return foldByCategory(entries, labels, domain.IsRevenue)
}

// CashInTotal sums entradas de caixa: every inflow that has actually been
// received, whatever its origin. Pending entries are excluded — money that has
// not arrived is not cash.
func CashInTotal(entries []domain.FinancialEntry) int64 {
	var total int64
	for _, e := range entries {
		if e.Type == domain.EntryTypeIncome && CashDate(e) != nil {
			total += e.Amount
		}
	}
	return total
}

// defaultEntryLimit / maxEntryLimit bound how many rows a listing tool hands
// back to the model. The default used to be 20, which silently cut a normal
// month of contas a pagar in half — see listing() for why the cut is now
// visible, and why these are sized to fit a whole month of a small pharmacy's
// bills rather than a screenful.
const (
	defaultEntryLimit = 50
	maxEntryLimit     = 200
)

func clampLimit(n int) int {
	if n <= 0 {
		return defaultEntryLimit
	}
	if n > maxEntryLimit {
		return maxEntryLimit
	}
	return n
}

// CategoryLabel names a category the way its owner named it, falling back to
// the default definitions and then to the slug title-cased. Nil labels is the
// caller saying it has no catalog at hand, not that the user has no categories.
//
// The fallback matters more than it used to: every category beyond the
// defaults is one somebody typed a label for, and printing "venda_atacado" at
// them where the catalog could not be read is worse than printing "Venda
// Atacado".
func CategoryLabel(labels map[string]string, slug string) string {
	if label, ok := labels[slug]; ok && label != "" {
		return label
	}
	for _, c := range domain.DefaultCategories("") {
		if c.Slug == slug {
			return c.Label
		}
	}
	return domain.CategoryLabelFromSlug(slug)
}

// foldByCategory totals entries per category, largest total first. It is the
// one definition of "agrupado por categoria", shared by CategorySummary, by the
// listing tools' by_category block and by RevenueByCategory.
//
// keep decides which entries are in the fold; nil counts every one of them.
func foldByCategory(entries []domain.FinancialEntry, labels map[string]string, keep func(domain.FinancialEntry) bool) []CategorySummary {
	totals := make(map[string]*CategorySummary)
	for _, e := range entries {
		if keep != nil && !keep(e) {
			continue
		}
		if _, ok := totals[e.Category]; !ok {
			totals[e.Category] = &CategorySummary{
				Category: e.Category,
				Label:    CategoryLabel(labels, e.Category),
				Type:     e.Type,
			}
		}
		totals[e.Category].Total += e.Amount
		totals[e.Category].Count++
	}

	result := make([]CategorySummary, 0, len(totals))
	for _, v := range totals {
		result = append(result, *v)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Total != result[j].Total {
			return result[i].Total > result[j].Total
		}
		return result[i].Category < result[j].Category
	})
	return result
}

// reaisToCentavos converts a reais amount to integer centavos, rounding to the
// nearest centavo to avoid float truncation (e.g. 19.99 → 1999, not 1998).
func reaisToCentavos(reais float64) int64 {
	if reais < 0 {
		return -int64(-reais*100 + 0.5)
	}
	return int64(reais*100 + 0.5)
}

func centavosToReais(centavos int64) float64 {
	return float64(centavos) / 100
}

// categoryToolPayload renders a per-category fold for the model: amounts in
// reais, and the label beside the slug so a reply can name the category instead
// of reading "venda_atacado" out loud.
func categoryToolPayload(cats []CategorySummary) []map[string]any {
	out := make([]map[string]any, 0, len(cats))
	for _, c := range cats {
		out = append(out, map[string]any{
			"category": c.Category,
			"label":    c.Label,
			"type":     string(c.Type),
			"total":    centavosToReais(c.Total),
			"count":    c.Count,
		})
	}
	return out
}

func entriesToMaps(entries []domain.FinancialEntry) []map[string]any {
	results := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		results = append(results, entryToMap(e))
	}
	return results
}

func entryToMap(e domain.FinancialEntry) map[string]any {
	m := map[string]any{
		"entry_id":    e.EntryID,
		"type":        string(e.Type),
		"amount":      centavosToReais(e.Amount),
		"category":    e.Category,
		"description": e.Description,
		"date":        e.TransactionDate.String(),
		"status":      string(e.PaymentStatus),
	}
	if e.DueDate != nil {
		m["due_date"] = e.DueDate.Format("2006-01-02")
	}
	return m
}
