package dynamotest

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// The fake's value is that it refuses what it does not understand. If an
// unsupported expression silently evaluated to true, every store using this
// package would get a green test for a query real DynamoDB would reject — so
// the rejections are tested as carefully as the matches.

func evalExpr(t *testing.T, expr string, item map[string]types.AttributeValue, names map[string]string, values map[string]types.AttributeValue) (bool, error) {
	t.Helper()
	n, err := parseExpression(expr)
	if err != nil {
		return false, err
	}
	return n.eval(env{item: item, names: names, values: values})
}

func TestExpressionRejectsWhatItCannotModel(t *testing.T) {
	cases := map[string]string{
		"unknown function":    "size(SK) > :n",
		"unbalanced paren":    "(PK = :pk",
		"missing operator":    "PK :pk",
		"BETWEEN without AND": "SK BETWEEN :lo :hi",
		"IN without paren":    "SK IN :a",
		"trailing garbage":    "PK = :pk EXTRA",
		"empty value ref":     "PK = :",
		"stray character":     "PK = :pk & SK = :sk",
		"empty expression":    "",
	}

	for name, expr := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseExpression(expr); err == nil {
				t.Fatalf("parseExpression(%q) succeeded, want an explicit error", expr)
			}
		})
	}
}

func TestExpressionRejectsUndefinedPlaceholders(t *testing.T) {
	item := map[string]types.AttributeValue{"PK": s("u1")}

	// A :value or #name the caller forgot to define is a request bug, not a
	// non-match — silently returning false would hide it.
	if _, err := evalExpr(t, "PK = :missing", item, nil, nil); err == nil {
		t.Fatal("an undefined :value must be an error")
	}
	if _, err := evalExpr(t, "#alias = :pk", item, nil, map[string]types.AttributeValue{":pk": s("u1")}); err == nil {
		t.Fatal("an undefined #name must be an error")
	}
}

