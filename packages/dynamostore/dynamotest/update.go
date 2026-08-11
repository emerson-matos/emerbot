package dynamotest

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// This file implements the slice of DynamoDB's UpdateExpression language this
// repo writes: SET (a plain value, or if_not_exists so a first write seeds an
// attribute), ADD on a number, and REMOVE.
//
// It follows the same rule as expr.go: anything outside that slice is an
// error, never a silently-skipped clause. An update the fake did not
// understand would otherwise leave the item unchanged and let a store's test
// pass while the same request wrote something else — or nothing — in
// production.

// errConditionFailed is the internal signal that an Update's ConditionExpression
// evaluated false. Callers turn it into whatever the operation reports:
// ConditionalCheckFailedException for UpdateItem, a cancellation reason inside
// TransactWriteItems.
var errConditionFailed = errors.New("dynamotest: condition failed")

// updatePlan is a parsed UpdateExpression, with every #placeholder already
// resolved to a real attribute name.
type updatePlan struct {
	sets    []setAction
	adds    []addAction
	removes []string
}

type setAction struct {
	attr string
	// valueRef is the :placeholder holding the new value.
	valueRef string
	// ifNotExists, when set, names the attribute whose absence gates the write.
	// DynamoDB allows if_not_exists(other, :v); this models the general form
	// even though every caller here guards the attribute it is setting.
	ifNotExists string
	guarded     bool
}

type addAction struct {
	attr     string
	valueRef string
}

// applyUpdate returns the item that results from applying expr to the stored
// item, which is nil when the key does not exist yet — an update creates the
// item, carrying only its key plus whatever the expression writes, exactly as
// DynamoDB does.
func applyUpdate(
	stored, key map[string]types.AttributeValue,
	expr string,
	names map[string]string,
	values map[string]types.AttributeValue,
	primary Key,
) (map[string]types.AttributeValue, error) {
	plan, err := parseUpdateExpression(expr, names)
	if err != nil {
		return nil, err
	}
	if err := plan.checkKeyAttrs(primary); err != nil {
		return nil, err
	}

	item := cloneItem(stored)
	if item == nil {
		item = cloneItem(key)
	}

	for _, s := range plan.sets {
		if s.guarded {
			if _, exists := item[s.ifNotExists]; exists {
				continue
			}
		}
		v, ok := values[s.valueRef]
		if !ok {
			return nil, fmt.Errorf("dynamotest: update value %s is not defined in ExpressionAttributeValues", s.valueRef)
		}
		item[s.attr] = v
	}

	for _, a := range plan.adds {
		v, ok := values[a.valueRef]
		if !ok {
			return nil, fmt.Errorf("dynamotest: update value %s is not defined in ExpressionAttributeValues", a.valueRef)
		}
		delta, ok := v.(*types.AttributeValueMemberN)
		if !ok {
			return nil, fmt.Errorf("dynamotest: ADD %s %s expects a number; sets are not modelled", a.attr, a.valueRef)
		}
		// An ADD on a missing attribute starts from the delta itself, which is
		// what lets the first movement create a debtor's balance without a read.
		current := "0"
		if existing, ok := item[a.attr]; ok {
			n, ok := existing.(*types.AttributeValueMemberN)
			if !ok {
				return nil, fmt.Errorf("dynamotest: ADD %s applies to a number, but the stored attribute is %T", a.attr, existing)
			}
			current = n.Value
		}
		sum, err := addNumbers(current, delta.Value)
		if err != nil {
			return nil, err
		}
		item[a.attr] = &types.AttributeValueMemberN{Value: sum}
	}

	for _, attr := range plan.removes {
		delete(item, attr)
	}

	return item, nil
}

