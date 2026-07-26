// Package dynamotest provides an in-memory DynamoDB table that implements
// dynamostore.API, so stores can be tested through their real read and write
// paths instead of against hand-rolled call recorders.
//
// It models the parts of DynamoDB that this repo's correctness actually
// depends on: composite primary keys, sparse global secondary indexes, sort
// order and ScanIndexForward, Limit-before-filter, paginated responses with
// LastEvaluatedKey, conditional writes, BatchWriteItem's UnprocessedItems, and
// the all-or-nothing semantics of TransactWriteItems. Everything it does not
// model — an unknown expression function, a key condition that touches a
// non-key attribute, a write missing its key — is reported as an error, so a
// store issuing a request that real DynamoDB would reject fails the test here
// too.
package dynamotest

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/emerson/emerbot/packages/dynamostore"
)

// Key names the hash and (optionally) range attribute of a table or index.
type Key struct {
	Hash  string
	Range string // empty for a hash-only key schema
}

// Config describes the table a Table fake should imitate.
type Config struct {
	// Name is the table name; requests naming a different table are rejected.
	Name string
	// Key is the primary key schema.
	Key Key
	// GSIs maps index name to its key schema. An item participates in an
	// index only when it carries every attribute of that index's key, which
	// is what makes real sparse indexes sparse.
	GSIs map[string]Key
	// PageSize caps how many items one Query reads before returning a
	// LastEvaluatedKey. Zero means "everything in one page"; set it to force
	// multi-page responses and exercise a store's pagination loop.
	PageSize int
}

// Table is an in-memory DynamoDB table. The zero value is not usable; call New.
// It is safe for concurrent use.
type Table struct {
	cfg Config

	mu     sync.Mutex
	items  map[string]map[string]types.AttributeValue
	calls  map[string]int
	faults map[string][]error
	unproc int
	seq    uint64
	seqOf  map[string]uint64
}

// New creates an empty table. Config.Name defaults to "test-table".
func New(cfg Config) *Table {
	if cfg.Name == "" {
		cfg.Name = "test-table"
	}
	return &Table{
		cfg:    cfg,
		items:  map[string]map[string]types.AttributeValue{},
		calls:  map[string]int{},
		faults: map[string][]error{},
		seqOf:  map[string]uint64{},
	}
}

// Name returns the table name the fake answers to.
func (t *Table) Name() string { return t.cfg.Name }

var _ dynamostore.API = (*Table)(nil)

// --- test controls ---

// FailNext queues err to be returned by the next call to op ("PutItem",
// "GetItem", "DeleteItem", "Query", "Scan", "BatchWriteItem",
// "TransactWriteItems"). Queued errors are consumed in order, one per call, so
// a test can fail a specific call within a multi-call operation: queue nil for
// the calls that should succeed, then the error for the one that should not.
func (t *Table) FailNext(op string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.faults[op] = append(t.faults[op], err)
}

// ThrottleBatches makes the next n BatchWriteItem calls return every request as
// unprocessed instead of applying it, which is how DynamoDB reports throttling.
func (t *Table) ThrottleBatches(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.unproc = n
}

// Calls reports how many times op was invoked, including calls that failed.
func (t *Table) Calls(op string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls[op]
}

// Len reports how many items the table holds.
func (t *Table) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.items)
}

// Items returns every stored item, ordered by primary key.
func (t *Table) Items() []map[string]types.AttributeValue {
	t.mu.Lock()
	defer t.mu.Unlock()
	keys := make([]string, 0, len(t.items))
	for k := range t.items {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]map[string]types.AttributeValue, 0, len(keys))
	for _, k := range keys {
		out = append(out, cloneItem(t.items[k]))
	}
	return out
}

// Seed inserts items directly, bypassing conditions — the way to set up
// pre-existing (including deliberately malformed) rows.
func (t *Table) Seed(items ...map[string]types.AttributeValue) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, it := range items {
		if err := t.put(it); err != nil {
			return err
		}
	}
	return nil
}

// --- dynamostore.API ---

func (t *Table) PutItem(_ context.Context, in *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.begin("PutItem", in.TableName); err != nil {
		return nil, err
	}
	ok, err := t.checkCondition(in.Item, in.ConditionExpression, in.ExpressionAttributeNames, in.ExpressionAttributeValues)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &types.ConditionalCheckFailedException{Message: aws.String("The conditional request failed")}
	}
	if err := t.put(in.Item); err != nil {
		return nil, err
	}
	return &dynamodb.PutItemOutput{}, nil
}

