// Command migrate-origin backfills the Origin attribute on existing entries and
// rewrites GSI1 to its new meaning.
//
// Two things changed under the entries table and both need existing rows
// rewritten:
//
//  1. Income entries gained an Origin attribute (domain.IncomeOrigin).
//     Faturamento is now "origin == venda" instead of "category is anything but
//     outros_receitas". Until every row carries one, domain.IsRevenue falls back
//     to the old category rule — this command is what lets that shim be deleted.
//
//  2. GSI1 stopped meaning "<category>#<date>", which nothing ever queried, and
//     became the sparse cash-date index "<paymentDate>#<entryID>". Rows written
//     by older code still carry the category-shaped value, which sorts above any
//     date and so silently matches nothing in a date range — an under-count with
//     no error attached, which is why this runs before any read is switched over.
//
// The origin mapping deliberately reproduces the pre-migration faturamento
// exactly, so no figure moves on the day it runs: the improvement comes from
// newly written entries, which carry a real origin. Verify by comparing the
// month totals before and after; they must be identical.
//
// Safe to re-run: every write is idempotent, and --dry-run reports what would
// change without touching anything.
//
// Usage:
//
//	go run ./scripts/migrate-origin --dry-run
//	go run ./scripts/migrate-origin
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/dynamostore"
	"github.com/emerson/emerbot/packages/shared"
)

// legacyRevenueCategory is the category that used to stand in for "not a sale".
// It has to be spelled out here rather than read from domain, because this
// command is the last thing that should ever know about the old rule.
const legacyRevenueCategory = "outros_receitas"

func main() {
	endpoint := flag.String("endpoint", shared.Getenv("DYNAMODB_ENDPOINT", "http://localhost:8000"), "DynamoDB endpoint (empty = real AWS)")
	table := flag.String("table", shared.Getenv("FINANCIAL_ENTRIES_TABLE", "emerbot-local-financial-entries"), "financial entries table name")
	dryRun := flag.Bool("dry-run", false, "report what would change without writing")
	report := flag.Bool("report", false, "print how the ledger's inflows are classified and exit, writing nothing")
	flag.Parse()

	ctx := context.Background()
	client, err := dynamostore.NewClient(ctx, *endpoint)
	if err != nil {
		log.Fatalf("create client: %v", err)
	}

	if *report {
		if err := printReport(ctx, client, *table); err != nil {
			log.Fatalf("report: %v", err)
		}
		return
	}

	stats, err := migrate(ctx, client, *table, *dryRun)
	if err != nil {
		log.Fatalf("migrate: %v", err)
	}

	verb := "updated"
	if *dryRun {
		verb = "would update"
	}
	log.Printf("scanned %d entries; %s %d (origin: %d, gsi1: %d); %d already current",
		stats.scanned, verb, stats.changed, stats.originSet, stats.gsi1Rewritten, stats.unchanged)
	if *dryRun && stats.changed > 0 {
		log.Printf("re-run without --dry-run to apply")
	}
}

type stats struct {
	scanned       int
	changed       int
	originSet     int
	gsi1Rewritten int
	unchanged     int
}

// entryRow is the subset of an entry item this command reads. It is a local
// shape rather than finance.entryItem so that a later change to the stored
// entry cannot silently alter what this one-off rewrites.
type entryRow struct {
	PK            string `dynamodbav:"PK"`
	SK            string `dynamodbav:"SK"`
	EntryID       string `dynamodbav:"EntryID"`
	Type          string `dynamodbav:"Type"`
	Category      string `dynamodbav:"Category"`
	Origin        string `dynamodbav:"Origin"`
	PaymentStatus string `dynamodbav:"PaymentStatus"`
	PaymentDate   string `dynamodbav:"PaymentDate"`
	GSI1PK        string `dynamodbav:"GSI1PK"`
	GSI1SK        string `dynamodbav:"GSI1SK"`
}