// checkKeyAttrs rejects an update that writes a primary key attribute, which
// real DynamoDB refuses: the key addresses the item, it is not part of what an
// update may change.
func (p updatePlan) checkKeyAttrs(primary Key) error {
	touched := make([]string, 0, len(p.sets)+len(p.adds)+len(p.removes))
	for _, s := range p.sets {
		touched = append(touched, s.attr)
	}
	for _, a := range p.adds {
		touched = append(touched, a.attr)
	}
	touched = append(touched, p.removes...)

	for _, attr := range touched {
		if attr == primary.Hash || (primary.Range != "" && attr == primary.Range) {
			return fmt.Errorf("dynamotest: update writes key attribute %q; DynamoDB rejects that", attr)
		}
	}
	return nil
}

// addNumbers sums two DynamoDB N values exactly. Centavos are integers today,
// but a float sum would quietly round a balance, so the arithmetic goes through
// big.Rat and the result keeps the widest scale of its operands.
func addNumbers(a, b string) (string, error) {
	ra, ok := new(big.Rat).SetString(a)
	if !ok {
		return "", fmt.Errorf("dynamotest: %q is not a number", a)
	}
	rb, ok := new(big.Rat).SetString(b)
	if !ok {
		return "", fmt.Errorf("dynamotest: %q is not a number", b)
	}
	scale := max(decimalPlaces(a), decimalPlaces(b))
	out := new(big.Rat).Add(ra, rb).FloatString(scale)
	if scale > 0 {
		out = strings.TrimRight(out, "0")
		out = strings.TrimSuffix(out, ".")
	}
	return out, nil
}

// decimalPlaces counts the digits after the decimal point. Exponent notation is
// rejected rather than guessed at — nothing in this repo writes it, and reading
// it wrong would round somebody's money.
func decimalPlaces(s string) int {
	if strings.ContainsAny(s, "eE") {
		return 0
	}
	if i := strings.IndexByte(s, '.'); i >= 0 {
		return len(s) - i - 1
	}
	return 0
}

// --- parser ---

type updateParser struct {
	toks  []token
	pos   int
	src   string
	names map[string]string
}

func parseUpdateExpression(src string, names map[string]string) (updatePlan, error) {
	toks, err := lex(src)
	if err != nil {
		return updatePlan{}, err
	}
	p := &updateParser{toks: toks, src: src, names: names}

	var plan updatePlan
	seen := map[string]bool{}
	for p.peek().kind != tokEOF {
		clause := p.peek()
		if clause.kind != tokIdent {
			return updatePlan{}, fmt.Errorf("dynamotest: expected SET, ADD or REMOVE, got %q in update expression %q", clause.text, src)
		}
		kw := strings.ToUpper(clause.text)
		// DynamoDB allows each clause keyword at most once per expression.
		if seen[kw] {
			return updatePlan{}, fmt.Errorf("dynamotest: %s appears twice in update expression %q", kw, src)
		}
		seen[kw] = true
		p.next()

		switch kw {
		case "SET":
			actions, err := p.parseSets()
			if err != nil {
				return updatePlan{}, err
			}
			plan.sets = actions
		case "ADD":
			actions, err := p.parseAdds()
			if err != nil {
				return updatePlan{}, err
			}
			plan.adds = actions
		case "REMOVE":
			attrs, err := p.parseRemoves()
			if err != nil {
				return updatePlan{}, err
			}
			plan.removes = attrs
		default:
			return updatePlan{}, fmt.Errorf(
				"dynamotest: update clause %q is not modelled (only SET, ADD and REMOVE are) in %q — extend dynamotest if a store legitimately needs it",
				clause.text, src,
			)
		}
	}
	if len(plan.sets)+len(plan.adds)+len(plan.removes) == 0 {
		return updatePlan{}, fmt.Errorf("dynamotest: update expression %q does nothing", src)
	}
	return plan, nil
}

func (p *updateParser) peek() token { return p.toks[p.pos] }

func (p *updateParser) next() token {
	t := p.toks[p.pos]
	if t.kind != tokEOF {
		p.pos++
	}
	return t
}

// atClauseKeyword reports whether the cursor sits on the start of another
// clause, which is what ends the current one when there is no comma.
func (p *updateParser) atClauseKeyword() bool {
	t := p.peek()
	if t.kind != tokIdent {
		return false
	}
	switch strings.ToUpper(t.text) {
	case "SET", "ADD", "REMOVE", "DELETE":
		return true
	}
	return false
}

