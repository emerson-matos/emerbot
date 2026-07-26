package payments

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/dynamostore"
)

// Canonical payment items share the existing finance table (single-table
// design). They live in the same USER#<ledger> partition as FinancialEntry so
// the dashboard reads them through the existing auth path, distinguished by
// their SK prefix. Each item stores its originating SourceDate so Save can
// replace exactly one (Provider, SourceDate) import even though receivables
// carry future, varied ExpectedDates.
//
// The item date in an SK is the *business* date (sale day, expected day, payment
// day), because that is what the dashboard range-queries. A day's import is
// therefore scattered across many SKs, so each import also writes an index item
// (importSK) listing exactly the SKs it wrote: that is what lets Save find the
// previous version of an import with a single GetItem instead of re-reading the
// partition's whole history on every run.
const (
	pkPrefix   = "USER#"
	salePrefix = "SALE#"
	recvPrefix = "RECV#"
	payPrefix  = "PAYMT#"
	importPfx  = "IMPORT#"

	// skHigh is appended to an upper date bound so a BETWEEN on the SK includes
	// every item on the "to" day regardless of the trailing id/parcela. U+FFFF
	// sorts above every character the SKs actually use and, unlike a bare 0xFF
	// byte, is valid UTF-8 — DynamoDB compares strings by their UTF-8 bytes.
	skHigh = "#\uffff"
)

func saleSK(date domain.CalendarDate, id SaleID) string {
	return salePrefix + date.String() + "#" + string(id)
}

func recvSK(date domain.CalendarDate, id SaleID, parcela int) string {
	return recvPrefix + date.String() + "#" + string(id) + "#" + strconv.Itoa(parcela)
}

// paySK includes the parcela for the same reason recvSK does: one sale can
// liquidate several installments on a single day (anticipation), and without it
// those payments collapse onto one key — losing every liquidation but the last,
// and failing the write outright when they land in the same batch.
func paySK(date domain.CalendarDate, id SaleID, parcela int) string {
	return payPrefix + date.String() + "#" + string(id) + "#" + strconv.Itoa(parcela)
}

// importSK identifies the index item for one logical import.
func importSK(provider Provider, sourceDate domain.CalendarDate) string {
	return importPfx + string(provider) + "#" + sourceDate.String()
}

// --- DynamoDB item shapes ---

type saleItem struct {
	PK           string `dynamodbav:"PK"`
	SK           string `dynamodbav:"SK"`
	Provider     string `dynamodbav:"Provider"`
	SourceDate   string `dynamodbav:"SourceDate"`
	SaleID       string `dynamodbav:"SaleID"`
	ExternalID   string `dynamodbav:"ExternalID"`
	SaleDate     string `dynamodbav:"SaleDate"`
	GrossAmount  int64  `dynamodbav:"GrossAmount"`
	NetAmount    int64  `dynamodbav:"NetAmount"`
	FeeAmount    int64  `dynamodbav:"FeeAmount"`
	Method       string `dynamodbav:"Method"`
	Brand        string `dynamodbav:"Brand"`
	Installments int    `dynamodbav:"Installments"`
}

type recvItem struct {
	PK                string `dynamodbav:"PK"`
	SK                string `dynamodbav:"SK"`
	Provider          string `dynamodbav:"Provider"`
	SourceDate        string `dynamodbav:"SourceDate"`
	SaleID            string `dynamodbav:"SaleID"`
	ExpectedDate      string `dynamodbav:"ExpectedDate"`
	Amount            int64  `dynamodbav:"Amount"`
	InstallmentNumber int    `dynamodbav:"InstallmentNumber"`
	InstallmentTotal  int    `dynamodbav:"InstallmentTotal"`
}

type payItem struct {
	PK                string `dynamodbav:"PK"`
	SK                string `dynamodbav:"SK"`
	Provider          string `dynamodbav:"Provider"`
	SourceDate        string `dynamodbav:"SourceDate"`
	SaleID            string `dynamodbav:"SaleID"`
	PaymentDate       string `dynamodbav:"PaymentDate"`
	Amount            int64  `dynamodbav:"Amount"`
	InstallmentNumber int    `dynamodbav:"InstallmentNumber"`
}

// importItem is the index of one logical import: the SKs it wrote. Save reads it
// to know what the previous version of this (Provider, SourceDate) covered.
type importItem struct {
	PK         string   `dynamodbav:"PK"`
	SK         string   `dynamodbav:"SK"`
	Provider   string   `dynamodbav:"Provider"`
	SourceDate string   `dynamodbav:"SourceDate"`
	Keys       []string `dynamodbav:"Keys"`
}

