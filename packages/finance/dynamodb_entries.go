package finance

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/emerson/emerbot/packages/domain"
)

func entrySK(date domain.CalendarDate, id domain.EntryID) string {
	return entryPrefix + date.String() + "#" + string(id)
}

type entryItem struct {
	PK string `dynamodbav:"PK"`
	SK string `dynamodbav:"SK"`
	// GSI1PK/GSI1SK index the entry by CashDate — see gsi1IndexName. They are
	// omitempty because the index is sparse: a pending entry has no payment
	// date, so it must be absent from the index entirely. Without omitempty
	// the marshaller writes an empty string, which real DynamoDB rejects for a
	// key attribute — every pending write would fail in production while the
	// in-memory fake happily accepted it.
	GSI1PK   string `dynamodbav:"GSI1PK,omitempty"`
	GSI1SK   string `dynamodbav:"GSI1SK,omitempty"`
	GSI2PK   string `dynamodbav:"GSI2PK"`
	GSI2SK   string `dynamodbav:"GSI2SK"`
	EntryID  string `dynamodbav:"EntryID"`
	UserID   string `dynamodbav:"UserID"`
	Date     string `dynamodbav:"Date"` // RFC3339
	Amount   int64  `dynamodbav:"Amount"`
	Category string `dynamodbav:"Category"`
	Type     string `dynamodbav:"Type"`
	// Origin is domain.IncomeOrigin; empty on expenses, and on income entries
	// written before the field existed (see domain.IsRevenue's shim).
	Origin      string `dynamodbav:"Origin"`
	Description string `dynamodbav:"Description"`
	// DescriptionLower is the lowercased Description, stored so search filters can
	// do case-insensitive contains() (DynamoDB has no lower() in expressions),
	// matching the in-memory store's ToLower-on-both-sides behavior.
	DescriptionLower string `dynamodbav:"DescriptionLower"`
	DueDate          string `dynamodbav:"DueDate"` // RFC3339 or ""
	PaymentStatus    string `dynamodbav:"PaymentStatus"`
	PaymentDate      string `dynamodbav:"PaymentDate"` // RFC3339 or ""
	// PaymentMethod is the user's own words for how this was settled, and is
	// empty on every entry written before ADR-026 — an absent attribute
	// unmarshals to "", which is exactly the right reading, so there is no
	// backfill. Nothing queries or filters on it.
	PaymentMethod   string `dynamodbav:"PaymentMethod"`
	Supplier        string `dynamodbav:"Supplier"`
	Source          string `dynamodbav:"Source"`
	CreatedAt       string `dynamodbav:"CreatedAt"`
	UpdatedAt       string `dynamodbav:"UpdatedAt"`
	RecurrenceID    string `dynamodbav:"RecurrenceID,omitempty"`
	RecurrenceIndex int    `dynamodbav:"RecurrenceIndex,omitempty"`
	RecurrenceTotal int    `dynamodbav:"RecurrenceTotal,omitempty"`
}

func entryToItem(e domain.FinancialEntry) entryItem {
	dueDate := ""
	if e.DueDate != nil {
		dueDate = e.DueDate.String()
	}
	payDate := ""
	if e.PaymentDate != nil {
		payDate = e.PaymentDate.String()
	}
	dateStr := e.TransactionDate.String()
	status := string(e.PaymentStatus)

	// GSI1 is the sparse cash-date index: both key attributes stay empty for a
	// pending entry so the item never enters the index. Money that has not
	// moved is not cash, and a "cash in July" query must not find it.
	gsi1PK, gsi1SK := "", ""
	if cash := CashDate(e); cash != nil {
		gsi1PK = pkPrefix + e.UserID
		gsi1SK = cash.Format("2006-01-02") + "#" + string(e.EntryID)
	}

	return entryItem{
		PK:     pkPrefix + e.UserID,
		SK:     entrySK(e.TransactionDate, e.EntryID),
		GSI1PK: gsi1PK,
		GSI1SK: gsi1SK,
		GSI2PK: pkPrefix + e.UserID,
		// GSI2SK orders entries by effectiveDate (DueDate when set, else
		// Date) — see effectiveDate's doc comment in store.go for why this,
		// not registration Date, is the field callers actually want to
		// query and bucket by.
		GSI2SK:           EffectiveDate(e).Format("2006-01-02") + "#" + string(e.EntryID),
		EntryID:          string(e.EntryID),
		UserID:           e.UserID,
		Date:             dateStr,
		Amount:           e.Amount,
		Category:         e.Category,
		Type:             string(e.Type),
		Origin:           string(e.Origin),
		Description:      e.Description,
		DescriptionLower: strings.ToLower(e.Description),
		DueDate:          dueDate,
		PaymentStatus:    status,
		PaymentDate:      payDate,
		PaymentMethod:    e.PaymentMethod,
		Supplier:         e.Supplier,
		Source:           string(e.Source),
		CreatedAt:        e.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:        e.UpdatedAt.UTC().Format(time.RFC3339),
		RecurrenceID:     e.RecurrenceID,
		RecurrenceIndex:  e.RecurrenceIndex,
		RecurrenceTotal:  e.RecurrenceTotal,
	}
}

