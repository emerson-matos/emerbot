package payments

import (
	"context"
	"fmt"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/emerson/emerbot/packages/domain"
)

// Canonical payment items share the existing finance table (single-table
// design). They live in the same USER#<ledger> partition as FinancialEntry so
// the dashboard reads them through the existing auth path, distinguished by
// their SK prefix. Each item stores its originating SourceDate so Save can
// replace exactly one (Provider, SourceDate) import even though receivables
// carry future, varied ExpectedDates.
const (
	pkPrefix   = "USER#"
	salePrefix = "SALE#"
	recvPrefix = "RECV#"
	payPrefix  = "PAYMT#"

	// skHigh is appended to an upper date bound so a BETWEEN on the SK includes
	// every item on the "to" day regardless of the trailing id/parcela.
	skHigh = "#\xff"
)

func saleSK(date domain.CalendarDate, id SaleID) string {
	return salePrefix + date.String() + "#" + string(id)
}

func recvSK(date domain.CalendarDate, id SaleID, parcela int) string {
	return recvPrefix + date.String() + "#" + string(id) + "#" + strconv.Itoa(parcela)
}

func paySK(date domain.CalendarDate, id SaleID) string {
	return payPrefix + date.String() + "#" + string(id)
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
	PK          string `dynamodbav:"PK"`
	SK          string `dynamodbav:"SK"`
	Provider    string `dynamodbav:"Provider"`
	SourceDate  string `dynamodbav:"SourceDate"`
	SaleID      string `dynamodbav:"SaleID"`
	PaymentDate string `dynamodbav:"PaymentDate"`
	Amount      int64  `dynamodbav:"Amount"`
}

// DynamoDBRepository implements Repository against the shared finance table.
type DynamoDBRepository struct {
	client    *dynamodb.Client
	tableName string
	ledgerID  string // partition all payment items belong to (the pharmacy ledger)
}

// NewDynamoDBRepository creates a repository bound to one table and ledger
// partition. If endpoint is non-empty it overrides the endpoint (DynamoDB Local).
func NewDynamoDBRepository(ctx context.Context, tableName, endpoint, ledgerID string) (*DynamoDBRepository, error) {
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
	return &DynamoDBRepository{client: client, tableName: tableName, ledgerID: ledgerID}, nil
}

func (r *DynamoDBRepository) pk() string { return pkPrefix + r.ledgerID }

// Save replaces exactly the prior (Provider, SourceDate) set with result. It
// deletes previously-imported items for that import that are absent from the new
// set, then puts every new item (a put overwrites a same-key item), so re-import
// is idempotent. Writes go out in atomic chunks of at most maxTransactWriteItems.
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

	var writes []types.TransactWriteItem
	for _, sk := range oldKeys {
		if newKeys[sk] {
			continue // will be overwritten by a Put — avoid a same-item conflict
		}
		writes = append(writes, types.TransactWriteItem{Delete: &types.Delete{
			TableName: aws.String(r.tableName),
			Key: map[string]types.AttributeValue{
				"PK": &types.AttributeValueMemberS{Value: r.pk()},
				"SK": &types.AttributeValueMemberS{Value: sk},
			},
		}})
	}
	for _, it := range newItems {
		writes = append(writes, types.TransactWriteItem{Put: &types.Put{
			TableName: aws.String(r.tableName),
			Item:      it,
		}})
	}
	return r.transactWrite(ctx, writes)
}

// maxTransactWriteItems is DynamoDB's hard per-call limit for TransactWriteItems.
const maxTransactWriteItems = 100

func (r *DynamoDBRepository) transactWrite(ctx context.Context, writes []types.TransactWriteItem) error {
	for start := 0; start < len(writes); start += maxTransactWriteItems {
		end := min(start+maxTransactWriteItems, len(writes))
		if _, err := r.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
			TransactItems: writes[start:end],
		}); err != nil {
			return fmt.Errorf("transact write payments %d-%d: %w", start, end, err)
		}
	}
	return nil
}

// marshalResult marshals every canonical item and returns the marshalled items
// plus the set of their SKs.
func (r *DynamoDBRepository) marshalResult(result ImportResult) ([]map[string]types.AttributeValue, map[string]bool, error) {
	items := make([]map[string]types.AttributeValue, 0, len(result.Sales)+len(result.Receivables)+len(result.Payments))
	keys := make(map[string]bool)
	sd := result.SourceDate.String()

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
		sk := paySK(p.PaymentDate, p.SaleID)
		if err := add(sk, payItem{
			PK: r.pk(), SK: sk, Provider: string(p.Provider), SourceDate: sd,
			SaleID: string(p.SaleID), PaymentDate: p.PaymentDate.String(), Amount: p.Amount,
		}); err != nil {
			return nil, nil, err
		}
	}
	return items, keys, nil
}

// existingKeys returns the SKs of items already stored for this (provider,
// sourceDate) import, across all three prefixes.
func (r *DynamoDBRepository) existingKeys(ctx context.Context, provider Provider, sourceDate domain.CalendarDate) ([]string, error) {
	var keys []string
	for _, prefix := range []string{salePrefix, recvPrefix, payPrefix} {
		paginator := dynamodb.NewQueryPaginator(r.client, &dynamodb.QueryInput{
			TableName:              aws.String(r.tableName),
			KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
			FilterExpression:       aws.String("Provider = :prov AND SourceDate = :sd"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pk":     &types.AttributeValueMemberS{Value: r.pk()},
				":prefix": &types.AttributeValueMemberS{Value: prefix},
				":prov":   &types.AttributeValueMemberS{Value: string(provider)},
				":sd":     &types.AttributeValueMemberS{Value: sourceDate.String()},
			},
			ProjectionExpression: aws.String("SK"),
		})
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				return nil, fmt.Errorf("query existing %s: %w", prefix, err)
			}
			for _, raw := range page.Items {
				if sk, ok := raw["SK"].(*types.AttributeValueMemberS); ok {
					keys = append(keys, sk.Value)
				}
			}
		}
	}
	return keys, nil
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