func (t *Table) GetItem(_ context.Context, in *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.begin("GetItem", in.TableName); err != nil {
		return nil, err
	}
	id, err := t.keyOf(in.Key)
	if err != nil {
		return nil, err
	}
	item, ok := t.items[id]
	if !ok {
		return &dynamodb.GetItemOutput{}, nil
	}
	return &dynamodb.GetItemOutput{Item: cloneItem(item)}, nil
}

func (t *Table) DeleteItem(_ context.Context, in *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.begin("DeleteItem", in.TableName); err != nil {
		return nil, err
	}
	id, err := t.keyOf(in.Key)
	if err != nil {
		return nil, err
	}
	ok, err := t.checkCondition(t.items[id], in.ConditionExpression, in.ExpressionAttributeNames, in.ExpressionAttributeValues)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &types.ConditionalCheckFailedException{Message: aws.String("The conditional request failed")}
	}
	delete(t.items, id)
	delete(t.seqOf, id)
	return &dynamodb.DeleteItemOutput{}, nil
}

func (t *Table) BatchWriteItem(_ context.Context, in *dynamodb.BatchWriteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.begin("BatchWriteItem", nil); err != nil {
		return nil, err
	}

	writes, ok := in.RequestItems[t.cfg.Name]
	if !ok && len(in.RequestItems) > 0 {
		return nil, fmt.Errorf("dynamotest: BatchWriteItem for unknown table(s) %v, this fake is %q", tableNames(in.RequestItems), t.cfg.Name)
	}
	if len(writes) > maxBatchWriteItems {
		return nil, fmt.Errorf("dynamotest: BatchWriteItem got %d requests, DynamoDB allows at most %d", len(writes), maxBatchWriteItems)
	}
	if t.unproc > 0 {
		t.unproc--
		return &dynamodb.BatchWriteItemOutput{UnprocessedItems: map[string][]types.WriteRequest{t.cfg.Name: writes}}, nil
	}

	for _, w := range writes {
		switch {
		case w.PutRequest != nil && w.DeleteRequest != nil:
			return nil, fmt.Errorf("dynamotest: a WriteRequest carries both a put and a delete")
		case w.PutRequest != nil:
			if err := t.put(w.PutRequest.Item); err != nil {
				return nil, err
			}
		case w.DeleteRequest != nil:
			id, err := t.keyOf(w.DeleteRequest.Key)
			if err != nil {
				return nil, err
			}
			delete(t.items, id)
			delete(t.seqOf, id)
		default:
			return nil, fmt.Errorf("dynamotest: an empty WriteRequest was submitted")
		}
	}
	return &dynamodb.BatchWriteItemOutput{}, nil
}

func (t *Table) TransactWriteItems(_ context.Context, in *dynamodb.TransactWriteItemsInput, _ ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.begin("TransactWriteItems", nil); err != nil {
		return nil, err
	}
	if len(in.TransactItems) > maxTransactWriteItems {
		return nil, fmt.Errorf("dynamotest: TransactWriteItems got %d items, DynamoDB allows at most %d", len(in.TransactItems), maxTransactWriteItems)
	}

	// Stage everything first so a rejected item leaves the table untouched,
	// matching a transaction's all-or-nothing contract.
	type staged struct {
		id   string
		item map[string]types.AttributeValue // nil means delete
	}
	var plan []staged
	seen := map[string]bool{}

	for i, ti := range in.TransactItems {
		switch {
		case ti.Put != nil:
			if err := t.checkTableName(ti.Put.TableName); err != nil {
				return nil, err
			}
			id, err := t.keyOf(ti.Put.Item)
			if err != nil {
				return nil, err
			}
			ok, err := t.checkCondition(ti.Put.Item, ti.Put.ConditionExpression, ti.Put.ExpressionAttributeNames, ti.Put.ExpressionAttributeValues)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, transactionCancelled(i, "ConditionalCheckFailed")
			}
			if seen[id] {
				// DynamoDB rejects a transaction that touches the same item
				// twice; a store relying on that ordering has a real bug.
				return nil, transactionCancelled(i, "ValidationError: transaction operates on the same item more than once")
			}
			seen[id] = true
			plan = append(plan, staged{id: id, item: cloneItem(ti.Put.Item)})

		case ti.Delete != nil:
			if err := t.checkTableName(ti.Delete.TableName); err != nil {
				return nil, err
			}
			id, err := t.keyOf(ti.Delete.Key)
			if err != nil {
				return nil, err
			}
			ok, err := t.checkCondition(t.items[id], ti.Delete.ConditionExpression, ti.Delete.ExpressionAttributeNames, ti.Delete.ExpressionAttributeValues)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, transactionCancelled(i, "ConditionalCheckFailed")
			}
			if seen[id] {
				return nil, transactionCancelled(i, "ValidationError: transaction operates on the same item more than once")
			}
			seen[id] = true
			plan = append(plan, staged{id: id})

		default:
			return nil, fmt.Errorf("dynamotest: TransactWriteItems item %d uses an operation this fake does not model (only Put and Delete are supported)", i)
		}
	}

	for _, s := range plan {
		if s.item == nil {
			delete(t.items, s.id)
			delete(t.seqOf, s.id)
			continue
		}
		t.store(s.id, s.item)
	}
	return &dynamodb.TransactWriteItemsOutput{}, nil
}