func parseStoredCalendarDate(s string) (domain.CalendarDate, error) {
	if d, err := domain.ParseCalendarDate(s); err == nil {
		return d, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return domain.CalendarDate{}, err
	}
	return domain.NewCalendarDate(t), nil
}

func itemToEntry(item entryItem) (domain.FinancialEntry, error) {
	date, err := parseStoredCalendarDate(item.Date)
	if err != nil {
		return domain.FinancialEntry{}, fmt.Errorf("parse Date: %w", err)
	}
	createdAt, _ := time.Parse(time.RFC3339, item.CreatedAt)
	updatedAt, _ := time.Parse(time.RFC3339, item.UpdatedAt)

	var dueDate *domain.CalendarDate
	if item.DueDate != "" {
		t, err := parseStoredCalendarDate(item.DueDate)
		if err == nil {
			dueDate = &t
		}
	}
	var payDate *domain.CalendarDate
	if item.PaymentDate != "" {
		t, err := parseStoredCalendarDate(item.PaymentDate)
		if err == nil {
			payDate = &t
		}
	}

	source := domain.NormalizeSource(item.Source)
	e := domain.FinancialEntry{
		UserID:          item.UserID,
		EntryID:         domain.EntryID(item.EntryID),
		TransactionDate: date,
		Amount:          item.Amount,
		Category:        item.Category,
		Type:            domain.EntryType(item.Type),
		Origin:          domain.NormalizeIncomeOrigin(item.Origin),
		Description:     item.Description,
		DueDate:         dueDate,
		PaymentStatus:   domain.PaymentStatus(item.PaymentStatus),
		PaymentDate:     payDate,
		PaymentMethod:   item.PaymentMethod,
		Supplier:        item.Supplier,
		Source:          source,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
		RecurrenceID:    item.RecurrenceID,
		RecurrenceIndex: item.RecurrenceIndex,
		RecurrenceTotal: item.RecurrenceTotal,
	}
	e.Normalize() // self-heal: corrige inconsistências de dados legados
	if err := e.Validate(); err != nil {
		return domain.FinancialEntry{}, fmt.Errorf("invalid entry %q: %w", item.EntryID, err)
	}
	return e, nil
}

func (s *DynamoDBStore) SaveEntry(ctx context.Context, entry domain.FinancialEntry) error {
	if err := entry.Validate(); err != nil {
		return err
	}
	item := entryToItem(entry)
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item:      av,
	})
	return err
}

// maxTransactWriteItems is DynamoDB's hard per-call limit for
// TransactWriteItems — the only atomic multi-item write DynamoDB offers.
const maxTransactWriteItems = 100

// SaveEntries writes entries in atomic chunks of at most
// maxTransactWriteItems each (a whole chunk succeeds or fails together).
// Series larger than that are split across multiple transactions with no
// cross-chunk rollback: if a later chunk fails, earlier chunks remain
// committed and the error names which chunk failed.
func (s *DynamoDBStore) SaveEntries(ctx context.Context, entries []domain.FinancialEntry) error {
	for start := 0; start < len(entries); start += maxTransactWriteItems {
		end := min(start+maxTransactWriteItems, len(entries))
		chunk := entries[start:end]

		items := make([]types.TransactWriteItem, 0, len(chunk))
		for _, e := range chunk {
			av, err := attributevalue.MarshalMap(entryToItem(e))
			if err != nil {
				return fmt.Errorf("marshal entry: %w", err)
			}
			items = append(items, types.TransactWriteItem{
				Put: &types.Put{
					TableName: aws.String(s.tableName),
					Item:      av,
				},
			})
		}

		if _, err := s.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
			TransactItems: items,
		}); err != nil {
			return fmt.Errorf("transact write entries %d-%d: %w", start, end, err)
		}
	}
	return nil
}