// DynamoDBRepository implements Repository against the shared finance table.
type DynamoDBRepository struct {
	client    dynamostore.API
	tableName string
	ledgerID  string // partition all payment items belong to (the pharmacy ledger)
}

var _ Repository = (*DynamoDBRepository)(nil)

// NewDynamoDBRepository creates a repository bound to one table and ledger
// partition. If endpoint is non-empty it overrides the endpoint (DynamoDB Local).
func NewDynamoDBRepository(ctx context.Context, tableName, endpoint, ledgerID string) (*DynamoDBRepository, error) {
	client, err := dynamostore.NewClient(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	return NewDynamoDBRepositoryWithClient(client, tableName, ledgerID), nil
}

// NewDynamoDBRepositoryWithClient builds a repository over any dynamostore.API,
// which is how tests exercise the replace-and-reindex logic in Save.
func NewDynamoDBRepositoryWithClient(client dynamostore.API, tableName, ledgerID string) *DynamoDBRepository {
	return &DynamoDBRepository{client: client, tableName: tableName, ledgerID: ledgerID}
}

func (r *DynamoDBRepository) pk() string { return pkPrefix + r.ledgerID }

// Save replaces exactly the prior (Provider, SourceDate) set with result: it
// deletes the previous version's items that are absent from the new set, then
// puts every new item (a put overwrites a same-key item), and finally records
// the new key set in the import's index item.
//
// The replace is NOT atomic — a day's import can exceed one batch, and a failure
// partway leaves the day half-replaced. It is instead *idempotent and
// self-healing*: re-running the same envelope converges on the same state, and
// the index item is written last, so a crash before it leaves the index still
// describing the previous version and the next run re-derives the full delete
// set. Recovery is therefore always "re-drop the object" (see docs/deploy.md).
func (r *DynamoDBRepository) Save(ctx context.Context, result ImportResult) error {
	if err := ValidateImportResult(result); err != nil {
		return err
	}

	newItems, newKeys, err := r.marshalResult(result)
	if err != nil {
		return err
	}

	oldKeys, err := r.existingKeys(ctx, result.Provider, result.SourceDate)
	if err != nil {
		return err
	}

	var writes []types.WriteRequest
	for _, sk := range oldKeys {
		if newKeys[sk] {
			continue // will be overwritten by a Put — avoid a same-key conflict
		}
		writes = append(writes, types.WriteRequest{DeleteRequest: &types.DeleteRequest{
			Key: map[string]types.AttributeValue{
				"PK": &types.AttributeValueMemberS{Value: r.pk()},
				"SK": &types.AttributeValueMemberS{Value: sk},
			},
		}})
	}
	for _, it := range newItems {
		writes = append(writes, types.WriteRequest{PutRequest: &types.PutRequest{Item: it}})
	}
	if err := r.batchWrite(ctx, writes); err != nil {
		return err
	}
	return r.putImportIndex(ctx, result, newKeys)
}

// maxBatchWriteItems is DynamoDB's hard per-call limit for BatchWriteItem.
// BatchWriteItem is used rather than TransactWriteItems because the replace
// spans more items than one transaction allows anyway, so the transaction bought
// no real atomicity while costing twice the write capacity.
const maxBatchWriteItems = 25

// maxBatchWriteRetries bounds the retry loop for UnprocessedItems (throttling).
const maxBatchWriteRetries = 8

// batchWrite writes every request, re-submitting UnprocessedItems — which
// BatchWriteItem returns instead of failing when a batch is throttled.
func (r *DynamoDBRepository) batchWrite(ctx context.Context, writes []types.WriteRequest) error {
	for start := 0; start < len(writes); start += maxBatchWriteItems {
		end := min(start+maxBatchWriteItems, len(writes))
		pending := map[string][]types.WriteRequest{r.tableName: writes[start:end]}

		for attempt := 0; len(pending[r.tableName]) > 0; attempt++ {
			if attempt >= maxBatchWriteRetries {
				return fmt.Errorf("batch write payments %d-%d: %d items still unprocessed after %d attempts",
					start, end, len(pending[r.tableName]), attempt)
			}
			out, err := r.client.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{RequestItems: pending})
			if err != nil {
				return fmt.Errorf("batch write payments %d-%d: %w", start, end, err)
			}
			pending = out.UnprocessedItems
			if len(pending[r.tableName]) == 0 {
				break
			}
			// Exponential backoff before retrying the throttled remainder.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(1<<attempt) * 50 * time.Millisecond):
			}
		}
	}
	return nil
}

