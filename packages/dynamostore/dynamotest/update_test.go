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

// The fake has its own tests for the same reason it errors on what it does not
// model: an update expression it silently ignored would leave a store's test
// green while the real write did something else.

func updateTable(t *testing.T) *Table {
	t.Helper()
	return New(Config{Name: "t", Key: Key{Hash: "PK", Range: "SK"}})
}

func key(pk, sk string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"PK": &types.AttributeValueMemberS{Value: pk},
		"SK": &types.AttributeValueMemberS{Value: sk},
	}
}

func str(v string) types.AttributeValue { return &types.AttributeValueMemberS{Value: v} }
func num(v string) types.AttributeValue { return &types.AttributeValueMemberN{Value: v} }
func numOf(t *testing.T, item map[string]types.AttributeValue, attr string) string {
	t.Helper()
	v, ok := item[attr]
	if !ok {
		t.Fatalf("item has no %q: %v", attr, item)
	}
	n, ok := v.(*types.AttributeValueMemberN)
	if !ok {
		t.Fatalf("%q is %T, want a number", attr, v)
	}
	return n.Value
}

// ADD on an attribute that is not there starts from the value itself, which is
// what lets a counter be created without a read.
func TestUpdateItemAddCreatesAndIncrements(t *testing.T) {
	tbl := updateTable(t)
	ctx := context.Background()

	for _, delta := range []string{"4000", "2500", "-1500"} {
		out, err := tbl.UpdateItem(ctx, &dynamodb.UpdateItemInput{
			TableName:                 aws.String("t"),
			Key:                       key("u1", "FIADO#joao"),
			UpdateExpression:          aws.String("ADD #bal :d"),
			ExpressionAttributeNames:  map[string]string{"#bal": "Balance"},
			ExpressionAttributeValues: map[string]types.AttributeValue{":d": num(delta)},
			ReturnValues:              types.ReturnValueAllNew,
		})
		if err != nil {
			t.Fatalf("update %s: %v", delta, err)
		}
		if out.Attributes == nil {
			t.Fatal("ALL_NEW returned no attributes")
		}
	}

	items := tbl.Items()
	if len(items) != 1 {
		t.Fatalf("table has %d items, want 1", len(items))
	}
	if got := numOf(t, items[0], "Balance"); got != "5000" {
		t.Fatalf("Balance = %s, want 5000", got)
	}
}

func TestUpdateItemSetAndIfNotExists(t *testing.T) {
	tbl := updateTable(t)
	ctx := context.Background()

	update := func(name, since string) {
		t.Helper()
		_, err := tbl.UpdateItem(ctx, &dynamodb.UpdateItemInput{
			TableName:        aws.String("t"),
			Key:              key("u1", "FIADO#joao"),
			UpdateExpression: aws.String("SET #name = :n, #since = if_not_exists(#since, :s) ADD #bal :d"),
			ExpressionAttributeNames: map[string]string{
				"#name": "Name", "#since": "Since", "#bal": "Balance",
			},
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":n": str(name), ":s": str(since), ":d": num("10"),
			},
		})
		if err != nil {
			t.Fatalf("update: %v", err)
		}
	}
	update("João", "2026-08-01")
	update("João Silva", "2026-08-09")

	item := tbl.Items()[0]
	// SET overwrites every time; if_not_exists writes once and then stands.
	if got := item["Name"].(*types.AttributeValueMemberS).Value; got != "João Silva" {
		t.Fatalf("Name = %q, want %q", got, "João Silva")
	}
	if got := item["Since"].(*types.AttributeValueMemberS).Value; got != "2026-08-01" {
		t.Fatalf("Since = %q, want 2026-08-01 — if_not_exists overwrote", got)
	}
	if got := numOf(t, item, "Balance"); got != "20" {
		t.Fatalf("Balance = %s, want 20", got)
	}
}

