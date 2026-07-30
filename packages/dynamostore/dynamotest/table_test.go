package dynamotest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// The fake is test infrastructure, so it carries its own tests: a bug here
// would show up as false confidence in every store that uses it.

func s(v string) types.AttributeValue { return &types.AttributeValueMemberS{Value: v} }
func n(v string) types.AttributeValue { return &types.AttributeValueMemberN{Value: v} }

func newTable(t *testing.T, cfg Config) *Table {
	t.Helper()
	if cfg.Key.Hash == "" {
		cfg.Key = Key{Hash: "PK", Range: "SK"}
	}
	return New(cfg)
}

func seed(t *testing.T, tbl *Table, items ...map[string]types.AttributeValue) {
	t.Helper()
	if err := tbl.Seed(items...); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func item(pk, sk string, extra ...any) map[string]types.AttributeValue {
	it := map[string]types.AttributeValue{"PK": s(pk), "SK": s(sk)}
	for i := 0; i+1 < len(extra); i += 2 {
		it[extra[i].(string)] = extra[i+1].(types.AttributeValue)
	}
	return it
}

func query(t *testing.T, tbl *Table, in *dynamodb.QueryInput) *dynamodb.QueryOutput {
	t.Helper()
	if in.TableName == nil {
		in.TableName = aws.String(tbl.Name())
	}
	out, err := tbl.Query(context.Background(), in)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	return out
}

func sks(out *dynamodb.QueryOutput) []string {
	got := make([]string, 0, len(out.Items))
	for _, it := range out.Items {
		got = append(got, it["SK"].(*types.AttributeValueMemberS).Value)
	}
	return got
}

func TestPutGetDelete(t *testing.T) {
	tbl := newTable(t, Config{})
	ctx := context.Background()

	if _, err := tbl.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(tbl.Name()),
		Item:      item("u1", "a", "Amount", n("100")),
	}); err != nil {
		t.Fatalf("put: %v", err)
	}

	out, err := tbl.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(tbl.Name()),
		Key:       map[string]types.AttributeValue{"PK": s("u1"), "SK": s("a")},
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := out.Item["Amount"].(*types.AttributeValueMemberN).Value; got != "100" {
		t.Fatalf("Amount = %q, want 100", got)
	}

	// A miss returns a nil item rather than an error, as DynamoDB does.
	miss, err := tbl.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(tbl.Name()),
		Key:       map[string]types.AttributeValue{"PK": s("u1"), "SK": s("nope")},
	})
	if err != nil {
		t.Fatalf("get miss: %v", err)
	}
	if miss.Item != nil {
		t.Fatalf("expected nil item for a miss, got %v", miss.Item)
	}

	if _, err := tbl.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(tbl.Name()),
		Key:       map[string]types.AttributeValue{"PK": s("u1"), "SK": s("a")},
	}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if tbl.Len() != 0 {
		t.Fatalf("expected empty table after delete, got %d items", tbl.Len())
	}
}

