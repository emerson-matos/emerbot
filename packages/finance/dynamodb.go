package finance

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/emerson/emerbot/packages/domain"
)

const (
	pkPrefix    = "USER#"
	entryPrefix = "ENTRY#"
	catPrefix   = "CAT#"
	goalPrefix  = "GOAL#"
)

// DynamoDBStore implements Store using AWS DynamoDB.
// All financial data lives in a single table (single-table design).
type DynamoDBStore struct {
	client    *dynamodb.Client
	tableName string
}

// NewDynamoDBStore creates a DynamoDBStore. If endpoint is non-empty it
// overrides the endpoint (used for DynamoDB Local in docker-compose).
func NewDynamoDBStore(ctx context.Context, tableName, endpoint string) (*DynamoDBStore, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if endpoint != "" {
		opts = append(opts, awsconfig.WithBaseEndpoint(endpoint))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})
	return &DynamoDBStore{client: client, tableName: tableName}, nil
}

// --- DynamoDB item shapes ---

type entryItem struct {
	PK            string `dynamodbav:"PK"`
	SK            string `dynamodbav:"SK"`
	GSI1PK        string `dynamodbav:"GSI1PK"`
	GSI1SK        string `dynamodbav:"GSI1SK"`
	GSI2PK        string `dynamodbav:"GSI2PK"`
	GSI2SK        string `dynamodbav:"GSI2SK"`
	EntryID       string `dynamodbav:"EntryID"`
	UserID        string `dynamodbav:"UserID"`
	Date          string `dynamodbav:"Date"` // RFC3339
	Amount        int64  `dynamodbav:"Amount"`
	Category      string `dynamodbav:"Category"`
	Type          string `dynamodbav:"Type"`
	Description   string `dynamodbav:"Description"`
	DueDate       string `dynamodbav:"DueDate"` // RFC3339 or ""
	PaymentStatus string `dynamodbav:"PaymentStatus"`
	PaymentDate   string `dynamodbav:"PaymentDate"` // RFC3339 or ""
	Supplier      string `dynamodbav:"Supplier"`
	Source        string `dynamodbav:"Source"`
	CreatedAt     string `dynamodbav:"CreatedAt"`
	UpdatedAt     string `dynamodbav:"UpdatedAt"`
}

type categoryItem struct {
	PK      string `dynamodbav:"PK"`
	SK      string `dynamodbav:"SK"`
	UserID  string `dynamodbav:"UserID"`
	Slug    string `dynamodbav:"Slug"`
	Label   string `dynamodbav:"Label"`
	Type    string `dynamodbav:"Type"`
	Default bool   `dynamodbav:"Default"`
}