// putImportIndex records the SKs this import wrote, so the next run of the same
// (Provider, SourceDate) can find them with one GetItem.
func (r *DynamoDBRepository) putImportIndex(ctx context.Context, result ImportResult, keys map[string]bool) error {
	sks := make([]string, 0, len(keys))
	for sk := range keys {
		sks = append(sks, sk)
	}
	sort.Strings(sks) // deterministic item content, so a re-import is a no-op write

	sk := importSK(result.Provider, result.SourceDate)
	av, err := attributevalue.MarshalMap(importItem{
		PK: r.pk(), SK: sk, Provider: string(result.Provider),
		SourceDate: result.SourceDate.String(), Keys: sks,
	})
	if err != nil {
		return fmt.Errorf("marshal import index %s: %w", sk, err)
	}
	if _, err := r.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(r.tableName), Item: av,
	}); err != nil {
		return fmt.Errorf("put import index %s: %w", sk, err)
	}
	return nil
}

// marshalResult marshals every canonical item and returns the marshalled items
// plus the set of their SKs.
func (r *DynamoDBRepository) marshalResult(result ImportResult) ([]map[string]types.AttributeValue, map[string]bool, error) {
	items := make([]map[string]types.AttributeValue, 0, len(result.Sales)+len(result.Receivables)+len(result.Payments))
	keys := make(map[string]bool)
	sd := result.SourceDate.String()

	// Duplicate SKs are rejected upstream by ValidateImportResult, which Save
	// runs first — BatchWriteItem would otherwise fail the whole batch with
	// "Provided list of item keys contains duplicates".
	add := func(sk string, v any) error {
		av, err := attributevalue.MarshalMap(v)
		if err != nil {
			return fmt.Errorf("marshal %s: %w", sk, err)
		}
		items = append(items, av)
		keys[sk] = true
		return nil
	}

	for _, s := range result.Sales {
		sk := saleSK(s.SaleDate, s.ID)
		if err := add(sk, saleItem{
			PK: r.pk(), SK: sk, Provider: string(s.Provider), SourceDate: sd,
			SaleID: string(s.ID), ExternalID: s.ExternalID, SaleDate: s.SaleDate.String(),
			GrossAmount: s.GrossAmount, NetAmount: s.NetAmount, FeeAmount: s.FeeAmount,
			Method: string(s.Method), Brand: s.Brand, Installments: s.Installments,
		}); err != nil {
			return nil, nil, err
		}
	}
	for _, rc := range result.Receivables {
		sk := recvSK(rc.ExpectedDate, rc.SaleID, rc.InstallmentNumber)
		if err := add(sk, recvItem{
			PK: r.pk(), SK: sk, Provider: string(rc.Provider), SourceDate: sd,
			SaleID: string(rc.SaleID), ExpectedDate: rc.ExpectedDate.String(), Amount: rc.Amount,
			InstallmentNumber: rc.InstallmentNumber, InstallmentTotal: rc.InstallmentTotal,
		}); err != nil {
			return nil, nil, err
		}
	}
	for _, p := range result.Payments {
		sk := paySK(p.PaymentDate, p.SaleID, p.InstallmentNumber)
		if err := add(sk, payItem{
			PK: r.pk(), SK: sk, Provider: string(p.Provider), SourceDate: sd,
			SaleID: string(p.SaleID), PaymentDate: p.PaymentDate.String(), Amount: p.Amount,
			InstallmentNumber: p.InstallmentNumber,
		}); err != nil {
			return nil, nil, err
		}
	}
	return items, keys, nil
}

// existingKeys returns the SKs written by the previous run of this (provider,
// sourceDate) import, read from the import's index item.
//
// This is a single GetItem. The obvious alternative — querying each prefix and
// filtering on Provider/SourceDate — is charged for every item read *before* the
// filter applies, so it would re-read the ledger's entire payment history on
// every import, at a cost that grows without bound as history accumulates.
func (r *DynamoDBRepository) existingKeys(ctx context.Context, provider Provider, sourceDate domain.CalendarDate) ([]string, error) {
	sk := importSK(provider, sourceDate)
	out, err := r.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: r.pk()},
			"SK": &types.AttributeValueMemberS{Value: sk},
		},
		ConsistentRead: aws.Bool(true), // a re-import must not read a stale key set
	})
	if err != nil {
		return nil, fmt.Errorf("get import index %s: %w", sk, err)
	}
	if out.Item == nil {
		return nil, nil // first import of this day
	}
	var it importItem
	if err := attributevalue.UnmarshalMap(out.Item, &it); err != nil {
		return nil, fmt.Errorf("unmarshal import index %s: %w", sk, err)
	}
	return it.Keys, nil
}