func TestReturnedItemsAreCopies(t *testing.T) {
	tbl := newTable(t, Config{})
	seed(t, tbl, item("u1", "a", "Amount", n("100")))

	out, err := tbl.GetItem(context.Background(), &dynamodb.GetItemInput{
		TableName: aws.String(tbl.Name()),
		Key:       map[string]types.AttributeValue{"PK": s("u1"), "SK": s("a")},
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	out.Item["Amount"] = n("999") // a caller mutating its result must not alter the table

	again, _ := tbl.GetItem(context.Background(), &dynamodb.GetItemInput{
		TableName: aws.String(tbl.Name()),
		Key:       map[string]types.AttributeValue{"PK": s("u1"), "SK": s("a")},
	})
	if got := again.Item["Amount"].(*types.AttributeValueMemberN).Value; got != "100" {
		t.Fatalf("stored Amount = %q, want 100 — the fake handed out a live reference", got)
	}
}

func TestPutRejectsItemMissingKey(t *testing.T) {
	tbl := newTable(t, Config{})
	_, err := tbl.PutItem(context.Background(), &dynamodb.PutItemInput{
		TableName: aws.String(tbl.Name()),
		Item:      map[string]types.AttributeValue{"PK": s("u1")}, // no SK
	})
	if err == nil || !strings.Contains(err.Error(), "range key") {
		t.Fatalf("expected a missing-range-key error, got %v", err)
	}
}

func TestWrongTableNameIsRejected(t *testing.T) {
	tbl := newTable(t, Config{Name: "real"})
	_, err := tbl.PutItem(context.Background(), &dynamodb.PutItemInput{
		TableName: aws.String("other"),
		Item:      item("u1", "a"),
	})
	if err == nil || !strings.Contains(err.Error(), "targets table") {
		t.Fatalf("expected a table-name error, got %v", err)
	}
}

func TestConditionalPut(t *testing.T) {
	tbl := newTable(t, Config{Key: Key{Hash: "Phone"}})
	ctx := context.Background()
	put := func(phone, exp, cond string) error {
		in := &dynamodb.PutItemInput{
			TableName: aws.String(tbl.Name()),
			Item:      map[string]types.AttributeValue{"Phone": s(phone), "ExpiresAt": n(exp)},
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":exp": n(exp),
			},
		}
		if cond != "" {
			in.ConditionExpression = aws.String(cond)
		}
		_, err := tbl.PutItem(ctx, in)
		return err
	}

	const cond = "attribute_not_exists(ExpiresAt) OR ExpiresAt < :exp"
	if err := put("+55", "100", cond); err != nil {
		t.Fatalf("first put: %v", err)
	}
	if err := put("+55", "200", cond); err != nil {
		t.Fatalf("extending put: %v", err)
	}

	// Moving the expiry backwards must fail the condition.
	err := put("+55", "150", cond)
	var failed *types.ConditionalCheckFailedException
	if !errors.As(err, &failed) {
		t.Fatalf("expected ConditionalCheckFailedException, got %v", err)
	}

	out, _ := tbl.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(tbl.Name()),
		Key:       map[string]types.AttributeValue{"Phone": s("+55")},
	})
	if got := out.Item["ExpiresAt"].(*types.AttributeValueMemberN).Value; got != "200" {
		t.Fatalf("ExpiresAt = %q, want 200 — the rejected write leaked through", got)
	}
}

func TestQueryRangeConditions(t *testing.T) {
	tbl := newTable(t, Config{})
	seed(
		t, tbl,
		item("u1", "2026-01-05"), item("u1", "2026-02-10"),
		item("u1", "2026-03-15"), item("u2", "2026-02-01"),
	)

	cases := []struct {
		name string
		expr string
		vals map[string]types.AttributeValue
		want []string
	}{
		{
			"hash only", "PK = :pk",
			map[string]types.AttributeValue{":pk": s("u1")},
			[]string{"2026-01-05", "2026-02-10", "2026-03-15"},
		},
		{
			"between", "PK = :pk AND SK BETWEEN :lo AND :hi",
			map[string]types.AttributeValue{":pk": s("u1"), ":lo": s("2026-02-01"), ":hi": s("2026-03-01")},
			[]string{"2026-02-10"},
		},
		{
			"less than", "PK = :pk AND SK < :c",
			map[string]types.AttributeValue{":pk": s("u1"), ":c": s("2026-02-10")},
			[]string{"2026-01-05"},
		},
		{
			"greater or equal", "PK = :pk AND SK >= :f",
			map[string]types.AttributeValue{":pk": s("u1"), ":f": s("2026-02-10")},
			[]string{"2026-02-10", "2026-03-15"},
		},
		{
			"begins_with", "PK = :pk AND begins_with(SK, :p)",
			map[string]types.AttributeValue{":pk": s("u1"), ":p": s("2026-02")},
			[]string{"2026-02-10"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := query(t, tbl, &dynamodb.QueryInput{
				KeyConditionExpression:    aws.String(tc.expr),
				ExpressionAttributeValues: tc.vals,
			})
			assertSKs(t, out, tc.want)
		})
	}
}

func TestQueryScanIndexForwardFalseReversesOrder(t *testing.T) {
	tbl := newTable(t, Config{})
	seed(t, tbl, item("u1", "a"), item("u1", "b"), item("u1", "c"))

	out := query(t, tbl, &dynamodb.QueryInput{
		KeyConditionExpression:    aws.String("PK = :pk"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":pk": s("u1")},
		ScanIndexForward:          aws.Bool(false),
	})
	assertSKs(t, out, []string{"c", "b", "a"})
}

func TestQueryFilterExpression(t *testing.T) {
	tbl := newTable(t, Config{})
	seed(
		t, tbl,
		item("u1", "a", "Category", s("mercado"), "Desc", s("pão na padaria")),
		item("u1", "b", "Category", s("aluguel"), "Desc", s("aluguel loja")),
		item("u1", "c", "Category", s("mercado"), "Desc", s("feira")),
	)

	out := query(t, tbl, &dynamodb.QueryInput{
		KeyConditionExpression: aws.String("PK = :pk"),
		FilterExpression:       aws.String("Category = :cat"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk": s("u1"), ":cat": s("mercado"),
		},
	})
	assertSKs(t, out, []string{"a", "c"})

	out = query(t, tbl, &dynamodb.QueryInput{
		KeyConditionExpression:   aws.String("PK = :pk"),
		FilterExpression:         aws.String("contains(#d, :term)"),
		ExpressionAttributeNames: map[string]string{"#d": "Desc"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk": s("u1"), ":term": s("aluguel"),
		},
	})
	assertSKs(t, out, []string{"b"})
}