func migrate(ctx context.Context, client dynamostore.API, table string, dryRun bool) (stats, error) {
	var st stats

	paginator := dynamodb.NewScanPaginator(client, &dynamodb.ScanInput{
		TableName:        aws.String(table),
		FilterExpression: aws.String("begins_with(SK, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":prefix": &types.AttributeValueMemberS{Value: "ENTRY#"},
		},
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return st, fmt.Errorf("scan: %w", err)
		}
		for _, raw := range page.Items {
			var row entryRow
			if err := attributevalue.UnmarshalMap(raw, &row); err != nil {
				return st, fmt.Errorf("unmarshal entry: %w", err)
			}
			st.scanned++

			plan := planFor(row)
			if plan.empty() {
				st.unchanged++
				continue
			}
			st.changed++
			if plan.setOrigin {
				st.originSet++
			}
			if plan.rewriteGSI1 {
				st.gsi1Rewritten++
			}
			if dryRun {
				log.Printf("would update %s: %s", row.EntryID, plan.describe(row))
				continue
			}
			if err := apply(ctx, client, table, raw, plan); err != nil {
				return st, fmt.Errorf("update entry %s: %w", row.EntryID, err)
			}
		}
	}
	return st, nil
}

type plan struct {
	setOrigin bool
	origin    domain.IncomeOrigin
	// rewriteGSI1 is true when the stored index keys do not match what the new
	// scheme would write, including the case where they must be removed.
	rewriteGSI1 bool
	gsi1PK      string
	gsi1SK      string
}

func (p plan) empty() bool { return !p.setOrigin && !p.rewriteGSI1 }

func (p plan) describe(row entryRow) string {
	var parts []string
	if p.setOrigin {
		parts = append(parts, fmt.Sprintf("origin %q->%q (category %q)", row.Origin, p.origin, row.Category))
	}
	if p.rewriteGSI1 {
		to := p.gsi1SK
		if to == "" {
			to = "<removed>"
		}
		parts = append(parts, fmt.Sprintf("gsi1sk %q->%s", row.GSI1SK, to))
	}
	return strings.Join(parts, ", ")
}

// planFor decides what a row needs, without writing anything.
func planFor(row entryRow) plan {
	var p plan

	// Origin: only income entries get one, and only when they have none. An
	// entry that already carries an origin was written by current code and must
	// not be second-guessed by the legacy category rule.
	if row.Type == string(domain.EntryTypeIncome) && row.Origin == "" {
		p.setOrigin = true
		p.origin = originForLegacy(row.Category)
	}

	// GSI1: paid entries index by payment date, pending ones are absent.
	wantPK, wantSK := "", ""
	if row.PaymentStatus == string(domain.PaymentStatusPaid) && row.PaymentDate != "" {
		wantPK = row.PK
		wantSK = paymentDay(row.PaymentDate) + "#" + row.EntryID
	}
	if row.GSI1PK != wantPK || row.GSI1SK != wantSK {
		p.rewriteGSI1 = true
		p.gsi1PK, p.gsi1SK = wantPK, wantSK
	}
	return p
}

// originForLegacy reproduces the pre-Origin faturamento rule exactly: every
// income category counted as a sale except the "Outros (Receita)" catch-all.
//
// Custom categories map to venda for the same reason — that is what they did
// before. It is not necessarily *right* (a user-made "Empréstimos" category
// counted as faturamento, which is the hole the origin field closes), but
// changing it here would silently move historical figures during a migration
// whose whole promise is that nothing moves. Newly written entries carry a real
// origin; history keeps the number it always reported.
func originForLegacy(category string) domain.IncomeOrigin {
	if category == legacyRevenueCategory {
		return domain.OriginOutros
	}
	return domain.OriginVenda
}

// paymentDay normalizes a stored payment date to "YYYY-MM-DD". Older rows may
// hold an RFC3339 timestamp; the index key is a calendar day either way.
func paymentDay(stored string) string {
	if len(stored) >= 10 {
		return stored[:10]
	}
	return stored
}

