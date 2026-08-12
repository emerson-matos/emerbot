package dynamotest

import (
	"fmt"
	"math/big"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// This file implements the slice of DynamoDB's expression language that this
// repo actually writes: comparisons, BETWEEN, IN, the four attribute functions,
// and AND/OR/NOT/parentheses over them.
//
// The important property is that anything outside that slice is a parse or
// evaluation *error*, never a silently-true condition. A fake that ignored an
// expression it did not understand would let a store issue a nonsense query and
// still see a green test; here the test fails instead.

// --- lexer ---

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokIdent
	tokNameRef  // #placeholder
	tokValueRef // :placeholder
	tokOp       // = <> < <= > >=
	tokLParen
	tokRParen
	tokComma
)

type token struct {
	kind tokenKind
	text string
}

func lex(s string) ([]token, error) {
	var toks []token
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(':
			toks = append(toks, token{tokLParen, "("})
			i++
		case c == ')':
			toks = append(toks, token{tokRParen, ")"})
			i++
		case c == ',':
			toks = append(toks, token{tokComma, ","})
			i++
		case c == '=':
			toks = append(toks, token{tokOp, "="})
			i++
		case c == '<':
			if i+1 < len(s) && (s[i+1] == '=' || s[i+1] == '>') {
				toks = append(toks, token{tokOp, s[i : i+2]})
				i += 2
			} else {
				toks = append(toks, token{tokOp, "<"})
				i++
			}
		case c == '>':
			if i+1 < len(s) && s[i+1] == '=' {
				toks = append(toks, token{tokOp, ">="})
				i += 2
			} else {
				toks = append(toks, token{tokOp, ">"})
				i++
			}
		case c == '#' || c == ':' || isIdentStart(c):
			start := i
			kind := tokIdent
			switch c {
			case '#':
				kind = tokNameRef
				i++
			case ':':
				kind = tokValueRef
				i++
			}
			for i < len(s) && isIdentPart(s[i]) {
				i++
			}
			if i == start+1 && kind != tokIdent {
				return nil, fmt.Errorf("dynamotest: empty placeholder at offset %d in %q", start, s)
			}
			toks = append(toks, token{kind, s[start:i]})
		default:
			return nil, fmt.Errorf("dynamotest: unexpected character %q in expression %q", string(c), s)
		}
	}
	return append(toks, token{tokEOF, ""}), nil
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

// --- AST ---

type node interface {
	eval(env) (bool, error)
}

// env carries everything an expression needs to resolve against one item.
type env struct {
	item   map[string]types.AttributeValue
	names  map[string]string
	values map[string]types.AttributeValue
}

type andNode struct{ left, right node }

func (n andNode) eval(e env) (bool, error) {
	l, err := n.left.eval(e)
	if err != nil || !l {
		return false, err
	}
	return n.right.eval(e)
}

type orNode struct{ left, right node }

func (n orNode) eval(e env) (bool, error) {
	l, err := n.left.eval(e)
	if err != nil {
		return false, err
	}
	if l {
		return true, nil
	}
	return n.right.eval(e)
}

type notNode struct{ inner node }

func (n notNode) eval(e env) (bool, error) {
	v, err := n.inner.eval(e)
	return !v, err
}

// operand is either an attribute path or a value placeholder.
type operand struct {
	path     string // set when this operand names an attribute
	valueRef string // set when this operand is a :placeholder
}

// resolve returns the operand's value and whether it is present. A missing
// attribute is absent (not an error); a missing :placeholder is a bug in the
// caller and is reported as one.
func (o operand) resolve(e env) (types.AttributeValue, bool, error) {
	if o.valueRef != "" {
		v, ok := e.values[o.valueRef]
		if !ok {
			return nil, false, fmt.Errorf("dynamotest: expression value %s is not defined in ExpressionAttributeValues", o.valueRef)
		}
		return v, true, nil
	}
	name := o.path
	if strings.HasPrefix(name, "#") {
		resolved, ok := e.names[name]
		if !ok {
			return nil, false, fmt.Errorf("dynamotest: expression name %s is not defined in ExpressionAttributeNames", name)
		}
		name = resolved
	}
	v, ok := e.item[name]
	return v, ok, nil
}