func TestQueryLimitAppliesBeforeFilter(t *testing.T) {
	tbl := newTable(t, Config{})
	seed(
		t, tbl,
		item("u1", "a", "Type", s("expense")),
		item("u1", "b", "Type", s("income")),
		item("u1", "c", "Type", s("income")),
	)

	// Reading 2 items yields only 1 match: DynamoDB charges the read before
	// the filter runs, so Count < Limit does not mean the data is exhausted.
	out := query(t, tbl, &dynamodb.QueryInput{
		KeyConditionExpression:    aws.String("PK = :pk"),
		FilterExpression:          aws.String("#t = :type"),
		ExpressionAttributeNames:  map[string]string{"#t": "Type"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":pk": s("u1"), ":type": s("income")},
		Limit:                     aws.Int32(2),
	})
	assertSKs(t, out, []string{"b"})
	if out.ScannedCount != 2 {
		t.Fatalf("ScannedCount = %d, want 2", out.ScannedCount)
	}
	if len(out.LastEvaluatedKey) == 0 {
		t.Fatal("expected a LastEvaluatedKey: item c was never read")
	}
}

func TestQueryPaginationRoundTrip(t *testing.T) {
	tbl := newTable(t, Config{PageSize: 2})
	for _, sk := range []string{"a", "b", "c", "d", "e"} {
		seed(t, tbl, item("u1", sk))
	}

	paginator := dynamodb.NewQueryPaginator(tbl, &dynamodb.QueryInput{
		TableName:                 aws.String(tbl.Name()),
		KeyConditionExpression:    aws.String("PK = :pk"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":pk": s("u1")},
	})

	var got []string
	pages := 0
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.Background())
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		pages++
		got = append(got, sks(page)...)
	}

	if pages != 3 {
		t.Fatalf("pages = %d, want 3 (5 items at 2 per page)", pages)
	}
	want := []string{"a", "b", "c", "d", "e"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("paginated items = %v, want %v", got, want)
	}
}

func TestQueryOnSparseGSI(t *testing.T) {
	tbl := newTable(t, Config{
		GSIs: map[string]Key{"GSI2": {Hash: "GSI2PK", Range: "GSI2SK"}},
	})
	seed(
		t, tbl,
		item("u1", "ENTRY#1", "GSI2PK", s("u1"), "GSI2SK", s("2026-03-01#1")),
		item("u1", "ENTRY#2", "GSI2PK", s("u1"), "GSI2SK", s("2026-01-01#2")),
		item("u1", "GOAL"), // no GSI2 attributes: must not appear in the index
	)

	out := query(t, tbl, &dynamodb.QueryInput{
		IndexName:                 aws.String("GSI2"),
		KeyConditionExpression:    aws.String("GSI2PK = :pk"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":pk": s("u1")},
	})
	// Ordered by GSI2SK, not by the table's own SK.
	assertSKs(t, out, []string{"ENTRY#2", "ENTRY#1"})
}

func TestQueryUnknownIndexIsRejected(t *testing.T) {
	tbl := newTable(t, Config{})
	_, err := tbl.Query(context.Background(), &dynamodb.QueryInput{
		TableName:                 aws.String(tbl.Name()),
		IndexName:                 aws.String("nope"),
		KeyConditionExpression:    aws.String("PK = :pk"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":pk": s("u1")},
	})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected an unknown-index error, got %v", err)
	}
}

func TestQueryKeyConditionOnNonKeyAttributeIsRejected(t *testing.T) {
	tbl := newTable(t, Config{})
	_, err := tbl.Query(context.Background(), &dynamodb.QueryInput{
		TableName:                 aws.String(tbl.Name()),
		KeyConditionExpression:    aws.String("PK = :pk AND Category = :cat"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":pk": s("u1"), ":cat": s("x")},
	})
	if err == nil || !strings.Contains(err.Error(), "not a key attribute") {
		t.Fatalf("expected a non-key-attribute error, got %v", err)
	}
}