// maxBatchWriteItems and maxTransactWriteItems are DynamoDB's hard per-call
// limits; exceeding them is a request error, not a partial write.
const (
	maxBatchWriteItems    = 25
	maxTransactWriteItems = 100
)

func (t *Table) Query(_ context.Context, in *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.begin("Query", in.TableName); err != nil {
		return nil, err
	}

	idxKey, err := t.indexKey(in.IndexName)
	if err != nil {
		return nil, err
	}
	if in.KeyConditionExpression == nil {
		return nil, fmt.Errorf("dynamotest: Query requires a KeyConditionExpression")
	}
	keyCond, err := parseExpression(*in.KeyConditionExpression)
	if err != nil {
		return nil, err
	}
	if err := checkKeyRefs(keyCond, idxKey, in.ExpressionAttributeNames); err != nil {
		return nil, err
	}

	var filter node
	if in.FilterExpression != nil {
		if filter, err = parseExpression(*in.FilterExpression); err != nil {
			return nil, err
		}
	}

	// Only items projected into the index participate: a sparse GSI omits
	// items missing any of its key attributes.
	candidates := make([]map[string]types.AttributeValue, 0, len(t.items))
	for _, item := range t.items {
		if !inIndex(item, idxKey) {
			continue
		}
		match, err := keyCond.eval(env{item: item, names: in.ExpressionAttributeNames, values: in.ExpressionAttributeValues})
		if err != nil {
			return nil, err
		}
		if match {
			candidates = append(candidates, item)
		}
	}
	t.sortByIndex(candidates, idxKey)

	if in.ScanIndexForward != nil && !*in.ScanIndexForward {
		reverse(candidates)
	}

	if len(in.ExclusiveStartKey) > 0 {
		start, err := t.keyOf(in.ExclusiveStartKey)
		if err != nil {
			return nil, fmt.Errorf("dynamotest: invalid ExclusiveStartKey: %w", err)
		}
		pos := -1
		for i, item := range candidates {
			if id, err := t.keyOf(item); err == nil && id == start {
				pos = i
				break
			}
		}
		if pos < 0 {
			return nil, fmt.Errorf("dynamotest: ExclusiveStartKey %s does not name an item in this query's result set", start)
		}
		candidates = candidates[pos+1:]
	}

	// DynamoDB reads up to Limit items and *then* applies the filter, so a
	// filtered query can return fewer rows than Limit while still having more
	// to read. Stores that assume otherwise are wrong against real DynamoDB.
	budget := len(candidates)
	if t.cfg.PageSize > 0 && t.cfg.PageSize < budget {
		budget = t.cfg.PageSize
	}
	if in.Limit != nil && int(*in.Limit) < budget {
		budget = int(*in.Limit)
	}
	read := candidates[:budget]

	out := &dynamodb.QueryOutput{}
	for _, item := range read {
		out.ScannedCount++
		if filter != nil {
			match, err := filter.eval(env{item: item, names: in.ExpressionAttributeNames, values: in.ExpressionAttributeValues})
			if err != nil {
				return nil, err
			}
			if !match {
				continue
			}
		}
		out.Items = append(out.Items, cloneItem(item))
		out.Count++
	}

	if len(read) < len(candidates) {
		last := read[len(read)-1]
		lek := map[string]types.AttributeValue{t.cfg.Key.Hash: last[t.cfg.Key.Hash]}
		if t.cfg.Key.Range != "" {
			lek[t.cfg.Key.Range] = last[t.cfg.Key.Range]
		}
		if in.IndexName != nil {
			lek[idxKey.Hash] = last[idxKey.Hash]
			if idxKey.Range != "" {
				lek[idxKey.Range] = last[idxKey.Range]
			}
		}
		out.LastEvaluatedKey = lek
	}
	return out, nil
}