type compareNode struct {
	left, right operand
	op          string
}

func (n compareNode) eval(e env) (bool, error) {
	l, lok, err := n.left.resolve(e)
	if err != nil {
		return false, err
	}
	r, rok, err := n.right.resolve(e)
	if err != nil {
		return false, err
	}
	if !lok || !rok {
		return false, nil // a missing attribute never satisfies a comparison
	}
	if n.op == "=" {
		return avEqual(l, r), nil
	}
	if n.op == "<>" {
		return !avEqual(l, r), nil
	}
	cmp, ok := avCompare(l, r)
	if !ok {
		return false, nil // incomparable types simply do not match
	}
	switch n.op {
	case "<":
		return cmp < 0, nil
	case "<=":
		return cmp <= 0, nil
	case ">":
		return cmp > 0, nil
	case ">=":
		return cmp >= 0, nil
	}
	return false, fmt.Errorf("dynamotest: unsupported comparison operator %q", n.op)
}

type betweenNode struct{ val, lo, hi operand }

func (n betweenNode) eval(e env) (bool, error) {
	v, ok, err := n.val.resolve(e)
	if err != nil || !ok {
		return false, err
	}
	lo, lok, err := n.lo.resolve(e)
	if err != nil {
		return false, err
	}
	hi, hok, err := n.hi.resolve(e)
	if err != nil {
		return false, err
	}
	if !lok || !hok {
		return false, nil
	}
	loCmp, ok1 := avCompare(v, lo)
	hiCmp, ok2 := avCompare(v, hi)
	if !ok1 || !ok2 {
		return false, nil
	}
	return loCmp >= 0 && hiCmp <= 0, nil
}

type inNode struct {
	val  operand
	list []operand
}

func (n inNode) eval(e env) (bool, error) {
	v, ok, err := n.val.resolve(e)
	if err != nil || !ok {
		return false, err
	}
	for _, cand := range n.list {
		c, cok, err := cand.resolve(e)
		if err != nil {
			return false, err
		}
		if cok && avEqual(v, c) {
			return true, nil
		}
	}
	return false, nil
}

type funcNode struct {
	name string
	args []operand
}

func (n funcNode) eval(e env) (bool, error) {
	switch n.name {
	case "attribute_exists", "attribute_not_exists":
		if len(n.args) != 1 {
			return false, fmt.Errorf("dynamotest: %s takes 1 argument, got %d", n.name, len(n.args))
		}
		_, ok, err := n.args[0].resolve(e)
		if err != nil {
			return false, err
		}
		return ok == (n.name == "attribute_exists"), nil

	case "begins_with", "contains":
		if len(n.args) != 2 {
			return false, fmt.Errorf("dynamotest: %s takes 2 arguments, got %d", n.name, len(n.args))
		}
		subject, ok, err := n.args[0].resolve(e)
		if err != nil || !ok {
			return false, err
		}
		operandVal, ok, err := n.args[1].resolve(e)
		if err != nil || !ok {
			return false, err
		}
		if n.name == "begins_with" {
			s, ok1 := avString(subject)
			p, ok2 := avString(operandVal)
			return ok1 && ok2 && strings.HasPrefix(s, p), nil
		}
		return avContains(subject, operandVal), nil
	}
	return false, fmt.Errorf("dynamotest: unsupported function %q — extend dynamotest if a store legitimately needs it", n.name)
}

// --- parser ---

type parser struct {
	toks []token
	pos  int
	src  string
}