// GetEntry is a single GetItem: the transaction date supplies the other half
// of the sort key, so the row is addressed directly rather than searched for.
func (s *DynamoDBStore) GetEntry(ctx context.Context, userID string, date domain.CalendarDate, entryID string) (domain.FinancialEntry, error) {
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key:       entryKeyAttrs(userID, date, domain.EntryID(entryID)),
	})
	if err != nil {
		return domain.FinancialEntry{}, fmt.Errorf("get entry: %w", err)
	}
	if len(out.Item) == 0 {
		return domain.FinancialEntry{}, fmt.Errorf("%w: %q on %s", ErrEntryNotFound, entryID, date)
	}
	var item entryItem
	if err := attributevalue.UnmarshalMap(out.Item, &item); err != nil {
		return domain.FinancialEntry{}, fmt.Errorf("unmarshal entry: %w", err)
	}
	return itemToEntry(item)
}

// FindEntryByID reads the user's whole partition to locate one row, because an
// ID on its own does not say which sort key it lives under. Only the AI's
// edit_entry tool needs it — see the interface comment in store.go.
func (s *DynamoDBStore) FindEntryByID(ctx context.Context, userID, entryID string) (domain.FinancialEntry, error) {
	entries, err := s.ListEntries(ctx, userID, EntryFilter{})
	if err != nil {
		return domain.FinancialEntry{}, err
	}
	for _, e := range entries {
		if string(e.EntryID) == entryID {
			return e, nil
		}
	}
	return domain.FinancialEntry{}, fmt.Errorf("%w: %q", ErrEntryNotFound, entryID)
}

// entryKeyAttrs is the primary key of one entry row.
func entryKeyAttrs(userID string, date domain.CalendarDate, id domain.EntryID) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"PK": &types.AttributeValueMemberS{Value: pkPrefix + userID},
		"SK": &types.AttributeValueMemberS{Value: entrySK(date, id)},
	}
}

// basisKeys names the index and key attributes that carry a given date basis,
// plus the prefix its sort key values need. The three bases live on three
// different key structures, all of them already provisioned:
//
//	BasisEffective   GSI2 (GSI2SK = "<effectiveDate>#<id>")
//	BasisPayment     GSI1 (GSI1SK = "<paymentDate>#<id>", sparse)
//	BasisTransaction the base table (SK = "ENTRY#<transactionDate>#<id>")
//
// The base table needs no index at all: its own sort key is already ordered by
// transaction date, which is what faturamento is measured on.
func basisKeys(b DateBasis) (indexName, pkAttr, skAttr, skPrefix string) {
	switch b {
	case BasisPayment:
		return gsi1IndexName, "GSI1PK", "GSI1SK", ""
	case BasisTransaction:
		return "", "PK", "SK", entryPrefix
	default:
		return gsi2IndexName, "GSI2PK", "GSI2SK", ""
	}
}

