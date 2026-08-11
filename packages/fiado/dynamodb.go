package fiado

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/dynamostore"
)

// DynamoDBStore keeps the caderninho in the financial-entries table, as
// neighbours of the entries in the same user partition. No index, no new
// resource, no attribute declared in Terraform: the six questions the
// caderninho answers are a GetItem and three begins_with Queries, and the
// account's spare capacity (3 RCU / 7 WCU on the Always-Free budget, ADR-008)
// has no room for a GSI anyway.
type DynamoDBStore struct {
	client    dynamostore.API
	tableName string
}

var _ Store = (*DynamoDBStore)(nil)

// NewDynamoDBStore creates a store over the real table. A non-empty endpoint
// overrides the resolved one, which is how it points at DynamoDB Local.
func NewDynamoDBStore(ctx context.Context, tableName, endpoint string) (*DynamoDBStore, error) {
	client, err := dynamostore.NewClient(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	return NewDynamoDBStoreWithClient(client, tableName), nil
}

// NewDynamoDBStoreWithClient builds a store over any dynamostore.API, so the
// real request path can be exercised against an in-memory table (ADR-014).
func NewDynamoDBStoreWithClient(client dynamostore.API, tableName string) *DynamoDBStore {
	return &DynamoDBStore{client: client, tableName: tableName}
}

// movementItem is one movement, written twice under two sort keys. Every
// attribute but SK is identical in both copies, so a single unmarshaller reads
// whichever one a query returned.
type movementItem struct {
	PK          string `dynamodbav:"PK"`
	SK          string `dynamodbav:"SK"`
	UserID      string `dynamodbav:"UserID"`
	Client      string `dynamodbav:"Client"`
	Name        string `dynamodbav:"Name"`
	MovementID  string `dynamodbav:"MovementID"`
	Amount      int64  `dynamodbav:"Amount"`
	Date        string `dynamodbav:"Date"`
	Description string `dynamodbav:"Description"`
	CreatedAt   string `dynamodbav:"CreatedAt"`
}

// debtorItem is the latest. It is only ever read as a struct: the writes go
// through an UpdateExpression, because a Put would need the current balance
// and reading it first is what loses a concurrent movement.
type debtorItem struct {
	PK        string `dynamodbav:"PK"`
	SK        string `dynamodbav:"SK"`
	UserID    string `dynamodbav:"UserID"`
	Client    string `dynamodbav:"Client"`
	Name      string `dynamodbav:"Name"`
	Balance   int64  `dynamodbav:"Balance"`
	Since     string `dynamodbav:"Since,omitempty"`
	UpdatedAt string `dynamodbav:"UpdatedAt"`
}

// latestNames maps every attribute the update expressions touch to a
// placeholder. It is not defensive dressing: Name, Date and a dozen other
// ordinary words are DynamoDB reserved words, and an expression that names one
// directly is a ValidationException in production that no fake would have to
// reproduce.
// The name maps are per-expression, not one shared map: DynamoDB rejects an
// ExpressionAttributeNames entry that the expression does not reference
// ("Value provided in ExpressionAttributeNames unused in expressions"), so a
// map covering every attribute of the latest only works for the one update
// that touches every attribute.
func recordNames() map[string]string {
	return map[string]string{
		"#uid":   "UserID",
		"#cli":   "Client",
		"#name":  "Name",
		"#bal":   "Balance",
		"#since": "Since",
		"#upd":   "UpdatedAt",
	}
}

func balanceNames() map[string]string {
	return map[string]string{
		"#bal": "Balance",
		"#upd": "UpdatedAt",
	}
}

func sinceNames() map[string]string {
	return map[string]string{
		"#since": "Since",
		"#bal":   "Balance",
	}
}

func (s *DynamoDBStore) debtorKey(userID, client string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"PK": &types.AttributeValueMemberS{Value: partitionKey(userID)},
		"SK": &types.AttributeValueMemberS{Value: debtorSK(client)},
	}
}

func (s *DynamoDBStore) itemKey(userID, sk string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"PK": &types.AttributeValueMemberS{Value: partitionKey(userID)},
		"SK": &types.AttributeValueMemberS{Value: sk},
	}
}