// parseExpression parses a complete condition, key-condition or filter
// expression. Trailing input is an error rather than something to ignore.
func parseExpression(src string) (node, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, src: src}
	n, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokEOF {
		return nil, fmt.Errorf("dynamotest: trailing input %q in expression %q", p.peek().text, src)
	}
	return n, nil
}

func (p *parser) peek() token { return p.toks[p.pos] }

func (p *parser) next() token {
	t := p.toks[p.pos]
	if t.kind != tokEOF {
		p.pos++
	}
	return t
}

// atKeyword reports whether the cursor sits on the given bare keyword, which
// DynamoDB matches case-insensitively.
func (p *parser) atKeyword(kw string) bool {
	t := p.peek()
	return t.kind == tokIdent && strings.EqualFold(t.text, kw)
}

func (p *parser) parseOr() (node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.atKeyword("OR") {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = orNode{left, right}
	}
	return left, nil
}

func (p *parser) parseAnd() (node, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.atKeyword("AND") {
		p.next()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = andNode{left, right}
	}
	return left, nil
}

func (p *parser) parseNot() (node, error) {
	if p.atKeyword("NOT") {
		p.next()
		inner, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return notNode{inner}, nil
	}
	return p.parsePrimary()
}

var knownFuncs = map[string]bool{
	"attribute_exists": true, "attribute_not_exists": true,
	"begins_with": true, "contains": true,
}

func (p *parser) parsePrimary() (node, error) {
	if p.peek().kind == tokLParen {
		p.next()
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tokRParen {
			return nil, fmt.Errorf("dynamotest: missing ) in expression %q", p.src)
		}
		p.next()
		return inner, nil
	}

	// A function call is an identifier immediately followed by "(".
	if p.peek().kind == tokIdent && p.toks[p.pos+1].kind == tokLParen {
		name := strings.ToLower(p.next().text)
		if !knownFuncs[name] {
			return nil, fmt.Errorf("dynamotest: unsupported function %q in expression %q — extend dynamotest if a store legitimately needs it", name, p.src)
		}
		p.next() // consume (
		var args []operand
		for {
			arg, err := p.parseOperand()
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
			if p.peek().kind == tokComma {
				p.next()
				continue
			}
			break
		}
		if p.peek().kind != tokRParen {
			return nil, fmt.Errorf("dynamotest: missing ) after %s(...) in expression %q", name, p.src)
		}
		p.next()
		return funcNode{name: name, args: args}, nil
	}

	left, err := p.parseOperand()
	if err != nil {
		return nil, err
	}

	switch {
	case p.peek().kind == tokOp:
		op := p.next().text
		right, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		return compareNode{left: left, right: right, op: op}, nil

	case p.atKeyword("BETWEEN"):
		p.next()
		lo, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		if !p.atKeyword("AND") {
			return nil, fmt.Errorf("dynamotest: BETWEEN without AND in expression %q", p.src)
		}
		p.next()
		hi, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		return betweenNode{val: left, lo: lo, hi: hi}, nil

	case p.atKeyword("IN"):
		p.next()
		if p.peek().kind != tokLParen {
			return nil, fmt.Errorf("dynamotest: IN without ( in expression %q", p.src)
		}
		p.next()
		var list []operand
		for {
			item, err := p.parseOperand()
			if err != nil {
				return nil, err
			}
			list = append(list, item)
			if p.peek().kind == tokComma {
				p.next()
				continue
			}
			break
		}
		if p.peek().kind != tokRParen {
			return nil, fmt.Errorf("dynamotest: missing ) after IN(...) in expression %q", p.src)
		}
		p.next()
		return inNode{val: left, list: list}, nil
	}

	return nil, fmt.Errorf("dynamotest: expected an operator after %q in expression %q", operandText(left), p.src)
}

func (p *parser) parseOperand() (operand, error) {
	t := p.next()
	switch t.kind {
	case tokIdent, tokNameRef:
		return operand{path: t.text}, nil
	case tokValueRef:
		return operand{valueRef: t.text}, nil
	}
	return operand{}, fmt.Errorf("dynamotest: expected a name or :value, got %q in expression %q", t.text, p.src)
}