func entryToItem(e domain.FinancialEntry) entryItem {
	dueDate := ""
	if e.DueDate != nil {
		dueDate = e.DueDate.UTC().Format(time.RFC3339)
	}
	payDate := ""
	if e.PaymentDate != nil {
		payDate = e.PaymentDate.UTC().Format(time.RFC3339)
	}
	dateStr := e.Date.Format("2006-01-02")
	status := string(e.PaymentStatus)

	return entryItem{
		PK:            pkPrefix + e.UserID,
		SK:            entryPrefix + dateStr + "#" + e.EntryID,
		GSI1PK:        pkPrefix + e.UserID,
		GSI1SK:        e.Category + "#" + dateStr,
		GSI2PK:        pkPrefix + e.UserID,
		GSI2SK:        status + "#" + dueDate,
		EntryID:       e.EntryID,
		UserID:        e.UserID,
		Date:          e.Date.UTC().Format(time.RFC3339),
		Amount:        e.Amount,
		Category:      e.Category,
		Type:          string(e.Type),
		Description:   e.Description,
		DueDate:       dueDate,
		PaymentStatus: status,
		PaymentDate:   payDate,
		Supplier:      e.Supplier,
		Source:        e.Source,
		CreatedAt:     e.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     e.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func itemToEntry(item entryItem) (domain.FinancialEntry, error) {
	date, err := time.Parse(time.RFC3339, item.Date)
	if err != nil {
		return domain.FinancialEntry{}, fmt.Errorf("parse Date: %w", err)
	}
	createdAt, _ := time.Parse(time.RFC3339, item.CreatedAt)
	updatedAt, _ := time.Parse(time.RFC3339, item.UpdatedAt)

	var dueDate *time.Time
	if item.DueDate != "" {
		t, err := time.Parse(time.RFC3339, item.DueDate)
		if err == nil {
			dueDate = &t
		}
	}
	var payDate *time.Time
	if item.PaymentDate != "" {
		t, err := time.Parse(time.RFC3339, item.PaymentDate)
		if err == nil {
			payDate = &t
		}
	}

	return domain.FinancialEntry{
		UserID:        item.UserID,
		EntryID:       item.EntryID,
		Date:          date,
		Amount:        item.Amount,
		Category:      item.Category,
		Type:          domain.EntryType(item.Type),
		Description:   item.Description,
		DueDate:       dueDate,
		PaymentStatus: domain.PaymentStatus(item.PaymentStatus),
		PaymentDate:   payDate,
		Supplier:      item.Supplier,
		Source:        item.Source,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}, nil
}

// --- Entries ---

func (s *DynamoDBStore) SaveEntry(ctx context.Context, entry domain.FinancialEntry) error {
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

func (s *DynamoDBStore) GetEntry(ctx context.Context, userID, entryID string) (domain.FinancialEntry, error) {
	// We need the date to build the SK — scan by EntryID using a filter.
	// For simplicity in a 2-user system, query by PK and filter on EntryID.
	entries, err := s.ListEntries(ctx, userID, EntryFilter{})
	if err != nil {
		return domain.FinancialEntry{}, err
	}
	for _, e := range entries {
		if e.EntryID == entryID {
			return e, nil
		}
	}
	return domain.FinancialEntry{}, fmt.Errorf("entry %q not found", entryID)
}

func (s *DynamoDBStore) ListEntries(ctx context.Context, userID string, filter EntryFilter) ([]domain.FinancialEntry, error) {
	keyCondition := "PK = :pk AND begins_with(SK, :prefix)"
	exprValues := map[string]types.AttributeValue{
		":pk":     &types.AttributeValueMemberS{Value: pkPrefix + userID},
		":prefix": &types.AttributeValueMemberS{Value: entryPrefix},
	}

	// Date range on SK
	if filter.From != nil && filter.To != nil {
		keyCondition = "PK = :pk AND SK BETWEEN :from AND :to"
		delete(exprValues, ":prefix")
		exprValues[":from"] = &types.AttributeValueMemberS{
			Value: entryPrefix + filter.From.Format("2006-01-02"),
		}
		exprValues[":to"] = &types.AttributeValueMemberS{
			Value: entryPrefix + filter.To.Format("2006-01-02") + "#\xff",
		}
	} else if filter.From != nil {
		keyCondition = "PK = :pk AND SK >= :from"
		delete(exprValues, ":prefix")
		exprValues[":from"] = &types.AttributeValueMemberS{
			Value: entryPrefix + filter.From.Format("2006-01-02"),
		}
	} else if filter.To != nil {
		keyCondition = "PK = :pk AND SK <= :to"
		delete(exprValues, ":prefix")
		exprValues[":to"] = &types.AttributeValueMemberS{
			Value: entryPrefix + filter.To.Format("2006-01-02") + "#\xff",
		}
	}

	var filterExpr *string
	var filterNames map[string]string

	var filters []string
	if filter.Category != "" {
		filters = append(filters, "Category = :cat")
		exprValues[":cat"] = &types.AttributeValueMemberS{Value: filter.Category}
	}
	if filter.Status != "" {
		filters = append(filters, "PaymentStatus = :status")
		exprValues[":status"] = &types.AttributeValueMemberS{Value: string(filter.Status)}
	}
	if filter.Type != "" {
		filters = append(filters, "#t = :type")
		exprValues[":type"] = &types.AttributeValueMemberS{Value: string(filter.Type)}
		filterNames = map[string]string{"#t": "Type"}
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
	if filterNames != nil {
		input.ExpressionAttributeNames = filterNames
	}

	var entries []domain.FinancialEntry
	paginator := dynamodb.NewQueryPaginator(s.client, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("query entries: %w", err)
		}
		for _, raw := range page.Items {
			var item entryItem
			if err := attributevalue.UnmarshalMap(raw, &item); err != nil {
				continue
			}
			e, err := itemToEntry(item)
			if err != nil {
				continue
			}
			entries = append(entries, e)
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Date.After(entries[j].Date)
	})
	return entries, nil
}

func (s *DynamoDBStore) UpdateEntry(ctx context.Context, entry domain.FinancialEntry) error {
	return s.SaveEntry(ctx, entry) // PutItem is idempotent on the same PK+SK
}

func (s *DynamoDBStore) DeleteEntry(ctx context.Context, userID, entryID string) error {
	entry, err := s.GetEntry(ctx, userID, entryID)
	if err != nil {
		return err
	}
	dateStr := entry.Date.Format("2006-01-02")
	_, err = s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: pkPrefix + userID},
			"SK": &types.AttributeValueMemberS{Value: entryPrefix + dateStr + "#" + entryID},
		},
	})
	return err
}