// listEntriesInput builds the Query for a filter: the key condition pushes the
// date range (or cursor) into the sort key of whichever basis the filter asks
// for, and everything else becomes a filter expression. It is separate from
// ListEntries so the request shape can be asserted directly, without inferring
// it from returned rows.
func (s *DynamoDBStore) listEntriesInput(userID string, filter EntryFilter) *dynamodb.QueryInput {
	indexName, pkAttr, skAttr, skPrefix := basisKeys(filter.DateBasis)

	keyCondition := pkAttr + " = :pk"
	exprValues := map[string]types.AttributeValue{
		":pk": &types.AttributeValueMemberS{Value: pkPrefix + userID},
	}
	day := func(t *time.Time) string { return skPrefix + t.Format("2006-01-02") }

	// Cursor takes priority: it provides an exact exclusive upper bound on the
	// sort key, preventing the page-boundary data loss that occurs when
	// cursor-based pagination uses a date-only bound. EntryFilter.Validate
	// restricts it to BasisEffective, whose sort key it is shaped like.
	switch {
	case filter.Cursor != "":
		keyCondition += " AND " + skAttr + " < :cursor"
		exprValues[":cursor"] = &types.AttributeValueMemberS{Value: filter.Cursor}
	case filter.From != nil && filter.To != nil:
		keyCondition += " AND " + skAttr + " BETWEEN :from AND :to"
		exprValues[":from"] = &types.AttributeValueMemberS{Value: day(filter.From)}
		exprValues[":to"] = &types.AttributeValueMemberS{Value: day(filter.To) + "#\xff"}
	case filter.From != nil:
		keyCondition += " AND " + skAttr + " >= :from"
		exprValues[":from"] = &types.AttributeValueMemberS{Value: day(filter.From)}
	case filter.To != nil:
		keyCondition += " AND " + skAttr + " <= :to"
		exprValues[":to"] = &types.AttributeValueMemberS{Value: day(filter.To) + "#\xff"}
	case skPrefix != "":
		// An unbounded query on the base table would sweep in the user's goal,
		// category and notification items, which share the partition. The GSIs
		// only ever contain entries, so they need no equivalent.
		keyCondition += " AND begins_with(" + skAttr + ", :skprefix)"
		exprValues[":skprefix"] = &types.AttributeValueMemberS{Value: skPrefix}
	}

	var filterExpr *string
	var filterNames map[string]string

	var filters []string
	if filter.Category != "" {
		filters = append(filters, "Category = :cat")
		exprValues[":cat"] = &types.AttributeValueMemberS{Value: filter.Category}
	}
	if filter.Description != "" {
		// DynamoDB's contains() is case-sensitive, so match against the stored
		// lowercased attribute with a lowercased term — case-insensitive, and
		// consistent with the in-memory store. Use a name placeholder since the
		// attribute is safest quoted.
		filters = append(filters, "contains(#desc, :desc)")
		exprValues[":desc"] = &types.AttributeValueMemberS{Value: strings.ToLower(filter.Description)}
		if filterNames == nil {
			filterNames = map[string]string{}
		}
		filterNames["#desc"] = "DescriptionLower"
	}
	if filter.Status != "" {
		filters = append(filters, "PaymentStatus = :status")
		exprValues[":status"] = &types.AttributeValueMemberS{Value: string(filter.Status)}
	}
	if filter.Type != "" {
		filters = append(filters, "#t = :type")
		exprValues[":type"] = &types.AttributeValueMemberS{Value: string(filter.Type)}
		if filterNames == nil {
			filterNames = map[string]string{}
		}
		filterNames["#t"] = "Type"
	}
	if filter.Origin != "" {
		filters = append(filters, "Origin = :origin")
		exprValues[":origin"] = &types.AttributeValueMemberS{Value: string(filter.Origin)}
	}
	if len(filters) > 0 {
		expr := strings.Join(filters, " AND ")
		filterExpr = &expr
	}

	input := &dynamodb.QueryInput{
		TableName:                 aws.String(s.tableName),
		KeyConditionExpression:    aws.String(keyCondition),
		ExpressionAttributeValues: exprValues,
		FilterExpression:          filterExpr,
	}
	// The transaction basis reads the base table, which has no IndexName.
	if indexName != "" {
		input.IndexName = aws.String(indexName)
	}
	if filterNames != nil {
		input.ExpressionAttributeNames = filterNames
	}
	if filter.Limit > 0 {
		// Most-recent-first, so we can stop reading pages as soon as we have
		// enough matches instead of scanning the whole partition — GSI2SK is
		// date-prefixed, so descending key order is effectiveDate descending.
		input.ScanIndexForward = aws.Bool(false)
	}
	return input
}

// ListEntries queries the GSI2-Status index (hash: GSI2PK, range: GSI2SK —
// see gsi2IndexName), which orders entries by effectiveDate rather than
// registration Date. filter.From/To push down directly into a GSI2SK range
// condition: an instalment registered in July but due in
// December is stored with GSI2SK="2026-12-.../..." and so is only returned
// when querying December, not July.
func (s *DynamoDBStore) ListEntries(ctx context.Context, userID string, filter EntryFilter) ([]domain.FinancialEntry, error) {
	if err := filter.Validate(); err != nil {
		return nil, err
	}
	var entries []domain.FinancialEntry
	paginator := dynamodb.NewQueryPaginator(s.client, s.listEntriesInput(userID, filter))
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("query entries: %w", err)
		}
		for _, raw := range page.Items {
			var item entryItem
			if err := attributevalue.UnmarshalMap(raw, &item); err != nil {
				return nil, fmt.Errorf("unmarshal entry: %w", err)
			}
			e, err := itemToEntry(item)
			if err != nil {
				return nil, err
			}
			entries = append(entries, e)
		}
		if filter.Limit > 0 && len(entries) >= filter.Limit {
			break
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return lessByBasis(filter.DateBasis, entries[i], entries[j])
	})
	if filter.Limit > 0 && len(entries) > filter.Limit {
		entries = entries[:filter.Limit]
	}
	return entries, nil
}