func operandText(o operand) string {
	if o.valueRef != "" {
		return o.valueRef
	}
	return o.path
}

// --- attribute value comparison ---

func avString(v types.AttributeValue) (string, bool) {
	s, ok := v.(*types.AttributeValueMemberS)
	if !ok {
		return "", false
	}
	return s.Value, true
}

func avEqual(a, b types.AttributeValue) bool {
	switch av := a.(type) {
	case *types.AttributeValueMemberS:
		bv, ok := b.(*types.AttributeValueMemberS)
		return ok && av.Value == bv.Value
	case *types.AttributeValueMemberN:
		bv, ok := b.(*types.AttributeValueMemberN)
		if !ok {
			return false
		}
		cmp, ok := avCompare(av, bv)
		return ok && cmp == 0
	case *types.AttributeValueMemberBOOL:
		bv, ok := b.(*types.AttributeValueMemberBOOL)
		return ok && av.Value == bv.Value
	case *types.AttributeValueMemberB:
		bv, ok := b.(*types.AttributeValueMemberB)
		return ok && string(av.Value) == string(bv.Value)
	case *types.AttributeValueMemberNULL:
		_, ok := b.(*types.AttributeValueMemberNULL)
		return ok
	}
	return false
}

// avCompare orders two attribute values. The second result is false when the
// values are of different or unordered types, which DynamoDB treats as "does
// not match" rather than as an error.
func avCompare(a, b types.AttributeValue) (int, bool) {
	switch av := a.(type) {
	case *types.AttributeValueMemberS:
		bv, ok := b.(*types.AttributeValueMemberS)
		if !ok {
			return 0, false
		}
		return strings.Compare(av.Value, bv.Value), true
	case *types.AttributeValueMemberN:
		bv, ok := b.(*types.AttributeValueMemberN)
		if !ok {
			return 0, false
		}
		an, ok1 := parseNumber(av.Value)
		bn, ok2 := parseNumber(bv.Value)
		if !ok1 || !ok2 {
			return 0, false
		}
		return an.Cmp(bn), true
	case *types.AttributeValueMemberB:
		bv, ok := b.(*types.AttributeValueMemberB)
		if !ok {
			return 0, false
		}
		return strings.Compare(string(av.Value), string(bv.Value)), true
	}
	return 0, false
}

// parseNumber parses a DynamoDB N value. DynamoDB numbers carry up to 38
// significant digits, more than float64 holds exactly, so compare with big.Float
// to keep large centavo amounts and epoch seconds exact.
func parseNumber(s string) (*big.Float, bool) {
	f, _, err := big.ParseFloat(s, 10, 200, big.ToNearestEven)
	if err != nil {
		return nil, false
	}
	return f, true
}

// avContains implements DynamoDB's contains(): substring for strings, and
// membership for lists and string/number sets.
func avContains(subject, operandVal types.AttributeValue) bool {
	switch s := subject.(type) {
	case *types.AttributeValueMemberS:
		p, ok := avString(operandVal)
		return ok && strings.Contains(s.Value, p)
	case *types.AttributeValueMemberSS:
		p, ok := avString(operandVal)
		if !ok {
			return false
		}
		if slices.Contains(s.Value, p) {
			return true
		}
	case *types.AttributeValueMemberNS:
		n, ok := operandVal.(*types.AttributeValueMemberN)
		if !ok {
			return false
		}
		for _, v := range s.Value {
			if av, ok := avCompare(&types.AttributeValueMemberN{Value: v}, n); ok && av == 0 {
				return true
			}
		}
	case *types.AttributeValueMemberL:
		for _, v := range s.Value {
			if avEqual(v, operandVal) {
				return true
			}
		}
	}
	return false
}