func TestBatchWrite(t *testing.T) {
	tbl := newTable(t, Config{})
	seed(t, tbl, item("u1", "old"))
	ctx := context.Background()

	_, err := tbl.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
		RequestItems: map[string][]types.WriteRequest{tbl.Name(): {
			{DeleteRequest: &types.DeleteRequest{Key: map[string]types.AttributeValue{"PK": s("u1"), "SK": s("old")}}},
			{PutRequest: &types.PutRequest{Item: item("u1", "new")}},
		}},
	})
	if err != nil {
		t.Fatalf("batch write: %v", err)
	}

	out := query(t, tbl, &dynamodb.QueryInput{
		KeyConditionExpression:    aws.String("PK = :pk"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":pk": s("u1")},
	})
	assertSKs(t, out, []string{"new"})
}

func TestBatchWriteThrottleReturnsUnprocessed(t *testing.T) {
	tbl := newTable(t, Config{})
	tbl.ThrottleBatches(1)

	writes := []types.WriteRequest{{PutRequest: &types.PutRequest{Item: item("u1", "a")}}}
	out, err := tbl.BatchWriteItem(context.Background(), &dynamodb.BatchWriteItemInput{
		RequestItems: map[string][]types.WriteRequest{tbl.Name(): writes},
	})
	if err != nil {
		t.Fatalf("batch write: %v", err)
	}
	if len(out.UnprocessedItems[tbl.Name()]) != 1 {
		t.Fatalf("expected 1 unprocessed item, got %d", len(out.UnprocessedItems[tbl.Name()]))
	}
	if tbl.Len() != 0 {
		t.Fatal("a throttled batch must not apply any of its writes")
	}
}

func TestBatchWriteRejectsOversizedBatch(t *testing.T) {
	tbl := newTable(t, Config{})
	writes := make([]types.WriteRequest, 26)
	for i := range writes {
		writes[i] = types.WriteRequest{PutRequest: &types.PutRequest{Item: item("u1", string(rune('a'+i)))}}
	}
	_, err := tbl.BatchWriteItem(context.Background(), &dynamodb.BatchWriteItemInput{
		RequestItems: map[string][]types.WriteRequest{tbl.Name(): writes},
	})
	if err == nil || !strings.Contains(err.Error(), "at most 25") {
		t.Fatalf("expected an oversized-batch error, got %v", err)
	}
}

func TestTransactWriteIsAtomic(t *testing.T) {
	tbl := newTable(t, Config{})
	seed(t, tbl, item("u1", "old"))
	ctx := context.Background()

	// The second item is missing its range key, so nothing may be applied.
	_, err := tbl.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{Delete: &types.Delete{TableName: aws.String(tbl.Name()), Key: map[string]types.AttributeValue{"PK": s("u1"), "SK": s("old")}}},
			{Put: &types.Put{TableName: aws.String(tbl.Name()), Item: map[string]types.AttributeValue{"PK": s("u1")}}},
		},
	})
	if err == nil {
		t.Fatal("expected the transaction to fail")
	}
	if tbl.Len() != 1 {
		t.Fatalf("table has %d items, want the original 1 — a failed transaction was partially applied", tbl.Len())
	}
}

func TestTransactWriteRejectsDuplicateKeys(t *testing.T) {
	tbl := newTable(t, Config{})
	_, err := tbl.TransactWriteItems(context.Background(), &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{Put: &types.Put{TableName: aws.String(tbl.Name()), Item: item("u1", "a")}},
			{Put: &types.Put{TableName: aws.String(tbl.Name()), Item: item("u1", "a")}},
		},
	})
	var cancelled *types.TransactionCanceledException
	if !errors.As(err, &cancelled) {
		t.Fatalf("expected TransactionCanceledException, got %v", err)
	}
}

func TestTransactWriteRejectsOversizedTransaction(t *testing.T) {
	tbl := newTable(t, Config{})
	items := make([]types.TransactWriteItem, 101)
	for i := range items {
		items[i] = types.TransactWriteItem{Put: &types.Put{
			TableName: aws.String(tbl.Name()),
			Item:      item("u1", string(rune(i))),
		}}
	}
	_, err := tbl.TransactWriteItems(context.Background(), &dynamodb.TransactWriteItemsInput{TransactItems: items})
	if err == nil || !strings.Contains(err.Error(), "at most 100") {
		t.Fatalf("expected an oversized-transaction error, got %v", err)
	}
}