// --- Summaries ---

func (s *DynamoDBStore) MonthlySummary(ctx context.Context, userID, yearMonth string) (MonthlySummary, error) {
	from, err := time.Parse("2006-01", yearMonth)
	if err != nil {
		return MonthlySummary{}, fmt.Errorf("invalid yearMonth %q: %w", yearMonth, err)
	}
	to := from.AddDate(0, 1, -1)
	entries, err := s.ListEntries(ctx, userID, EntryFilter{From: &from, To: &to})
	if err != nil {
		return MonthlySummary{}, err
	}

	summary := MonthlySummary{Month: yearMonth}
	for _, e := range entries {
		if e.Type == domain.EntryTypeIncome {
			summary.TotalIncome += e.Amount
		} else {
			summary.TotalExpense += e.Amount
		}
	}
	summary.Balance = summary.TotalIncome - summary.TotalExpense
	return summary, nil
}

func (s *DynamoDBStore) CategorySummary(ctx context.Context, userID string, from, to time.Time) ([]CategorySummary, error) {
	entries, err := s.ListEntries(ctx, userID, EntryFilter{From: &from, To: &to})
	if err != nil {
		return nil, err
	}

	totals := make(map[string]*CategorySummary)
	for _, e := range entries {
		if _, ok := totals[e.Category]; !ok {
			totals[e.Category] = &CategorySummary{Category: e.Category, Type: e.Type}
		}
		totals[e.Category].Total += e.Amount
		totals[e.Category].Count++
	}

	result := make([]CategorySummary, 0, len(totals))
	for _, v := range totals {
		result = append(result, *v)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Total > result[j].Total
	})
	return result, nil
}

func (s *DynamoDBStore) CashFlowForecast(ctx context.Context, userID string, days int) ([]CashFlowPoint, error) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	past := days / 2
	future := days - past

	from := today.AddDate(0, 0, -past)
	to := today.AddDate(0, 0, future-1)

	entries, err := s.ListEntries(ctx, userID, EntryFilter{From: &from, To: &to})
	if err != nil {
		return nil, err
	}

	// Compute starting balance: sum all entries before "from"
	startEntries, err := s.ListEntries(ctx, userID, EntryFilter{To: &from})
	if err != nil {
		return nil, err
	}
	var running int64
	for _, e := range startEntries {
		if e.Type == domain.EntryTypeIncome {
			running += e.Amount
		} else {
			running -= e.Amount
		}
	}

	type dayTotals struct{ income, expense int64 }
	byDay := make(map[string]*dayTotals)
	for _, e := range entries {
		dueDate := e.DueDate
		if dueDate == nil {
			d := e.Date
			dueDate = &d
		}
		if dueDate.Before(from) || dueDate.After(to) {
			continue
		}
		day := dueDate.Format("2006-01-02")
		if _, ok := byDay[day]; !ok {
			byDay[day] = &dayTotals{}
		}
		if e.Type == domain.EntryTypeIncome {
			byDay[day].income += e.Amount
		} else {
			byDay[day].expense += e.Amount
		}
	}

	points := make([]CashFlowPoint, 0, days)
	for i := 0; i < days; i++ {
		d := from.AddDate(0, 0, i)
		day := d.Format("2006-01-02")
		var inc, exp int64
		if t := byDay[day]; t != nil {
			inc, exp = t.income, t.expense
		}
		running += inc - exp
		points = append(points, CashFlowPoint{
			Date:             day,
			ProjectedIncome:  inc,
			ProjectedExpense: exp,
			RunningBalance:   running,
		})
	}
	return points, nil
}

