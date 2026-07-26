package finance

import (
	"context"
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
	PK          string `dynamodbav:"PK"`
	SK          string `dynamodbav:"SK"`
	GSI1PK      string `dynamodbav:"GSI1PK"`
	GSI1SK      string `dynamodbav:"GSI1SK"`
	GSI2PK      string `dynamodbav:"GSI2PK"`
	GSI2SK      string `dynamodbav:"GSI2SK"`
	EntryID     string `dynamodbav:"EntryID"`
	UserID      string `dynamodbav:"UserID"`
	Date        string `dynamodbav:"Date"` // RFC3339
	Amount      int64  `dynamodbav:"Amount"`
	Category    string `dynamodbav:"Category"`
	Type        string `dynamodbav:"Type"`
	Description string `dynamodbav:"Description"`
	// DescriptionLower is the lowercased Description, stored so search filters can
	// do case-insensitive contains() (DynamoDB has no lower() in expressions),
	// matching the in-memory store's ToLower-on-both-sides behavior.
	DescriptionLower string `dynamodbav:"DescriptionLower"`
	DueDate          string `dynamodbav:"DueDate"` // RFC3339 or ""
	PaymentStatus    string `dynamodbav:"PaymentStatus"`
	PaymentDate      string `dynamodbav:"PaymentDate"` // RFC3339 or ""
	Supplier         string `dynamodbav:"Supplier"`
	Source           string `dynamodbav:"Source"`
	CreatedAt        string `dynamodbav:"CreatedAt"`
	UpdatedAt        string `dynamodbav:"UpdatedAt"`
	RecurrenceID     string `dynamodbav:"RecurrenceID,omitempty"`
	RecurrenceIndex  int    `dynamodbav:"RecurrenceIndex,omitempty"`
	RecurrenceTotal  int    `dynamodbav:"RecurrenceTotal,omitempty"`
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

	return entryItem{
		PK:     pkPrefix + e.UserID,
		SK:     entrySK(e.TransactionDate, e.EntryID),
		GSI1PK: pkPrefix + e.UserID,
		GSI1SK: e.Category + "#" + dateStr,
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
		Description:      e.Description,
		DescriptionLower: strings.ToLower(e.Description),
		DueDate:          dueDate,
		PaymentStatus:    status,
		PaymentDate:      payDate,
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
		Description:     item.Description,
		DueDate:         dueDate,
		PaymentStatus:   domain.PaymentStatus(item.PaymentStatus),
		PaymentDate:     payDate,
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

func (s *DynamoDBStore) GetEntry(ctx context.Context, userID, entryID string) (domain.FinancialEntry, error) {
	// We need the date to build the SK — scan by EntryID using a filter.
	// For simplicity in a 2-user system, query by PK and filter on EntryID.
	entries, err := s.ListEntries(ctx, userID, EntryFilter{})
	if err != nil {
		return domain.FinancialEntry{}, err
	}
	for _, e := range entries {
		if string(e.EntryID) == entryID {
			return e, nil
		}
	}
	return domain.FinancialEntry{}, fmt.Errorf("entry %q not found", entryID)
}

// listEntriesInput builds the Query for a filter: the key condition pushes the
// date range (or cursor) into GSI2SK, and everything else becomes a filter
// expression. It is separate from ListEntries so the request shape can be
// asserted directly, without inferring it from returned rows.
func (s *DynamoDBStore) listEntriesInput(userID string, filter EntryFilter) *dynamodb.QueryInput {
	keyCondition := "GSI2PK = :pk"
	exprValues := map[string]types.AttributeValue{
		":pk": &types.AttributeValueMemberS{Value: pkPrefix + userID},
	}

	// Cursor takes priority: it provides an exact exclusive upper bound on
	// GSI2SK, preventing the page-boundary data loss that occurs when
	// cursor-based pagination uses a date-only bound.
	if filter.Cursor != "" {
		keyCondition = "GSI2PK = :pk AND GSI2SK < :cursor"
		exprValues[":cursor"] = &types.AttributeValueMemberS{Value: filter.Cursor}
	} else if filter.From != nil && filter.To != nil {
		keyCondition = "GSI2PK = :pk AND GSI2SK BETWEEN :from AND :to"
		exprValues[":from"] = &types.AttributeValueMemberS{Value: filter.From.Format("2006-01-02")}
		exprValues[":to"] = &types.AttributeValueMemberS{Value: filter.To.Format("2006-01-02") + "#\xff"}
	} else if filter.From != nil {
		keyCondition = "GSI2PK = :pk AND GSI2SK >= :from"
		exprValues[":from"] = &types.AttributeValueMemberS{Value: filter.From.Format("2006-01-02")}
	} else if filter.To != nil {
		keyCondition = "GSI2PK = :pk AND GSI2SK <= :to"
		exprValues[":to"] = &types.AttributeValueMemberS{Value: filter.To.Format("2006-01-02") + "#\xff"}
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
	if len(filters) > 0 {
		expr := strings.Join(filters, " AND ")
		filterExpr = &expr
	}

	input := &dynamodb.QueryInput{
		TableName:                 aws.String(s.tableName),
		IndexName:                 aws.String(gsi2IndexName),
		KeyConditionExpression:    aws.String(keyCondition),
		ExpressionAttributeValues: exprValues,
		FilterExpression:          filterExpr,
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
// condition: a /recorrente installment registered in July but due in
// December is stored with GSI2SK="2026-12-.../..." and so is only returned
// when querying December, not July.
func (s *DynamoDBStore) ListEntries(ctx context.Context, userID string, filter EntryFilter) ([]domain.FinancialEntry, error) {
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
		return EffectiveDate(entries[i]).After(EffectiveDate(entries[j]))
	})
	if filter.Limit > 0 && len(entries) > filter.Limit {
		entries = entries[:filter.Limit]
	}
	return entries, nil
}

func (s *DynamoDBStore) UpdateEntry(ctx context.Context, entry domain.FinancialEntry) error {
	if err := entry.Validate(); err != nil {
		return err
	}
	old, err := s.GetEntry(ctx, entry.UserID, string(entry.EntryID))
	if err != nil {
		return err
	}
	oldSK := entrySK(old.TransactionDate, old.EntryID)
	newSK := entrySK(entry.TransactionDate, entry.EntryID)
	if oldSK == newSK {
		return s.SaveEntry(ctx, entry)
	}
	item, err := attributevalue.MarshalMap(entryToItem(entry))
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}
	_, err = s.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{TransactItems: []types.TransactWriteItem{
		{Delete: &types.Delete{TableName: aws.String(s.tableName), Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: pkPrefix + entry.UserID},
			"SK": &types.AttributeValueMemberS{Value: oldSK},
		}}},
		{Put: &types.Put{TableName: aws.String(s.tableName), Item: item}},
	}})
	return err
}

func (s *DynamoDBStore) DeleteEntry(ctx context.Context, userID, entryID string) error {
	entry, err := s.GetEntry(ctx, userID, entryID)
	if err != nil {
		return err
	}
	_, err = s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: pkPrefix + userID},
			"SK": &types.AttributeValueMemberS{Value: entrySK(entry.TransactionDate, entry.EntryID)},
		},
	})
	return err
}