func TestUpdateItemRemove(t *testing.T) {
	tbl := updateTable(t)
	ctx := context.Background()
	if err := tbl.Seed(map[string]types.AttributeValue{
		"PK": str("u1"), "SK": str("FIADO#joao"), "Since": str("2026-08-01"), "Balance": num("0"),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := tbl.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 aws.String("t"),
		Key:                       key("u1", "FIADO#joao"),
		UpdateExpression:          aws.String("REMOVE #since"),
		ConditionExpression:       aws.String("#bal = :zero"),
		ExpressionAttributeNames:  map[string]string{"#since": "Since", "#bal": "Balance"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":zero": num("0")},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, ok := tbl.Items()[0]["Since"]; ok {
		t.Fatal("Since survived a REMOVE")
	}
}

func TestUpdateItemRespectsAFailedCondition(t *testing.T) {
	tbl := updateTable(t)
	ctx := context.Background()
	if err := tbl.Seed(map[string]types.AttributeValue{
		"PK": str("u1"), "SK": str("FIADO#joao"), "Since": str("2026-08-01"), "Balance": num("4000"),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := tbl.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 aws.String("t"),
		Key:                       key("u1", "FIADO#joao"),
		UpdateExpression:          aws.String("REMOVE #since"),
		ConditionExpression:       aws.String("#bal = :zero"),
		ExpressionAttributeNames:  map[string]string{"#since": "Since", "#bal": "Balance"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":zero": num("0")},
	})
	var condFailed *types.ConditionalCheckFailedException
	if !errors.As(err, &condFailed) {
		t.Fatalf("err = %v, want ConditionalCheckFailedException", err)
	}
	if _, ok := tbl.Items()[0]["Since"]; !ok {
		t.Fatal("Since was removed even though the condition failed")
	}
}

// An Update inside a transaction is staged like the rest: a later item's
// failure must leave the counter untouched.
func TestTransactUpdateIsAllOrNothing(t *testing.T) {
	tbl := updateTable(t)
	ctx := context.Background()

	_, err := tbl.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{TransactItems: []types.TransactWriteItem{
		{Update: &types.Update{
			TableName:                 aws.String("t"),
			Key:                       key("u1", "FIADO#joao"),
			UpdateExpression:          aws.String("ADD #bal :d"),
			ExpressionAttributeNames:  map[string]string{"#bal": "Balance"},
			ExpressionAttributeValues: map[string]types.AttributeValue{":d": num("4000")},
		}},
		{Delete: &types.Delete{
			TableName:           aws.String("t"),
			Key:                 key("u1", "FIADODIA#2026-08-01#joao#01A"),
			ConditionExpression: aws.String("attribute_exists(PK)"),
		}},
	}})
	if err == nil {
		t.Fatal("transaction succeeded with a failing condition")
	}
	if n := tbl.Len(); n != 0 {
		t.Fatalf("table has %d items after a cancelled transaction, want 0", n)
	}
}

// Everything the fake does not model has to be an error, never a clause that
// quietly did nothing.
func TestUpdateExpressionRejectsWhatItDoesNotModel(t *testing.T) {
	cases := map[string]struct {
		expr  string
		names map[string]string
		want  string
	}{
		"unknown clause":    {expr: "DELETE Tags :t", want: "not modelled"},
		"arithmetic":        {expr: "SET Balance = Balance + :d", want: "unexpected character"},
		"unknown function":  {expr: "SET Balance = list_append(Balance, :d)", want: "not modelled"},
		"undefined name":    {expr: "SET #nope = :v", want: "not defined in ExpressionAttributeNames"},
		"key attribute":     {expr: "SET SK = :v", want: "key attribute"},
		"repeated clause":   {expr: "SET A = :v SET B = :v", want: "appears twice"},
		"empty":             {expr: "  ", want: "does nothing"},
		"document path":     {expr: "SET a.b = :v", want: "unexpected"},
		"missing value ref": {expr: "ADD Balance Balance", want: "expects a :value"},
		"no clause at all":  {expr: ":v = :v", want: "expected SET, ADD or REMOVE"},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			_, err := parseUpdateExpression(tc.expr, tc.names)
			if err == nil {
				_, err = applyUpdate(nil, key("u1", "s"), tc.expr, tc.names,
					map[string]types.AttributeValue{":v": str("x"), ":d": num("1"), ":t": str("x")},
					Key{Hash: "PK", Range: "SK"})
			}
			if err == nil {
				t.Fatalf("expression %q was accepted", tc.expr)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// A name map shared between two updates that touch different attributes reads
// like harmless deduplication in Go and is a ValidationException on every call.
// The fake used to accept it, so the first thing to find it was a live run
// against dynamodb-local.
func TestUpdateItemRejectsUnusedExpressionAttributes(t *testing.T) {
	names := map[string]string{"#b": "Balance", "#n": "Name"}
	values := map[string]types.AttributeValue{":d": num("1"), ":x": str("unused")}

	cases := map[string]struct {
		names  map[string]string
		values map[string]types.AttributeValue
		want   string
	}{
		"unused name":  {names: names, values: map[string]types.AttributeValue{":d": num("1")}, want: "ExpressionAttributeNames #n"},
		"unused value": {names: map[string]string{"#b": "Balance"}, values: values, want: "ExpressionAttributeValues :x"},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			tbl := updateTable(t)
			_, err := tbl.UpdateItem(context.Background(), &dynamodb.UpdateItemInput{
				TableName:                 aws.String("t"),
				Key:                       key("u1", "s"),
				UpdateExpression:          aws.String("ADD #b :d"),
				ExpressionAttributeNames:  tc.names,
				ExpressionAttributeValues: tc.values,
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to name %s", err, tc.want)
			}
		})
	}
}

// The condition expression counts as a use: a name only the condition
// references is not leftover, and rejecting it would be the opposite bug.
func TestConditionExpressionCountsAsUse(t *testing.T) {
	tbl := updateTable(t)
	if _, err := tbl.UpdateItem(context.Background(), &dynamodb.UpdateItemInput{
		TableName:                 aws.String("t"),
		Key:                       key("u1", "s"),
		UpdateExpression:          aws.String("ADD #b :d"),
		ConditionExpression:       aws.String("attribute_not_exists(#n)"),
		ExpressionAttributeNames:  map[string]string{"#b": "Balance", "#n": "Name"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":d": num("1")},
	}); err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}
}

func TestUpdateItemRejectsAnUnmodelledReturnValue(t *testing.T) {
	tbl := updateTable(t)
	_, err := tbl.UpdateItem(context.Background(), &dynamodb.UpdateItemInput{
		TableName:                 aws.String("t"),
		Key:                       key("u1", "s"),
		UpdateExpression:          aws.String("ADD #b :d"),
		ExpressionAttributeNames:  map[string]string{"#b": "Balance"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":d": num("1")},
		ReturnValues:              types.ReturnValueUpdatedNew,
	})
	if err == nil || !strings.Contains(err.Error(), "not modelled") {
		t.Fatalf("err = %v, want an unmodelled-ReturnValues error", err)
	}
}

// Centavos are integers, but the arithmetic must not round anything it is
// handed either — a float sum would quietly lose a balance's last digits.
func TestAddNumbersIsExact(t *testing.T) {
	cases := map[string][3]string{
		"integers":        {"4000", "-1500", "2500"},
		"from nothing":    {"0", "-4000", "-4000"},
		"decimals":        {"0.1", "0.2", "0.3"},
		"big":             {"99999999999999999999", "1", "100000000000000000000"},
		"trailing zeroes": {"1.50", "0.50", "2"},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			got, err := addNumbers(tc[0], tc[1])
			if err != nil {
				t.Fatalf("addNumbers: %v", err)
			}
			if got != tc[2] {
				t.Fatalf("%s + %s = %s, want %s", tc[0], tc[1], got, tc[2])
			}
		})
	}
}