// --- Goals ---

type goalItem struct {
	PK            string `dynamodbav:"PK"`
	SK            string `dynamodbav:"SK"`
	UserID        string `dynamodbav:"UserID"`
	Month         string `dynamodbav:"Month"`
	RevenueTarget int64  `dynamodbav:"RevenueTarget"`
	ExpenseTarget int64  `dynamodbav:"ExpenseTarget"`
}

func (s *DynamoDBStore) SaveGoal(ctx context.Context, goal domain.Goal) error {
	item := goalItem{
		PK:            pkPrefix + goal.UserID,
		SK:            goalPrefix + goal.Month,
		UserID:        goal.UserID,
		Month:         goal.Month,
		RevenueTarget: goal.RevenueTarget,
		ExpenseTarget: goal.ExpenseTarget,
	}
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal goal: %w", err)
	}
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item:      av,
	})
	return err
}

func (s *DynamoDBStore) GetGoal(ctx context.Context, userID, month string) (domain.Goal, error) {
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: pkPrefix + userID},
			"SK": &types.AttributeValueMemberS{Value: goalPrefix + month},
		},
	})
	if err != nil {
		return domain.Goal{}, fmt.Errorf("get goal: %w", err)
	}
	if out.Item == nil {
		return domain.Goal{}, fmt.Errorf("goal not found for %s/%s", userID, month)
	}
	var item goalItem
	if err := attributevalue.UnmarshalMap(out.Item, &item); err != nil {
		return domain.Goal{}, fmt.Errorf("unmarshal goal: %w", err)
	}
	return domain.Goal{
		UserID:        item.UserID,
		Month:         item.Month,
		RevenueTarget: item.RevenueTarget,
		ExpenseTarget: item.ExpenseTarget,
	}, nil
}

// --- Categories ---

func (s *DynamoDBStore) SaveCategory(ctx context.Context, cat domain.Category) error {
	item := categoryItem{
		PK:      pkPrefix + cat.UserID,
		SK:      catPrefix + cat.Slug,
		UserID:  cat.UserID,
		Slug:    cat.Slug,
		Label:   cat.Label,
		Type:    string(cat.Type),
		Default: cat.Default,
	}
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal category: %w", err)
	}
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item:      av,
	})
	return err
}

func (s *DynamoDBStore) ListCategories(ctx context.Context, userID string) ([]domain.Category, error) {
	out, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.tableName),
		KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     &types.AttributeValueMemberS{Value: pkPrefix + userID},
			":prefix": &types.AttributeValueMemberS{Value: catPrefix},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("query categories: %w", err)
	}

	var cats []domain.Category
	for _, raw := range out.Items {
		var item categoryItem
		if err := attributevalue.UnmarshalMap(raw, &item); err != nil {
			continue
		}
		cats = append(cats, domain.Category{
			UserID:  item.UserID,
			Slug:    item.Slug,
			Label:   item.Label,
			Type:    domain.EntryType(item.Type),
			Default: item.Default,
		})
	}
	sort.Slice(cats, func(i, j int) bool { return cats[i].Slug < cats[j].Slug })
	return cats, nil
}