// apply rewrites the item in place with PutItem rather than issuing an
// UpdateItem. Two reasons: dynamostore.API exposes only the operations the
// stores use, and removing an attribute is the operation this needs most — a
// pending entry's GSI1 keys must be *absent*, not blank, since DynamoDB rejects
// an empty string in a key attribute. Deleting map entries and putting the
// whole item back says that directly.
func apply(ctx context.Context, client dynamostore.API, table string, item map[string]types.AttributeValue, p plan) error {
	updated := make(map[string]types.AttributeValue, len(item)+1)
	for k, v := range item {
		updated[k] = v
	}

	if p.setOrigin {
		updated["Origin"] = &types.AttributeValueMemberS{Value: string(p.origin)}
	}
	if p.rewriteGSI1 {
		if p.gsi1SK == "" {
			delete(updated, "GSI1PK")
			delete(updated, "GSI1SK")
		} else {
			updated["GSI1PK"] = &types.AttributeValueMemberS{Value: p.gsi1PK}
			updated["GSI1SK"] = &types.AttributeValueMemberS{Value: p.gsi1SK}
		}
	}

	_, err := client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(table),
		Item:      updated,
	})
	return err
}

// printReport answers "why does faturamento look like every inflow?" without
// changing anything. It breaks the ledger's income down by origin and by the
// category that stands in for one when the origin is missing, and prints the
// two totals side by side.
//
// The usual answer is that the inflows really are all sales, or that the rows
// predate the origin field and domain.IsRevenue's shim is still classifying
// them by category — the report tells the two apart.
func printReport(ctx context.Context, client dynamostore.API, table string) error {
	type bucket struct {
		count int
		total int64
	}
	byOrigin := map[string]*bucket{}
	var income, revenue int64
	var incomeRows, missingOrigin int

	paginator := dynamodb.NewScanPaginator(client, &dynamodb.ScanInput{
		TableName:        aws.String(table),
		FilterExpression: aws.String("begins_with(SK, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":prefix": &types.AttributeValueMemberS{Value: "ENTRY#"},
		},
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		for _, raw := range page.Items {
			var row entryRow
			if err := attributevalue.UnmarshalMap(raw, &row); err != nil {
				return fmt.Errorf("unmarshal entry: %w", err)
			}
			if row.Type != string(domain.EntryTypeIncome) {
				continue
			}
			var amount int64
			if av, ok := raw["Amount"].(*types.AttributeValueMemberN); ok {
				if _, err := fmt.Sscanf(av.Value, "%d", &amount); err != nil {
					return fmt.Errorf("parse Amount of %s: %w", row.EntryID, err)
				}
			}
			incomeRows++
			income += amount

			key := row.Origin
			if key == "" {
				missingOrigin++
				key = fmt.Sprintf("(sem origem, categoria %q)", row.Category)
			}
			b, ok := byOrigin[key]
			if !ok {
				b = &bucket{}
				byOrigin[key] = b
			}
			b.count++
			b.total += amount

			// Exactly the rule the dashboard applies, shim included.
			if domain.IsRevenue(domain.FinancialEntry{
				Type: domain.EntryTypeIncome, Category: row.Category,
				Origin: domain.IncomeOrigin(row.Origin),
			}) {
				revenue += amount
			}
		}
	}

	keys := make([]string, 0, len(byOrigin))
	for k := range byOrigin {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	log.Printf("%d inflow entries", incomeRows)
	for _, k := range keys {
		b := byOrigin[k]
		log.Printf("  %-44s %4d entradas  R$ %.2f", k, b.count, float64(b.total)/100)
	}
	log.Printf("faturamento       R$ %.2f", float64(revenue)/100)
	log.Printf("entradas de caixa R$ %.2f", float64(income)/100)
	if income > 0 && revenue == income {
		log.Printf("os dois são iguais: nenhum lançamento de entrada tem origem diferente de venda")
	}
	if missingOrigin > 0 {
		log.Printf("%d entradas ainda sem origem — rode a migração; até lá elas são classificadas pela categoria antiga", missingOrigin)
	}
	return nil
}