func (p *updateParser) parseSets() ([]setAction, error) {
	var out []setAction
	for {
		attr, err := p.parseAttr()
		if err != nil {
			return nil, err
		}
		if t := p.next(); t.kind != tokOp || t.text != "=" {
			return nil, fmt.Errorf("dynamotest: expected = after %s in update expression %q", attr, p.src)
		}

		action := setAction{attr: attr}
		switch t := p.peek(); {
		case t.kind == tokValueRef:
			action.valueRef = p.next().text
		case t.kind == tokIdent && strings.EqualFold(t.text, "if_not_exists"):
			p.next()
			if p.next().kind != tokLParen {
				return nil, fmt.Errorf("dynamotest: if_not_exists without ( in update expression %q", p.src)
			}
			guard, err := p.parseAttr()
			if err != nil {
				return nil, err
			}
			if p.next().kind != tokComma {
				return nil, fmt.Errorf("dynamotest: if_not_exists takes 2 arguments in update expression %q", p.src)
			}
			v := p.next()
			if v.kind != tokValueRef {
				return nil, fmt.Errorf("dynamotest: if_not_exists's second argument must be a :value in update expression %q", p.src)
			}
			if p.next().kind != tokRParen {
				return nil, fmt.Errorf("dynamotest: missing ) after if_not_exists(...) in update expression %q", p.src)
			}
			action.ifNotExists, action.guarded, action.valueRef = guard, true, v.text
		default:
			return nil, fmt.Errorf(
				"dynamotest: SET %s takes a :value or if_not_exists(...) — arithmetic and functions beyond that are not modelled — in update expression %q",
				attr, p.src,
			)
		}

		out = append(out, action)
		if p.peek().kind == tokComma {
			p.next()
			continue
		}
		if p.peek().kind == tokEOF || p.atClauseKeyword() {
			return out, nil
		}
		return nil, fmt.Errorf("dynamotest: unexpected %q after a SET action in update expression %q", p.peek().text, p.src)
	}
}

func (p *updateParser) parseAdds() ([]addAction, error) {
	var out []addAction
	for {
		attr, err := p.parseAttr()
		if err != nil {
			return nil, err
		}
		v := p.next()
		if v.kind != tokValueRef {
			return nil, fmt.Errorf("dynamotest: ADD %s expects a :value, got %q in update expression %q", attr, v.text, p.src)
		}
		out = append(out, addAction{attr: attr, valueRef: v.text})

		if p.peek().kind == tokComma {
			p.next()
			continue
		}
		if p.peek().kind == tokEOF || p.atClauseKeyword() {
			return out, nil
		}
		return nil, fmt.Errorf("dynamotest: unexpected %q after an ADD action in update expression %q", p.peek().text, p.src)
	}
}

func (p *updateParser) parseRemoves() ([]string, error) {
	var out []string
	for {
		attr, err := p.parseAttr()
		if err != nil {
			return nil, err
		}
		out = append(out, attr)

		if p.peek().kind == tokComma {
			p.next()
			continue
		}
		if p.peek().kind == tokEOF || p.atClauseKeyword() {
			return out, nil
		}
		return nil, fmt.Errorf("dynamotest: unexpected %q after a REMOVE action in update expression %q", p.peek().text, p.src)
	}
}

// parseAttr reads one top-level attribute name, resolving a #placeholder.
// Document paths (a.b, a[0]) are rejected rather than flattened: nothing here
// writes them, and pretending to support them would silently write the wrong
// attribute.
func (p *updateParser) parseAttr() (string, error) {
	t := p.next()
	switch t.kind {
	case tokIdent:
		return t.text, nil
	case tokNameRef:
		name, ok := p.names[t.text]
		if !ok {
			return "", fmt.Errorf("dynamotest: expression name %s is not defined in ExpressionAttributeNames", t.text)
		}
		return name, nil
	}
	return "", fmt.Errorf("dynamotest: expected an attribute name, got %q in update expression %q", t.text, p.src)
}