// Scan walks every item in the table, applying FilterExpression after the read
// budget just as Query does.
func (t *Table) Scan(_ context.Context, in *dynamodb.ScanInput, _ ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.begin("Scan", in.TableName); err != nil {
		return nil, err
	}
	if in.IndexName != nil {
		if _, err := t.indexKey(in.IndexName); err != nil {
			return nil, err
		}
	}

	var filter node
	if in.FilterExpression != nil {
		var err error
		if filter, err = parseExpression(*in.FilterExpression); err != nil {
			return nil, err
		}
	}

	ids := make([]string, 0, len(t.items))
	for id := range t.items {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	if len(in.ExclusiveStartKey) > 0 {
		start, err := t.keyOf(in.ExclusiveStartKey)
		if err != nil {
			return nil, fmt.Errorf("dynamotest: invalid ExclusiveStartKey: %w", err)
		}
		pos := sort.SearchStrings(ids, start)
		if pos >= len(ids) || ids[pos] != start {
			return nil, fmt.Errorf("dynamotest: ExclusiveStartKey %s does not name a stored item", start)
		}
		ids = ids[pos+1:]
	}

	budget := len(ids)
	if t.cfg.PageSize > 0 && t.cfg.PageSize < budget {
		budget = t.cfg.PageSize
	}
	if in.Limit != nil && int(*in.Limit) < budget {
		budget = int(*in.Limit)
	}
	read := ids[:budget]

	out := &dynamodb.ScanOutput{}
	for _, id := range read {
		item := t.items[id]
		out.ScannedCount++
		if filter != nil {
			match, err := filter.eval(env{item: item, names: in.ExpressionAttributeNames, values: in.ExpressionAttributeValues})
			if err != nil {
				return nil, err
			}
			if !match {
				continue
			}
		}
		out.Items = append(out.Items, cloneItem(item))
		out.Count++
	}

	if len(read) < len(ids) {
		last := t.items[read[len(read)-1]]
		lek := map[string]types.AttributeValue{t.cfg.Key.Hash: last[t.cfg.Key.Hash]}
		if t.cfg.Key.Range != "" {
			lek[t.cfg.Key.Range] = last[t.cfg.Key.Range]
		}
		out.LastEvaluatedKey = lek
	}
	return out, nil
}

// --- internals ---

// begin records the call and applies any queued fault and table-name check.
// It must be called with t.mu held.
func (t *Table) begin(op string, tableName *string) error {
	t.calls[op]++
	if queued := t.faults[op]; len(queued) > 0 {
		err := queued[0]
		t.faults[op] = queued[1:]
		// A queued nil consumes its slot and lets the call run normally, so a
		// test can single out the Nth call of a repeated operation.
		if err != nil {
			return err
		}
	}
	if tableName != nil {
		return t.checkTableName(tableName)
	}
	return nil
}

func (t *Table) checkTableName(name *string) error {
	if name == nil {
		return fmt.Errorf("dynamotest: request is missing a TableName")
	}
	if *name != t.cfg.Name {
		return fmt.Errorf("dynamotest: request targets table %q, this fake is %q", *name, t.cfg.Name)
	}
	return nil
}

func (t *Table) indexKey(name *string) (Key, error) {
	if name == nil {
		return t.cfg.Key, nil
	}
	k, ok := t.cfg.GSIs[*name]
	if !ok {
		return Key{}, fmt.Errorf("dynamotest: query names index %q, which does not exist on table %q", *name, t.cfg.Name)
	}
	return k, nil
}

// keyOf builds the map key identifying an item, and doubles as validation that
// every primary key attribute is present — a write missing one is a request
// DynamoDB would reject.
func (t *Table) keyOf(item map[string]types.AttributeValue) (string, error) {
	hash, ok := item[t.cfg.Key.Hash]
	if !ok {
		return "", fmt.Errorf("dynamotest: item is missing hash key attribute %q", t.cfg.Key.Hash)
	}
	id := encodeKeyValue(hash)
	if t.cfg.Key.Range != "" {
		rng, ok := item[t.cfg.Key.Range]
		if !ok {
			return "", fmt.Errorf("dynamotest: item is missing range key attribute %q", t.cfg.Key.Range)
		}
		id += "\x00" + encodeKeyValue(rng)
	}
	return id, nil
}

func (t *Table) put(item map[string]types.AttributeValue) error {
	id, err := t.keyOf(item)
	if err != nil {
		return err
	}
	t.store(id, cloneItem(item))
	return nil
}

// store writes an item, remembering first-write order so queries stay
// deterministic when two items share an index sort key.
func (t *Table) store(id string, item map[string]types.AttributeValue) {
	if _, exists := t.seqOf[id]; !exists {
		t.seq++
		t.seqOf[id] = t.seq
	}
	t.items[id] = item
}

func (t *Table) checkCondition(item map[string]types.AttributeValue, expr *string, names map[string]string, values map[string]types.AttributeValue) (bool, error) {
	if expr == nil {
		return true, nil
	}
	n, err := parseExpression(*expr)
	if err != nil {
		return false, err
	}
	// A condition on a put evaluates against the item already stored under
	// that key, not against the item being written.
	var existing map[string]types.AttributeValue
	if id, err := t.keyOf(item); err == nil {
		existing = t.items[id]
	}
	return n.eval(env{item: existing, names: names, values: values})
}

// sortByIndex orders items by the index's range key, breaking ties by write
// order so results never depend on Go's map iteration.
func (t *Table) sortByIndex(items []map[string]types.AttributeValue, idx Key) {
	sort.SliceStable(items, func(i, j int) bool {
		if idx.Range != "" {
			a, aok := items[i][idx.Range]
			b, bok := items[j][idx.Range]
			if aok && bok {
				if cmp, ok := avCompare(a, b); ok && cmp != 0 {
					return cmp < 0
				}
			}
		}
		ai, _ := t.keyOf(items[i])
		bj, _ := t.keyOf(items[j])
		if ai != bj {
			return t.seqOf[ai] < t.seqOf[bj]
		}
		return false
	})
}

// inIndex reports whether an item carries every attribute of an index's key.
func inIndex(item map[string]types.AttributeValue, idx Key) bool {
	if _, ok := item[idx.Hash]; !ok {
		return false
	}
	if idx.Range == "" {
		return true
	}
	_, ok := item[idx.Range]
	return ok
}

// checkKeyRefs rejects a key condition that touches a non-key attribute, which
// real DynamoDB refuses — such a predicate belongs in FilterExpression.
func checkKeyRefs(n node, idx Key, names map[string]string) error {
	for _, ref := range refs(n) {
		resolved := ref
		if strings.HasPrefix(ref, "#") {
			r, ok := names[ref]
			if !ok {
				return fmt.Errorf("dynamotest: key condition uses undefined name %s", ref)
			}
			resolved = r
		}
		if resolved != idx.Hash && (idx.Range == "" || resolved != idx.Range) {
			return fmt.Errorf("dynamotest: key condition references %q, which is not a key attribute of the queried index (hash %q, range %q)", resolved, idx.Hash, idx.Range)
		}
	}
	return nil
}

// refs collects every attribute path an expression reads.
func refs(n node) []string {
	var out []string
	var walk func(node)
	collect := func(ops ...operand) {
		for _, o := range ops {
			if o.valueRef == "" && o.path != "" {
				out = append(out, o.path)
			}
		}
	}
	walk = func(n node) {
		switch v := n.(type) {
		case andNode:
			walk(v.left)
			walk(v.right)
		case orNode:
			walk(v.left)
			walk(v.right)
		case notNode:
			walk(v.inner)
		case compareNode:
			collect(v.left, v.right)
		case betweenNode:
			collect(v.val, v.lo, v.hi)
		case inNode:
			collect(append([]operand{v.val}, v.list...)...)
		case funcNode:
			collect(v.args...)
		}
	}
	walk(n)
	return out
}

func encodeKeyValue(v types.AttributeValue) string {
	switch av := v.(type) {
	case *types.AttributeValueMemberS:
		return "S:" + av.Value
	case *types.AttributeValueMemberN:
		return "N:" + av.Value
	case *types.AttributeValueMemberB:
		return "B:" + string(av.Value)
	}
	return fmt.Sprintf("?:%v", v)
}

func cloneItem(item map[string]types.AttributeValue) map[string]types.AttributeValue {
	if item == nil {
		return nil
	}
	out := make(map[string]types.AttributeValue, len(item))
	for k, v := range item {
		out[k] = v
	}
	return out
}

func reverse(items []map[string]types.AttributeValue) {
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
}

func tableNames(m map[string][]types.WriteRequest) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func transactionCancelled(index int, reason string) error {
	return &types.TransactionCanceledException{
		Message: aws.String(fmt.Sprintf("Transaction cancelled at item %d: %s", index, reason)),
	}
}
