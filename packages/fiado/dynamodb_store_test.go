package fiado

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/emerson/emerbot/packages/dynamostore/dynamotest"
)

// These tests drive the DynamoDB store through its real request path against an
// in-memory table, so the key layout, the transaction and the update
// expressions are exercised as written. What they assert that the conformance
// suite cannot is the *shape on disk*: which items exist, under which sort
// keys, and what a raw query sees.

func newStore(t *testing.T, pageSize int) (*DynamoDBStore, *dynamotest.Table) {
	t.Helper()
	tbl := newFakeTable(pageSize)
	return NewDynamoDBStoreWithClient(tbl, testTable), tbl
}

// sortKeys lists every SK in the table, so a test can say exactly which items a
// write produced.
func sortKeys(tbl *dynamotest.Table) []string {
	var out []string
	for _, item := range tbl.Items() {
		if sk, ok := item["SK"].(*types.AttributeValueMemberS); ok {
			out = append(out, sk.Value)
		}
	}
	return out
}

func TestRecordWritesBothCopiesAndTheLatest(t *testing.T) {
	s, tbl := newStore(t, 0)

	m := mov(t, "01ABC", "João Silva", "2026-08-10", 4000)
	if _, err := s.Record(context.Background(), m); err != nil {
		t.Fatalf("record: %v", err)
	}

	want := []string{
		"FIADO#joao_silva",
		"FIADOCLI#joao_silva#2026-08-10#01ABC",
		"FIADODIA#2026-08-10#joao_silva#01ABC",
	}
	got := sortKeys(tbl)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("itens gravados = %v, want %v", got, want)
	}

	// One transaction, not three writes: there must be no window in which only
	// one copy of the movement exists.
	if n := tbl.Calls("TransactWriteItems"); n != 1 {
		t.Fatalf("TransactWriteItems chamado %d vezes, want 1", n)
	}
	if n := tbl.Calls("PutItem"); n != 0 {
		t.Fatalf("PutItem chamado %d vezes fora da transação, want 0", n)
	}
}

// The transaction is all-or-nothing: a failure leaves no half-written movement
// behind, which is the property that lets the balance be a cache of the copies.
func TestRecordWritesNothingWhenTheTransactionFails(t *testing.T) {
	s, tbl := newStore(t, 0)
	tbl.FailNext("TransactWriteItems", errors.New("boom"))

	if _, err := s.Record(context.Background(), mov(t, "01ABC", "João", "2026-08-10", 4000)); err == nil {
		t.Fatal("record devolveu sucesso com a transação falhando")
	}
	if n := tbl.Len(); n != 0 {
		t.Fatalf("tabela ficou com %d itens após a transação falhar: %v", n, sortKeys(tbl))
	}
}

// This is the test that protects the key design. The three prefixes are
// siblings, not nested: hanging the movements under the client ("FIADO#joao#…")
// would make the most frequent query — "quem me deve" — drag every client's
// whole history along, and a key condition cannot filter that out by suffix.
func TestBeginsWithFiadoReturnsOnlyTheLatests(t *testing.T) {
	s, tbl := newStore(t, 0)
	ctx := context.Background()

	for _, m := range []Movement{
		mov(t, "01A", "João Silva", "2026-08-10", 4000),
		mov(t, "01B", "João Silva", "2026-08-11", -1000),
		mov(t, "01C", "Ana", "2026-08-11", 2500),
	} {
		if _, err := s.Record(ctx, m); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	out, err := tbl.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(testTable),
		KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     &types.AttributeValueMemberS{Value: "USER#" + user},
			":prefix": &types.AttributeValueMemberS{Value: "FIADO#"},
		},
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if len(out.Items) != 2 {
		t.Fatalf(`begins_with(SK, "FIADO#") devolveu %d itens, want 2 (só os latests)`, len(out.Items))
	}
	for _, item := range out.Items {
		sk := item["SK"].(*types.AttributeValueMemberS).Value
		if strings.HasPrefix(sk, "FIADODIA#") || strings.HasPrefix(sk, "FIADOCLI#") {
			t.Fatalf("um movimento (%s) vazou na listagem do caderninho — os prefixos deixaram de ser irmãos", sk)
		}
	}
}

// The caderninho shares the partition with the ledger, and neither may see the
// other: an entry is not a debtor, and a debtor is not money.
func TestCaderninhoIgnoresItsNeighboursInThePartition(t *testing.T) {
	s, tbl := newStore(t, 0)
	ctx := context.Background()

	err := tbl.Seed(
		map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "USER#" + user},
			"SK": &types.AttributeValueMemberS{Value: "ENTRY#2026-08-10#e1"},
		},
		map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "USER#" + user},
			"SK": &types.AttributeValueMemberS{Value: "CAT#venda_balcao"},
		},
		map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "USER#" + user},
			"SK": &types.AttributeValueMemberS{Value: "NOTIFPREFS"},
		},
	)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := s.Record(ctx, mov(t, "01A", "João", "2026-08-10", 4000)); err != nil {
		t.Fatalf("record: %v", err)
	}

	debtors, err := s.ListDebtors(ctx, user)
	if err != nil {
		t.Fatalf("list debtors: %v", err)
	}
	if len(debtors) != 1 || debtors[0].Client != "joao" {
		t.Fatalf("caderninho = %+v, want só o joão", debtors)
	}
	page, err := s.DayMovements(ctx, user, day(t, "2026-08-10"), Page{})
	if err != nil {
		t.Fatalf("day movements: %v", err)
	}
	if len(page.Movements) != 1 {
		t.Fatalf("o dia devolveu %d movimentos, want 1 — um lançamento entrou no caderninho", len(page.Movements))
	}
}