// lessByBasis is the total order both stores return entries in: most recent
// first by the basis date, ties broken by descending EntryID.
//
// The tiebreak is not cosmetic. Dates repeat — a dozen invoices can share a due
// date — and a comparison on the date alone leaves those entries free to come
// back in any order. sort.Slice is not stable, and the in-memory store feeds it
// from a Go map, whose iteration order is randomised per call, so the two
// implementations could disagree on the same data and the in-memory one could
// disagree with itself.
//
// It breaks on the id *descending* because the id is the second half of the
// GSI2 sort key (date#id) and the cursor walks that key backwards. Ordering ties
// any other way would make the returned order stop matching the key the cursor
// pages through, and paginating could then skip or repeat them.
func lessByBasis(b DateBasis, x, y domain.FinancialEntry) bool {
	dx, dy := sortDate(b, x), sortDate(b, y)
	if !dx.Equal(dy) {
		return dx.After(dy)
	}
	return x.EntryID > y.EntryID
}

// sortDate is the date the returned slice is ordered by — the same one the
// query's key condition ranged over, so "most recent first" means the same
// thing to the caller as it did to DynamoDB. Both stores use it, which is what
// keeps their orderings from diverging.
func sortDate(b DateBasis, e domain.FinancialEntry) time.Time {
	switch b {
	case BasisPayment:
		// Non-nil for anything the sparse cash index can return; the fallback
		// only matters for the in-memory store's pre-filter pass.
		if cash := CashDate(e); cash != nil {
			return *cash
		}
		return time.Time{}
	case BasisTransaction:
		return RevenueDate(e)
	default:
		return EffectiveDate(e)
	}
}

// UpdateEntry writes updated where previous lives. It reads nothing first: the
// caller already holds the row it is editing, and `previous` is what says which
// key to replace.
//
// Existence is enforced by a condition rather than by a preceding read, so a
// missing row still fails instead of being silently created — one conditional
// write in place of a read plus a write.
func (s *DynamoDBStore) UpdateEntry(ctx context.Context, previous, updated domain.FinancialEntry) error {
	if err := updated.Validate(); err != nil {
		return err
	}
	item, err := attributevalue.MarshalMap(entryToItem(updated))
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}
	oldSK := entrySK(previous.TransactionDate, previous.EntryID)
	newSK := entrySK(updated.TransactionDate, updated.EntryID)

	if oldSK == newSK {
		_, err := s.client.PutItem(ctx, &dynamodb.PutItemInput{
			TableName:           aws.String(s.tableName),
			Item:                item,
			ConditionExpression: aws.String("attribute_exists(PK)"),
		})
		return notFoundIfConditionFailed(err, updated.EntryID)
	}

	// The transaction date moved, so the row's sort key moved with it: delete
	// where it was and write where it now belongs, atomically, or the entry
	// would exist twice — once under each date.
	_, err = s.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{TransactItems: []types.TransactWriteItem{
		{Delete: &types.Delete{
			TableName:           aws.String(s.tableName),
			Key:                 entryKeyAttrs(previous.UserID, previous.TransactionDate, previous.EntryID),
			ConditionExpression: aws.String("attribute_exists(PK)"),
		}},
		{Put: &types.Put{TableName: aws.String(s.tableName), Item: item}},
	}})
	return notFoundIfConditionFailed(err, updated.EntryID)
}

// notFoundIfConditionFailed turns the "attribute_exists" rejection into
// ErrEntryNotFound, so callers tell a missing row from a failed write with
// errors.Is instead of by inspecting AWS error types.
func notFoundIfConditionFailed(err error, id domain.EntryID) error {
	if err == nil {
		return nil
	}
	var condFailed *types.ConditionalCheckFailedException
	var txCanceled *types.TransactionCanceledException
	if errors.As(err, &condFailed) {
		return fmt.Errorf("%w: %q", ErrEntryNotFound, id)
	}
	if errors.As(err, &txCanceled) {
		for _, reason := range txCanceled.CancellationReasons {
			if reason.Code != nil && *reason.Code == "ConditionalCheckFailed" {
				return fmt.Errorf("%w: %q", ErrEntryNotFound, id)
			}
		}
	}
	return err
}

func (s *DynamoDBStore) DeleteEntry(ctx context.Context, userID string, date domain.CalendarDate, entryID string) error {
	id := domain.EntryID(entryID)
	_, err := s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.tableName),
		Key:       entryKeyAttrs(userID, date, id),
		// DynamoDB's delete is idempotent, so without this a wrong date (or an
		// already-deleted entry) would answer 204 and leave the row in place.
		ConditionExpression: aws.String("attribute_exists(PK)"),
	})
	return notFoundIfConditionFailed(err, id)
}