// Record writes the movement's two copies and moves the balance in one
// transaction, so no state exists in which only one copy is there.
//
// The balance moves by "ADD #bal :amt", which reads nothing first — two
// payments arriving together, or two retries of the same Lambda, still add up
// correctly. The price is that the write cannot know where the balance landed,
// which is why the resulting debtor comes from a read afterwards rather than
// from arithmetic here.
func (s *DynamoDBStore) Record(ctx context.Context, m Movement) (Debtor, error) {
	if err := m.Validate(); err != nil {
		return Debtor{}, err
	}

	dayAV, err := attributevalue.MarshalMap(s.movementCopy(m, daySK(m.Date, m.Client, m.ID)))
	if err != nil {
		return Debtor{}, fmt.Errorf("marshal movimento (dia): %w", err)
	}
	clientAV, err := attributevalue.MarshalMap(s.movementCopy(m, clientSK(m.Client, m.Date, m.ID)))
	if err != nil {
		return Debtor{}, fmt.Errorf("marshal movimento (cliente): %w", err)
	}

	now := time.Now().UTC()
	_, err = s.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{TransactItems: []types.TransactWriteItem{
		{Put: &types.Put{TableName: aws.String(s.tableName), Item: dayAV}},
		{Put: &types.Put{TableName: aws.String(s.tableName), Item: clientAV}},
		{Update: &types.Update{
			TableName: aws.String(s.tableName),
			Key:       s.debtorKey(m.UserID, m.Client),
			// if_not_exists on Since is "the day the balance left zero": it only
			// writes when the client was square, and settleSince below clears it
			// when they get square again.
			UpdateExpression: aws.String(
				"SET #uid = :uid, #cli = :cli, #name = :name, #upd = :now, #since = if_not_exists(#since, :date) ADD #bal :amt",
			),
			ExpressionAttributeNames: recordNames(),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":uid":  &types.AttributeValueMemberS{Value: m.UserID},
				":cli":  &types.AttributeValueMemberS{Value: m.Client},
				":name": &types.AttributeValueMemberS{Value: m.Name},
				":now":  &types.AttributeValueMemberS{Value: now.Format(time.RFC3339)},
				":date": &types.AttributeValueMemberS{Value: m.Date.String()},
				":amt":  &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", m.Amount)},
			},
		}},
	}})
	if err != nil {
		return Debtor{}, fmt.Errorf("registrar movimento: %w", err)
	}

	return s.settleSince(ctx, m.UserID, m.Client)
}

func (s *DynamoDBStore) movementCopy(m Movement, sk string) movementItem {
	return movementItem{
		PK:          partitionKey(m.UserID),
		SK:          sk,
		UserID:      m.UserID,
		Client:      m.Client,
		Name:        m.Name,
		MovementID:  m.ID,
		Amount:      m.Amount,
		Date:        m.Date.String(),
		Description: m.Description,
		CreatedAt:   m.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// Delete removes both copies and takes the amount back out of the balance, in
// one transaction. The movement is read first only to learn its amount — the
// inverse still goes in as an ADD, so the deletion is as safe under a retry as
// the write was.
func (s *DynamoDBStore) Delete(ctx context.Context, userID string, ref Ref) (Debtor, error) {
	if err := ref.Validate(); err != nil {
		return Debtor{}, err
	}

	m, err := s.movement(ctx, userID, ref)
	if err != nil {
		return Debtor{}, err
	}

	now := time.Now().UTC()
	_, err = s.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{TransactItems: []types.TransactWriteItem{
		{Delete: &types.Delete{
			TableName:           aws.String(s.tableName),
			Key:                 s.itemKey(userID, daySK(ref.Date, ref.Client, ref.ID)),
			ConditionExpression: aws.String("attribute_exists(PK)"),
		}},
		{Delete: &types.Delete{
			TableName:           aws.String(s.tableName),
			Key:                 s.itemKey(userID, clientSK(ref.Client, ref.Date, ref.ID)),
			ConditionExpression: aws.String("attribute_exists(PK)"),
		}},
		{Update: &types.Update{
			TableName: aws.String(s.tableName),
			Key:       s.debtorKey(userID, ref.Client),
			// The latest must already exist: a movement without one is the
			// corruption this condition exists to surface, not a row to create.
			ConditionExpression:      aws.String("attribute_exists(PK)"),
			UpdateExpression:         aws.String("SET #upd = :now ADD #bal :amt"),
			ExpressionAttributeNames: balanceNames(),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":now": &types.AttributeValueMemberS{Value: now.Format(time.RFC3339)},
				":amt": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", -m.Amount)},
			},
		}},
	}})
	if err != nil {
		return Debtor{}, notFoundIfConditionFailed(err, ref.ID)
	}

	return s.settleSince(ctx, userID, ref.Client)
}