func TestExpressionBooleanPrecedence(t *testing.T) {
	item := map[string]types.AttributeValue{
		"A": s("1"), "B": s("2"), "C": s("3"),
	}
	values := map[string]types.AttributeValue{
		":one": s("1"), ":two": s("2"), ":three": s("3"), ":nine": s("9"),
	}

	cases := []struct {
		expr string
		want bool
	}{
		// AND binds tighter than OR, so this is (false AND false) OR true.
		// Read as OR-first it would be false, which is the bug being guarded.
		{"A = :nine AND B = :nine OR C = :three", true},
		{"C = :three OR A = :nine AND B = :nine", true},
		// Parentheses override that: (true OR false) AND false is false.
		{"(C = :three OR A = :nine) AND B = :nine", false},
		{"A = :one AND B = :two", true},
		{"A = :one AND B = :nine", false},
		{"A = :nine OR B = :two", true},
		{"A = :nine OR B = :nine", false},
		{"(A = :nine OR B = :two) AND A = :one", true},
		{"NOT A = :nine", true},
		{"NOT A = :one", false},
		{"A <> :nine", true},
		{"A <> :one", false},
	}

	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			got, err := evalExpr(t, tc.expr, item, nil, values)
			if err != nil {
				t.Fatalf("eval %q: %v", tc.expr, err)
			}
			if got != tc.want {
				t.Fatalf("%q = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

func TestExpressionComparesNumbersNumerically(t *testing.T) {
	item := map[string]types.AttributeValue{"N": n("9")}
	values := map[string]types.AttributeValue{":ten": n("10")}

	// Lexicographically "9" > "10"; numerically it is not. A store comparing
	// epoch seconds or centavos depends on getting this right.
	got, err := evalExpr(t, "N < :ten", item, nil, values)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !got {
		t.Fatal("9 < 10 evaluated false — numbers are being compared as strings")
	}
}

func TestExpressionComparesLargeNumbersExactly(t *testing.T) {
	// Beyond float64's exact integer range; two distinct values must not
	// compare equal.
	item := map[string]types.AttributeValue{"N": n("9007199254740993")}
	values := map[string]types.AttributeValue{":other": n("9007199254740992")}

	equal, err := evalExpr(t, "N = :other", item, nil, values)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if equal {
		t.Fatal("two distinct large integers compared equal — precision was lost")
	}
}

func TestExpressionMissingAttributeNeverMatches(t *testing.T) {
	item := map[string]types.AttributeValue{"PK": s("u1")}
	values := map[string]types.AttributeValue{":v": s("x")}

	for _, expr := range []string{
		"Absent = :v", "Absent < :v", "Absent BETWEEN :v AND :v",
		"begins_with(Absent, :v)", "contains(Absent, :v)", "Absent IN (:v)",
	} {
		t.Run(expr, func(t *testing.T) {
			got, err := evalExpr(t, expr, item, nil, values)
			if err != nil {
				t.Fatalf("eval %q: %v", expr, err)
			}
			if got {
				t.Fatalf("%q matched against an absent attribute", expr)
			}
		})
	}

	// attribute_not_exists is the one that must match.
	got, err := evalExpr(t, "attribute_not_exists(Absent)", item, nil, values)
	if err != nil || !got {
		t.Fatalf("attribute_not_exists = %v (err %v), want true", got, err)
	}
}

func TestExpressionMismatchedTypesDoNotMatch(t *testing.T) {
	item := map[string]types.AttributeValue{"V": s("10")}
	values := map[string]types.AttributeValue{":num": n("10")}

	// DynamoDB treats a string and a number as incomparable rather than equal.
	got, err := evalExpr(t, "V = :num", item, nil, values)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got {
		t.Fatal("a string matched a number of the same digits")
	}
}

func TestExpressionContainsAcrossTypes(t *testing.T) {
	item := map[string]types.AttributeValue{
		"Str":  s("aluguel da loja"),
		"Set":  &types.AttributeValueMemberSS{Value: []string{"a", "b"}},
		"Nums": &types.AttributeValueMemberNS{Value: []string{"1", "2"}},
		"List": &types.AttributeValueMemberL{Value: []types.AttributeValue{s("x")}},
	}
	values := map[string]types.AttributeValue{
		":sub": s("aluguel"), ":miss": s("zzz"), ":b": s("b"), ":two": n("2"), ":x": s("x"),
	}

	cases := map[string]bool{
		"contains(Str, :sub)":   true,
		"contains(Str, :miss)":  false,
		"contains(Set, :b)":     true,
		"contains(Set, :miss)":  false,
		"contains(Nums, :two)":  true,
		"contains(List, :x)":    true,
		"contains(List, :miss)": false,
	}
	for expr, want := range cases {
		t.Run(expr, func(t *testing.T) {
			got, err := evalExpr(t, expr, item, nil, values)
			if err != nil {
				t.Fatalf("eval %q: %v", expr, err)
			}
			if got != want {
				t.Fatalf("%q = %v, want %v", expr, got, want)
			}
		})
	}
}

func TestExpressionFunctionArityIsChecked(t *testing.T) {
	item := map[string]types.AttributeValue{"PK": s("u1")}
	values := map[string]types.AttributeValue{":a": s("u"), ":b": s("1")}

	for _, expr := range []string{
		"attribute_exists(PK, :a)", // one argument only
		"begins_with(PK)",          // two arguments required
		"contains(PK, :a, :b)",
	} {
		t.Run(expr, func(t *testing.T) {
			if _, err := evalExpr(t, expr, item, nil, values); err == nil {
				t.Fatalf("%q was accepted with the wrong number of arguments", expr)
			}
		})
	}
}

func TestExpressionKeywordsAreCaseInsensitive(t *testing.T) {
	item := map[string]types.AttributeValue{"A": s("1"), "B": s("2")}
	values := map[string]types.AttributeValue{":one": s("1"), ":two": s("2")}

	got, err := evalExpr(t, "A = :one and B = :two", item, nil, values)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !got {
		t.Fatal("a lowercase AND was not recognised")
	}
}

func TestExpressionIN(t *testing.T) {
	item := map[string]types.AttributeValue{"Status": s("paid")}
	values := map[string]types.AttributeValue{
		":a": s("pending"), ":b": s("paid"), ":c": s("void"),
	}

	got, err := evalExpr(t, "Status IN (:a, :b, :c)", item, nil, values)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !got {
		t.Fatal("IN did not match a listed value")
	}

	if got, _ = evalExpr(t, "Status IN (:a, :c)", item, nil, values); got {
		t.Fatal("IN matched a value that is not in the list")
	}
}

func TestUnsupportedExpressionReachesTheStoreAsAnError(t *testing.T) {
	// End to end: a store issuing an expression the fake cannot model gets an
	// error back rather than a plausible-looking empty result.
	tbl := newTable(t, Config{})
	_, err := tbl.Query(t.Context(), queryInput("PK = :pk AND size(SK) > :n"))
	if err == nil || !strings.Contains(err.Error(), "unsupported function") {
		t.Fatalf("error = %v, want an unsupported-function error", err)
	}
}

func queryInput(keyCondition string) *dynamodb.QueryInput {
	return &dynamodb.QueryInput{
		TableName:              aws.String("test-table"),
		KeyConditionExpression: aws.String(keyCondition),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk": s("u1"), ":n": n("1"),
		},
	}
}
