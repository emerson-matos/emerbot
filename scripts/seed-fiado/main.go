// Command seed-fiado fills the caderninho in dynamodb-local so the dashboard
// has something to render. It goes through packages/fiado's DynamoDB store on
// purpose: this is the only path that exercises the ADD, the if_not_exists and
// the three-item TransactWriteItems against a real DynamoDB rather than the
// in-memory fake (ADR-027).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/dynamostore"
	"github.com/emerson/emerbot/packages/fiado"
	"github.com/emerson/emerbot/packages/shared"
)

// wipe removes every caderninho item in the partition — the three FIADO
// prefixes and nothing else, so re-seeding leaves the financial ledger sharing
// the table untouched. Without it a second run doubles every balance and the
// "desde" scenarios stop meaning anything.
func wipe(ctx context.Context, client *dynamodb.Client, table, userID string) (int, error) {
	deleted := 0
	for _, prefix := range []string{"FIADO#", "FIADODIA#", "FIADOCLI#"} {
		var start map[string]types.AttributeValue
		for {
			out, err := client.Query(ctx, &dynamodb.QueryInput{
				TableName:              aws.String(table),
				KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :sk)"),
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":pk": &types.AttributeValueMemberS{Value: "USER#" + userID},
					":sk": &types.AttributeValueMemberS{Value: prefix},
				},
				ExclusiveStartKey: start,
			})
			if err != nil {
				return deleted, err
			}
			for _, item := range out.Items {
				if _, err := client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
					TableName: aws.String(table),
					Key:       map[string]types.AttributeValue{"PK": item["PK"], "SK": item["SK"]},
				}); err != nil {
					return deleted, err
				}
				deleted++
			}
			if start = out.LastEvaluatedKey; len(start) == 0 {
				break
			}
		}
	}
	return deleted, nil
}

func main() {
	endpoint := flag.String("endpoint", shared.Getenv("DYNAMODB_ENDPOINT", "http://localhost:8000"), "DynamoDB endpoint")
	table := flag.String("table", shared.Getenv("FINANCIAL_ENTRIES_TABLE", "emerbot-local-financial-entries"), "table name")
	userID := flag.String("user-id", shared.FinanceLedgerID, "partition to seed")
	wipeOnly := flag.Bool("wipe-only", false, "empty the caderninho and stop, without seeding")
	flag.Parse()

	ctx := context.Background()
	client, err := dynamostore.NewClient(ctx, *endpoint)
	if err != nil {
		log.Fatalf("client: %v", err)
	}
	store := fiado.NewDynamoDBStoreWithClient(client, *table)

	n, err := wipe(ctx, client, *table, *userID)
	if err != nil {
		log.Fatalf("limpar caderninho: %v", err)
	}
	if n > 0 {
		fmt.Printf("apagados %d itens de fiado anteriores\n\n", n)
	}
	if *wipeOnly {
		return
	}

	today := time.Now()
	day := func(daysAgo int) domain.CalendarDate {
		return domain.NewCalendarDate(today.AddDate(0, 0, -daysAgo))
	}

	// Each line is one movement: positive is a debt, negative is a payment.
	movements := []struct {
		name   string
		amount int64
		date   domain.CalendarDate
		desc   string
	}{
		// João: still owes. Two purchases and a partial payment -> 20,00.
		{"João Silva", 4000, day(12), "remédio pressão"},
		{"João Silva", 3000, day(5), "fralda geriátrica"},
		{"João Silva", -5000, day(2), ""},

		// Maria: paid everything off, then bought again. The "desde" has to be
		// the new purchase, not the old debt.
		{"Maria Souza", 2500, day(20), "dipirona + soro"},
		{"Maria Souza", -2500, day(6), ""},
		{"Maria Souza", 1500, day(1), "vitamina C"},

		// Pedro: paid more than he owed. Negative balance is credit, and
		// credit does not age.
		{"Pedro Lima", 10000, day(15), "antibiótico"},
		{"Pedro Lima", -12000, day(4), ""},

		// Ana: bought today. Nothing to age yet.
		{"Ana Paula", 6000, day(0), "protetor solar"},

		// Two movements on the same day for the same client: the ULID is what
		// keeps them from overwriting each other.
		{"Carlos Mendes", 1800, day(3), "álcool em gel"},
		{"Carlos Mendes", 2200, day(3), "termômetro"},
	}

	for _, mv := range movements {
		m, err := fiado.NewMovement(*userID, mv.name, mv.amount, mv.date, mv.desc)
		if err != nil {
			log.Fatalf("build %s: %v", mv.name, err)
		}
		d, err := store.Record(ctx, m)
		if err != nil {
			log.Fatalf("record %s: %v", mv.name, err)
		}
		fmt.Printf("%-14s %+8d  -> saldo %8d  desde %v\n", d.Name, mv.amount, d.Balance, since(d))
	}

	debtors, err := store.ListDebtors(ctx, *userID)
	if err != nil {
		log.Fatalf("list: %v", err)
	}
	fmt.Println("\ncaderninho:")
	for _, d := range debtors {
		fmt.Printf("  %-14s saldo %8d  desde %-12v dias %v\n", d.Name, d.Balance, since(d), days(d, today))
	}
}

func days(d fiado.Debtor, today time.Time) any {
	n := fiado.DaysOpen(d, domain.NewCalendarDate(today))
	if n == nil {
		return "-"
	}
	return *n
}

func since(d fiado.Debtor) any {
	if d.Since == nil {
		return "-"
	}
	return d.Since.String()
}