// No index changes, no capacity spent: the caderninho declares no GSI attribute
// at all, so its items are absent from both indexes (ADR-027 §4).
func TestCaderninhoStaysOutOfTheIndexes(t *testing.T) {
	s, tbl := newStore(t, 0)
	ctx := context.Background()
	if _, err := s.Record(ctx, mov(t, "01A", "João", "2026-08-10", 4000)); err != nil {
		t.Fatalf("record: %v", err)
	}

	for _, idx := range []struct{ name, pk string }{
		{"GSI2-Status", "GSI2PK"},
		{"GSI1-Category", "GSI1PK"},
	} {
		out, err := tbl.Query(ctx, &dynamodb.QueryInput{
			TableName:              aws.String(testTable),
			IndexName:              aws.String(idx.name),
			KeyConditionExpression: aws.String(idx.pk + " = :pk"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pk": &types.AttributeValueMemberS{Value: "USER#" + user},
			},
		})
		if err != nil {
			t.Fatalf("query %s: %v", idx.name, err)
		}
		if len(out.Items) != 0 {
			t.Fatalf("%s devolveu %d itens do caderninho, want 0", idx.name, len(out.Items))
		}
	}
}

func TestDeleteRemovesBothCopiesAndLeavesTheLatest(t *testing.T) {
	s, tbl := newStore(t, 0)
	ctx := context.Background()

	m := mov(t, "01A", "João", "2026-08-10", 4000)
	if _, err := s.Record(ctx, m); err != nil {
		t.Fatalf("record: %v", err)
	}
	if _, err := s.Delete(ctx, user, m.Ref()); err != nil {
		t.Fatalf("delete: %v", err)
	}

	got := sortKeys(tbl)
	if len(got) != 1 || got[0] != "FIADO#joao" {
		t.Fatalf("itens após apagar = %v, want só FIADO#joao", got)
	}
	// The latest survives with a zero balance rather than disappearing: a
	// settled client is still somebody the name reconciliation must find.
	d, err := s.Debtor(ctx, user, "joao")
	if err != nil {
		t.Fatalf("debtor: %v", err)
	}
	if d.Balance != 0 || d.Since != nil {
		t.Fatalf("latest = saldo %d desde %q, want 0 e sem desde", d.Balance, sinceOf(d))
	}
}

func TestDeleteLeavesBothCopiesWhenTheTransactionFails(t *testing.T) {
	s, tbl := newStore(t, 0)
	ctx := context.Background()

	m := mov(t, "01A", "João", "2026-08-10", 4000)
	if _, err := s.Record(ctx, m); err != nil {
		t.Fatalf("record: %v", err)
	}
	tbl.FailNext("TransactWriteItems", errors.New("boom"))

	if _, err := s.Delete(ctx, user, m.Ref()); err == nil {
		t.Fatal("delete devolveu sucesso com a transação falhando")
	}
	if n := len(sortKeys(tbl)); n != 3 {
		t.Fatalf("tabela ficou com %d itens, want 3 — a transação não é atômica", n)
	}
	assertBalance(t, s, "joao", 4000)
}

// The balance moves by ADD, never by read-modify-write, so two movements that
// interleave still add up. Recording twice against the same latest without
// re-reading is the closest a single-threaded test gets to proving it.
func TestBalanceIsAnAtomicCounter(t *testing.T) {
	s, tbl := newStore(t, 0)
	ctx := context.Background()

	for _, m := range []Movement{
		mov(t, "01A", "João", "2026-08-10", 4000),
		mov(t, "01B", "João", "2026-08-10", 2500),
		mov(t, "01C", "João", "2026-08-10", -1500),
	} {
		if _, err := s.Record(ctx, m); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	assertBalance(t, s, "joao", 5000)

	// Three movements on one day for one client: the ULID is what keeps them
	// from overwriting each other.
	if n := len(sortKeys(tbl)); n != 7 {
		t.Fatalf("tabela tem %d itens, want 7 (1 latest + 3 movimentos × 2 cópias): %v", n, sortKeys(tbl))
	}
}

// A page boundary must not drop rows: the store pages through DynamoDB's own
// LastEvaluatedKey rather than assuming one Query returned everything.
func TestUnlimitedTimelineWalksEveryPage(t *testing.T) {
	s, _ := newStore(t, 2)
	ctx := context.Background()

	for _, id := range []string{"01A", "01B", "01C", "01D", "01E"} {
		if _, err := s.Record(ctx, mov(t, id, "João", "2026-08-10", 1000)); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	page, err := s.ClientMovements(ctx, user, "joao", Page{})
	if err != nil {
		t.Fatalf("client movements: %v", err)
	}
	if len(page.Movements) != 5 {
		t.Fatalf("extrato completo devolveu %d movimentos, want 5 — uma soma parcial é um número errado sem aviso", len(page.Movements))
	}
	assertBalance(t, s, "joao", 5000)
}