// movement reads one copy of a movement — the client-ordered one, since a Ref
// addresses it directly.
func (s *DynamoDBStore) movement(ctx context.Context, userID string, ref Ref) (Movement, error) {
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(s.tableName),
		Key:            s.itemKey(userID, clientSK(ref.Client, ref.Date, ref.ID)),
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return Movement{}, fmt.Errorf("ler movimento: %w", err)
	}
	if len(out.Item) == 0 {
		return Movement{}, fmt.Errorf("%w: %q", ErrMovementNotFound, ref.ID)
	}
	var item movementItem
	if err := attributevalue.UnmarshalMap(out.Item, &item); err != nil {
		return Movement{}, fmt.Errorf("unmarshal movimento: %w", err)
	}
	return itemToMovement(item)
}

// settleSince reads the debtor the write just produced and repairs "desde" if
// the balance crossed zero, in either direction.
//
// It exists because the balance moves by a blind ADD: the transaction cannot
// know where it landed, and "desde" is defined by exactly that. The read is not
// an extra cost — callers need the resulting balance anyway — and the two
// repairs are both conditional on the balance still being what was read, so a
// movement arriving in between wins rather than being overwritten.
func (s *DynamoDBStore) settleSince(ctx context.Context, userID, client string) (Debtor, error) {
	d, err := s.Debtor(ctx, userID, client)
	if err != nil {
		return Debtor{}, err
	}

	switch {
	case clearSince(d.Balance) && d.Since != nil:
		if err := s.writeSince(ctx, d, nil); err != nil {
			return Debtor{}, err
		}
		d.Since = nil

	case !clearSince(d.Balance) && d.Since == nil:
		// The balance was zero and a deletion re-opened it — "o joão não me pagou
		// aquilo". The day it last left zero is not in the latest any more, so it
		// is recomputed from the movements, which are the truth. This is the only
		// path that reads a whole history, and only a correction reaches it.
		since, err := s.sinceFromHistory(ctx, userID, client)
		if err != nil {
			return Debtor{}, err
		}
		if since == nil {
			return d, nil
		}
		if err := s.writeSince(ctx, d, since); err != nil {
			return Debtor{}, err
		}
		d.Since = since
	}
	return d, nil
}

// writeSince sets or removes Since, but only while the balance is still the one
// that justified the change.
func (s *DynamoDBStore) writeSince(ctx context.Context, d Debtor, since *domain.CalendarDate) error {
	update := "REMOVE #since"
	values := map[string]types.AttributeValue{
		":bal": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", d.Balance)},
	}
	if since != nil {
		update = "SET #since = :date"
		values[":date"] = &types.AttributeValueMemberS{Value: since.String()}
	}

	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 aws.String(s.tableName),
		Key:                       s.debtorKey(d.UserID, d.Client),
		UpdateExpression:          aws.String(update),
		ConditionExpression:       aws.String("#bal = :bal"),
		ExpressionAttributeNames:  sinceNames(),
		ExpressionAttributeValues: values,
	})
	var condFailed *types.ConditionalCheckFailedException
	if errors.As(err, &condFailed) {
		// Another movement landed between the read and this write, so it already
		// decided what "desde" is. Not an error: the caderninho is not wrong, it
		// is just newer than what this call read.
		return nil
	}
	if err != nil {
		return fmt.Errorf("ajustar desde: %w", err)
	}
	return nil
}