// queryRange returns raw items for a prefix whose SK date falls in [from, to].
func (r *DynamoDBRepository) queryRange(ctx context.Context, prefix string, from, to domain.CalendarDate) ([]map[string]types.AttributeValue, error) {
	var items []map[string]types.AttributeValue
	paginator := dynamodb.NewQueryPaginator(r.client, &dynamodb.QueryInput{
		TableName:              aws.String(r.tableName),
		KeyConditionExpression: aws.String("PK = :pk AND SK BETWEEN :lo AND :hi"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk": &types.AttributeValueMemberS{Value: r.pk()},
			":lo": &types.AttributeValueMemberS{Value: prefix + from.String()},
			":hi": &types.AttributeValueMemberS{Value: prefix + to.String() + skHigh},
		},
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("query %s range: %w", prefix, err)
		}
		items = append(items, page.Items...)
	}
	return items, nil
}

func (r *DynamoDBRepository) ListSales(ctx context.Context, from, to domain.CalendarDate) ([]Sale, error) {
	raw, err := r.queryRange(ctx, salePrefix, from, to)
	if err != nil {
		return nil, err
	}
	sales := make([]Sale, 0, len(raw))
	for _, m := range raw {
		var it saleItem
		if err := attributevalue.UnmarshalMap(m, &it); err != nil {
			return nil, fmt.Errorf("unmarshal sale: %w", err)
		}
		date, err := domain.ParseCalendarDate(it.SaleDate)
		if err != nil {
			return nil, fmt.Errorf("parse sale date: %w", err)
		}
		sales = append(sales, Sale{
			ID: SaleID(it.SaleID), Provider: Provider(it.Provider), ExternalID: it.ExternalID,
			SaleDate: date, GrossAmount: it.GrossAmount, NetAmount: it.NetAmount, FeeAmount: it.FeeAmount,
			Method: PaymentMethod(it.Method), Brand: it.Brand, Installments: it.Installments,
		})
	}
	return sales, nil
}

func (r *DynamoDBRepository) ListReceivables(ctx context.Context, from, to domain.CalendarDate) ([]ExpectedReceivable, error) {
	raw, err := r.queryRange(ctx, recvPrefix, from, to)
	if err != nil {
		return nil, err
	}
	recv := make([]ExpectedReceivable, 0, len(raw))
	for _, m := range raw {
		var it recvItem
		if err := attributevalue.UnmarshalMap(m, &it); err != nil {
			return nil, fmt.Errorf("unmarshal receivable: %w", err)
		}
		date, err := domain.ParseCalendarDate(it.ExpectedDate)
		if err != nil {
			return nil, fmt.Errorf("parse expected date: %w", err)
		}
		recv = append(recv, ExpectedReceivable{
			Provider: Provider(it.Provider), SaleID: SaleID(it.SaleID), ExpectedDate: date,
			Amount: it.Amount, InstallmentNumber: it.InstallmentNumber, InstallmentTotal: it.InstallmentTotal,
		})
	}
	return recv, nil
}

func (r *DynamoDBRepository) ListPayments(ctx context.Context, from, to domain.CalendarDate) ([]Payment, error) {
	raw, err := r.queryRange(ctx, payPrefix, from, to)
	if err != nil {
		return nil, err
	}
	pays := make([]Payment, 0, len(raw))
	for _, m := range raw {
		var it payItem
		if err := attributevalue.UnmarshalMap(m, &it); err != nil {
			return nil, fmt.Errorf("unmarshal payment: %w", err)
		}
		date, err := domain.ParseCalendarDate(it.PaymentDate)
		if err != nil {
			return nil, fmt.Errorf("parse payment date: %w", err)
		}
		pays = append(pays, Payment{
			Provider: Provider(it.Provider), SaleID: SaleID(it.SaleID), PaymentDate: date, Amount: it.Amount,
		})
	}
	return pays, nil
}