func TestScanPaginatesAndFilters(t *testing.T) {
	tbl := newTable(t, Config{PageSize: 2})
	seed(
		t, tbl,
		item("u1", "NOTIFPREFS"), item("u1", "ENTRY#1"),
		item("u2", "NOTIFPREFS"), item("u2", "ENTRY#1"),
		item("u3", "NOTIFPREFS"),
	)

	paginator := dynamodb.NewScanPaginator(tbl, &dynamodb.ScanInput{
		TableName:                 aws.String(tbl.Name()),
		FilterExpression:          aws.String("SK = :sk"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":sk": s("NOTIFPREFS")},
	})

	var pks []string
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.Background())
		if err != nil {
			t.Fatalf("scan page: %v", err)
		}
		for _, it := range page.Items {
			pks = append(pks, it["PK"].(*types.AttributeValueMemberS).Value)
		}
	}

	// All three prefs items must survive being spread across pages, and no
	// entry item may leak through the filter.
	if got := strings.Join(pks, ","); got != "u1,u2,u3" {
		t.Fatalf("scanned PKs = %q, want u1,u2,u3", got)
	}
}

func TestFailNextInjectsErrorOnce(t *testing.T) {
	tbl := newTable(t, Config{})
	boom := errors.New("boom")
	tbl.FailNext("PutItem", boom)

	if _, err := tbl.PutItem(context.Background(), &dynamodb.PutItemInput{
		TableName: aws.String(tbl.Name()), Item: item("u1", "a"),
	}); !errors.Is(err, boom) {
		t.Fatalf("first put error = %v, want boom", err)
	}
	if _, err := tbl.PutItem(context.Background(), &dynamodb.PutItemInput{
		TableName: aws.String(tbl.Name()), Item: item("u1", "a"),
	}); err != nil {
		t.Fatalf("second put should succeed, got %v", err)
	}
	if got := tbl.Calls("PutItem"); got != 2 {
		t.Fatalf("Calls(PutItem) = %d, want 2", got)
	}
}

func TestFailNextNilLetsTheCallThrough(t *testing.T) {
	tbl := newTable(t, Config{})
	boom := errors.New("boom")
	tbl.FailNext("PutItem", nil) // first call succeeds
	tbl.FailNext("PutItem", boom)

	if _, err := tbl.PutItem(context.Background(), &dynamodb.PutItemInput{
		TableName: aws.String(tbl.Name()), Item: item("u1", "a"),
	}); err != nil {
		t.Fatalf("first put should succeed, got %v", err)
	}
	if tbl.Len() != 1 {
		t.Fatal("a nil-queued call must still apply its write")
	}
	if _, err := tbl.PutItem(context.Background(), &dynamodb.PutItemInput{
		TableName: aws.String(tbl.Name()), Item: item("u1", "b"),
	}); !errors.Is(err, boom) {
		t.Fatalf("second put error = %v, want boom", err)
	}
}

func assertSKs(t *testing.T, out *dynamodb.QueryOutput, want []string) {
	t.Helper()
	got := sks(out)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("items = %v, want %v", got, want)
	}
	if int(out.Count) != len(got) {
		t.Fatalf("Count = %d, want %d", out.Count, len(got))
	}
}

// TestPutRejectsEmptyIndexKey is the guard for a bug class this fake used to
// hide: a struct tag missing `omitempty` marshals an unset key attribute to an
// empty string, real DynamoDB rejects that, and the fake used to accept it —
// even returning the item from index queries, since it only checked presence.
// The result was a green test suite and a ValidationException on every write.
func TestPutRejectsEmptyIndexKey(t *testing.T) {
	tbl := New(Config{
		Name: "t",
		Key:  Key{Hash: "PK", Range: "SK"},
		GSIs: map[string]Key{"GSI1": {Hash: "GSI1PK", Range: "GSI1SK"}},
	})

	_, err := tbl.PutItem(context.Background(), &dynamodb.PutItemInput{
		TableName: aws.String("t"),
		Item: map[string]types.AttributeValue{
			"PK":     &types.AttributeValueMemberS{Value: "USER#1"},
			"SK":     &types.AttributeValueMemberS{Value: "ENTRY#1"},
			"GSI1PK": &types.AttributeValueMemberS{Value: ""},
			"GSI1SK": &types.AttributeValueMemberS{Value: ""},
		},
	})
	if err == nil {
		t.Fatal("PutItem accepted an empty string in an index key attribute; DynamoDB would reject it")
	}
	if !strings.Contains(err.Error(), "GSI1PK") {
		t.Errorf("error should name the offending attribute, got: %v", err)
	}

	// Omitting the attributes entirely is the correct way to build a sparse
	// index, and must still work.
	if _, err := tbl.PutItem(context.Background(), &dynamodb.PutItemInput{
		TableName: aws.String("t"),
		Item: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "USER#1"},
			"SK": &types.AttributeValueMemberS{Value: "ENTRY#2"},
		},
	}); err != nil {
		t.Fatalf("PutItem rejected an item that is legitimately absent from the index: %v", err)
	}
}