// sinceFromHistory replays a client's movements to find the day their balance
// last left zero.
func (s *DynamoDBStore) sinceFromHistory(ctx context.Context, userID, client string) (*domain.CalendarDate, error) {
	page, err := s.ClientMovements(ctx, userID, client, Page{})
	if err != nil {
		return nil, err
	}
	return sinceFromMovements(oldestFirst(page.Movements)), nil
}

func (s *DynamoDBStore) Debtor(ctx context.Context, userID, client string) (Debtor, error) {
	if client == "" {
		return Debtor{}, ErrNoClient
	}
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key:       s.debtorKey(userID, client),
		// Strongly consistent on purpose: "fiado 40 do joão" and "quanto o joão
		// me deve?" arrive seconds apart, and an eventual read would answer the
		// second one with a number from before the first — confidently.
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return Debtor{}, fmt.Errorf("ler cliente do caderninho: %w", err)
	}
	if len(out.Item) == 0 {
		return Debtor{}, fmt.Errorf("%w: %q", ErrDebtorNotFound, client)
	}
	var item debtorItem
	if err := attributevalue.UnmarshalMap(out.Item, &item); err != nil {
		return Debtor{}, fmt.Errorf("unmarshal cliente: %w", err)
	}
	return itemToDebtor(item)
}

// ListDebtors is one Query on "FIADO#", which returns the latests and nothing
// else — the movements live under sibling prefixes, so they cannot leak in.
func (s *DynamoDBStore) ListDebtors(ctx context.Context, userID string) ([]Debtor, error) {
	var out []Debtor
	paginator := dynamodb.NewQueryPaginator(s.client, &dynamodb.QueryInput{
		TableName:              aws.String(s.tableName),
		KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     &types.AttributeValueMemberS{Value: partitionKey(userID)},
			":prefix": &types.AttributeValueMemberS{Value: debtorPrefix},
		},
		ConsistentRead: aws.Bool(true),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listar caderninho: %w", err)
		}
		for _, raw := range page.Items {
			var item debtorItem
			if err := attributevalue.UnmarshalMap(raw, &item); err != nil {
				return nil, fmt.Errorf("unmarshal cliente: %w", err)
			}
			d, err := itemToDebtor(item)
			if err != nil {
				return nil, err
			}
			out = append(out, d)
		}
	}
	return out, nil
}

func (s *DynamoDBStore) DayMovements(ctx context.Context, userID string, date domain.CalendarDate, page Page) (MovementPage, error) {
	if !date.Valid() {
		return MovementPage{}, fmt.Errorf("data do dia é obrigatória")
	}
	return s.timeline(ctx, userID, dayQueryPrefix(date), true, page)
}

func (s *DynamoDBStore) ClientMovements(ctx context.Context, userID, client string, page Page) (MovementPage, error) {
	if client == "" {
		return MovementPage{}, ErrNoClient
	}
	return s.timeline(ctx, userID, clientQueryPrefix(client), false, page)
}

// timeline serves both movement queries. The sort key already holds the order
// each one wants, so neither sorts in Go: the day reads forward, a client's
// statement reads backward (ScanIndexForward=false) and comes back most recent
// first.
func (s *DynamoDBStore) timeline(ctx context.Context, userID, prefix string, ascending bool, page Page) (MovementPage, error) {
	input := &dynamodb.QueryInput{
		TableName:              aws.String(s.tableName),
		KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     &types.AttributeValueMemberS{Value: partitionKey(userID)},
			":prefix": &types.AttributeValueMemberS{Value: prefix},
		},
		ConsistentRead: aws.Bool(true),
	}
	if !ascending {
		input.ScanIndexForward = aws.Bool(false)
	}
	if page.Cursor != "" {
		// A cursor from another timeline would page through the wrong rows and
		// report them as this client's. Rejecting it is the only safe reading.
		if !strings.HasPrefix(page.Cursor, prefix) {
			return MovementPage{}, fmt.Errorf("cursor %q não pertence a esta listagem", page.Cursor)
		}
		input.ExclusiveStartKey = s.itemKey(userID, page.Cursor)
	}

	// A limited call is one page, and the caller decides whether to ask for the
	// next. An unlimited one is a total — "quanto o João me pagou" — and a total
	// read halfway is a wrong number with no sign of being one, so it walks
	// every page before answering.
	if page.Limit > 0 {
		input.Limit = aws.Int32(int32(page.Limit))
		out, err := s.client.Query(ctx, input)
		if err != nil {
			return MovementPage{}, fmt.Errorf("consultar movimentos: %w", err)
		}
		movements, err := itemsToMovements(out.Items)
		if err != nil {
			return MovementPage{}, err
		}
		result := MovementPage{Movements: movements}
		if len(out.LastEvaluatedKey) > 0 && len(movements) > 0 {
			result.NextCursor = movements[len(movements)-1].sortKey(ascending)
		}
		return result, nil
	}

	var all []Movement
	paginator := dynamodb.NewQueryPaginator(s.client, input)
	for paginator.HasMorePages() {
		out, err := paginator.NextPage(ctx)
		if err != nil {
			return MovementPage{}, fmt.Errorf("consultar movimentos: %w", err)
		}
		movements, err := itemsToMovements(out.Items)
		if err != nil {
			return MovementPage{}, err
		}
		all = append(all, movements...)
	}
	return MovementPage{Movements: all}, nil
}

// sortKey rebuilds the sort key a movement was returned under, which is what a
// cursor is.
func (m Movement) sortKey(ascending bool) string {
	if ascending {
		return daySK(m.Date, m.Client, m.ID)
	}
	return clientSK(m.Client, m.Date, m.ID)
}

func itemsToMovements(raws []map[string]types.AttributeValue) ([]Movement, error) {
	out := make([]Movement, 0, len(raws))
	for _, raw := range raws {
		var item movementItem
		if err := attributevalue.UnmarshalMap(raw, &item); err != nil {
			return nil, fmt.Errorf("unmarshal movimento: %w", err)
		}
		m, err := itemToMovement(item)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func itemToMovement(item movementItem) (Movement, error) {
	date, err := domain.ParseCalendarDate(item.Date)
	if err != nil {
		return Movement{}, fmt.Errorf("movimento %q com data inválida %q: %w", item.MovementID, item.Date, err)
	}
	createdAt, _ := time.Parse(time.RFC3339, item.CreatedAt)
	return Movement{
		UserID:      item.UserID,
		Client:      item.Client,
		Name:        item.Name,
		ID:          item.MovementID,
		Date:        date,
		Amount:      item.Amount,
		Description: item.Description,
		CreatedAt:   createdAt,
	}, nil
}

func itemToDebtor(item debtorItem) (Debtor, error) {
	d := Debtor{
		UserID:  item.UserID,
		Client:  item.Client,
		Name:    item.Name,
		Balance: item.Balance,
	}
	if item.Since != "" {
		since, err := domain.ParseCalendarDate(item.Since)
		if err != nil {
			return Debtor{}, fmt.Errorf("cliente %q com desde inválido %q: %w", item.Client, item.Since, err)
		}
		d.Since = &since
	}
	d.UpdatedAt, _ = time.Parse(time.RFC3339, item.UpdatedAt)
	return d, nil
}

// notFoundIfConditionFailed turns a failed attribute_exists into
// ErrMovementNotFound, so a caller tells "that line is not there" from "the
// write failed" with errors.Is instead of by inspecting AWS error types.
func notFoundIfConditionFailed(err error, id string) error {
	if err == nil {
		return nil
	}
	var condFailed *types.ConditionalCheckFailedException
	var txCanceled *types.TransactionCanceledException
	if errors.As(err, &condFailed) {
		return fmt.Errorf("%w: %q", ErrMovementNotFound, id)
	}
	if errors.As(err, &txCanceled) {
		for _, reason := range txCanceled.CancellationReasons {
			if reason.Code != nil && *reason.Code == "ConditionalCheckFailed" {
				return fmt.Errorf("%w: %q", ErrMovementNotFound, id)
			}
		}
	}
	return fmt.Errorf("apagar movimento: %w", err)
}
