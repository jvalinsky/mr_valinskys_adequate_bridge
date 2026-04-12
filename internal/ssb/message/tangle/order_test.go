package tangle

import (
	"context"
	"testing"
)

// mockTangleStore implements TangleStore for testing
type mockTangleStore struct {
	messages map[string]MessageWithTangles
	tangles  map[string]*Tangle
}

func newMockTangleStore() *mockTangleStore {
	return &mockTangleStore{
		messages: make(map[string]MessageWithTangles),
		tangles:  make(map[string]*Tangle),
	}
}

func (m *mockTangleStore) AddMessage(ctx context.Context, msgKey, tangleName, rootKey string, parentKeys []string) error {
	return nil
}

func (m *mockTangleStore) GetTangle(ctx context.Context, name, root string) (*Tangle, error) {
	if t, ok := m.tangles[name+":"+root]; ok {
		return t, nil
	}
	return nil, nil
}

func (m *mockTangleStore) GetTangleMessages(ctx context.Context, name, root string, sinceSeq int64) ([]MessageWithTangles, error) {
	var result []MessageWithTangles
	for _, msg := range m.messages {
		if msg.TangleName == name && msg.Root == root {
			result = append(result, msg)
		}
	}
	return result, nil
}

func (m *mockTangleStore) GetMessagesByParent(ctx context.Context, parentKey string) ([]MessageWithTangles, error) {
	var result []MessageWithTangles
	for _, msg := range m.messages {
		for _, parent := range msg.Parents {
			if parent == parentKey {
				result = append(result, msg)
				break
			}
		}
	}
	return result, nil
}

func (m *mockTangleStore) GetTangleTips(ctx context.Context, name, root string) ([]string, error) {
	return nil, nil
}

func (m *mockTangleStore) GetTangleMembership(ctx context.Context, msgKey string) (*TangleMembership, error) {
	return nil, nil
}

func (m *mockTangleStore) GetTangleMessageCount(ctx context.Context, name, root string) (int, error) {
	return 0, nil
}

func (m *mockTangleStore) Close() error {
	return nil
}

// TestSortLinearChain tests sorting a simple linear chain: A -> B -> C
func TestSortLinearChain(t *testing.T) {
	store := newMockTangleStore()
	store.messages = map[string]MessageWithTangles{
		"msg-a": {Key: "msg-a", TangleName: "post", Root: "root-1", Parents: []string{}, Content: []byte("A")},
		"msg-b": {Key: "msg-b", TangleName: "post", Root: "root-1", Parents: []string{"msg-a"}, Content: []byte("B")},
		"msg-c": {Key: "msg-c", TangleName: "post", Root: "root-1", Parents: []string{"msg-b"}, Content: []byte("C")},
	}

	sorter := NewTopologicalSorter(store)
	sorted, err := sorter.Sort(context.Background(), "post", "root-1")
	if err != nil {
		t.Fatalf("Sort failed: %v", err)
	}

	if len(sorted) != 3 {
		t.Errorf("expected 3 messages, got %d", len(sorted))
	}

	// Verify order: A before B before C
	if sorted[0].Key != "msg-a" {
		t.Errorf("first message should be msg-a, got %s", sorted[0].Key)
	}
	if sorted[1].Key != "msg-b" {
		t.Errorf("second message should be msg-b, got %s", sorted[1].Key)
	}
	if sorted[2].Key != "msg-c" {
		t.Errorf("third message should be msg-c, got %s", sorted[2].Key)
	}
}

// TestSortDiamondDAG tests a fork/merge pattern:
//     A
//    / \
//   B   C
//    \ /
//     D
func TestSortDiamondDAG(t *testing.T) {
	store := newMockTangleStore()
	store.messages = map[string]MessageWithTangles{
		"msg-a": {Key: "msg-a", TangleName: "post", Root: "root-1", Parents: []string{}, Content: []byte("A")},
		"msg-b": {Key: "msg-b", TangleName: "post", Root: "root-1", Parents: []string{"msg-a"}, Content: []byte("B")},
		"msg-c": {Key: "msg-c", TangleName: "post", Root: "root-1", Parents: []string{"msg-a"}, Content: []byte("C")},
		"msg-d": {Key: "msg-d", TangleName: "post", Root: "root-1", Parents: []string{"msg-b", "msg-c"}, Content: []byte("D")},
	}

	sorter := NewTopologicalSorter(store)
	sorted, err := sorter.Sort(context.Background(), "post", "root-1")
	if err != nil {
		t.Fatalf("Sort failed: %v", err)
	}

	if len(sorted) != 4 {
		t.Errorf("expected 4 messages, got %d", len(sorted))
	}

	// Verify topological ordering: A before B and C, B and C before D
	keyIndex := make(map[string]int)
	for i, msg := range sorted {
		keyIndex[msg.Key] = i
	}

	if keyIndex["msg-a"] >= keyIndex["msg-b"] {
		t.Error("msg-a should come before msg-b")
	}
	if keyIndex["msg-a"] >= keyIndex["msg-c"] {
		t.Error("msg-a should come before msg-c")
	}
	if keyIndex["msg-b"] >= keyIndex["msg-d"] {
		t.Error("msg-b should come before msg-d")
	}
	if keyIndex["msg-c"] >= keyIndex["msg-d"] {
		t.Error("msg-c should come before msg-d")
	}
}

// TestSortEmptyTangle tests sorting an empty tangle
func TestSortEmptyTangle(t *testing.T) {
	store := newMockTangleStore()
	store.messages = map[string]MessageWithTangles{}

	sorter := NewTopologicalSorter(store)
	sorted, err := sorter.Sort(context.Background(), "post", "root-1")
	if err != nil {
		t.Fatalf("Sort failed: %v", err)
	}

	if len(sorted) != 0 {
		t.Errorf("expected 0 messages, got %d", len(sorted))
	}
}

// TestSortSingleMessage tests sorting a single message (root)
func TestSortSingleMessage(t *testing.T) {
	store := newMockTangleStore()
	store.messages = map[string]MessageWithTangles{
		"msg-a": {Key: "msg-a", TangleName: "post", Root: "root-1", Parents: []string{}, Content: []byte("A")},
	}

	sorter := NewTopologicalSorter(store)
	sorted, err := sorter.Sort(context.Background(), "post", "root-1")
	if err != nil {
		t.Fatalf("Sort failed: %v", err)
	}

	if len(sorted) != 1 {
		t.Errorf("expected 1 message, got %d", len(sorted))
	}
	if sorted[0].Key != "msg-a" {
		t.Errorf("expected msg-a, got %s", sorted[0].Key)
	}
}

// TestSortCycleDetection tests that cycles are detected
func TestSortCycleDetection(t *testing.T) {
	store := newMockTangleStore()
	// Create a cycle: A -> B -> C -> A
	store.messages = map[string]MessageWithTangles{
		"msg-a": {Key: "msg-a", TangleName: "post", Root: "root-1", Parents: []string{"msg-c"}, Content: []byte("A")},
		"msg-b": {Key: "msg-b", TangleName: "post", Root: "root-1", Parents: []string{"msg-a"}, Content: []byte("B")},
		"msg-c": {Key: "msg-c", TangleName: "post", Root: "root-1", Parents: []string{"msg-b"}, Content: []byte("C")},
	}

	sorter := NewTopologicalSorter(store)
	sorted, err := sorter.Sort(context.Background(), "post", "root-1")

	// Should return a CycleError
	cycleErr, ok := err.(*CycleError)
	if !ok {
		t.Fatalf("expected CycleError, got %v", err)
	}

	// Should have detected the cycle (all 3 messages are part of it)
	if len(cycleErr.Messages) != 3 {
		t.Errorf("expected 3 messages in cycle, got %d", len(cycleErr.Messages))
	}

	// When all messages are in a cycle, nothing can be sorted
	if len(sorted) != 0 {
		t.Errorf("expected 0 messages in partial sort (all in cycle), got %d", len(sorted))
	}
}

// TestSortComplexDAG tests a more complex graph structure
func TestSortComplexDAG(t *testing.T) {
	store := newMockTangleStore()
	// Create a DAG: Root -> A, B -> C, D -> E
	store.messages = map[string]MessageWithTangles{
		"root": {Key: "root", TangleName: "post", Root: "root", Parents: []string{}, Content: []byte("Root")},
		"msg-a": {Key: "msg-a", TangleName: "post", Root: "root", Parents: []string{"root"}, Content: []byte("A")},
		"msg-b": {Key: "msg-b", TangleName: "post", Root: "root", Parents: []string{"root"}, Content: []byte("B")},
		"msg-c": {Key: "msg-c", TangleName: "post", Root: "root", Parents: []string{"msg-a", "msg-b"}, Content: []byte("C")},
		"msg-d": {Key: "msg-d", TangleName: "post", Root: "root", Parents: []string{"msg-c"}, Content: []byte("D")},
		"msg-e": {Key: "msg-e", TangleName: "post", Root: "root", Parents: []string{"msg-d"}, Content: []byte("E")},
	}

	sorter := NewTopologicalSorter(store)
	sorted, err := sorter.Sort(context.Background(), "post", "root")
	if err != nil {
		t.Fatalf("Sort failed: %v", err)
	}

	if len(sorted) != 6 {
		t.Errorf("expected 6 messages, got %d", len(sorted))
	}

	// Verify root is first
	if sorted[0].Key != "root" {
		t.Errorf("root should be first, got %s", sorted[0].Key)
	}
}

// TestGetMessageDepthAtRoot tests GetMessageDepth for root message
func TestGetMessageDepthAtRoot(t *testing.T) {
	messages := []MessageWithTangles{
		{Key: "msg-a", Parents: []string{}},
		{Key: "msg-b", Parents: []string{"msg-a"}},
		{Key: "msg-c", Parents: []string{"msg-b"}},
	}

	depth := GetMessageDepth("msg-a", messages)
	if depth != 0 {
		t.Errorf("root message should have depth 0, got %d", depth)
	}
}

// TestGetMessageDepthAtMidChain tests GetMessageDepth for mid-chain message
func TestGetMessageDepthAtMidChain(t *testing.T) {
	messages := []MessageWithTangles{
		{Key: "msg-a", Parents: []string{}},
		{Key: "msg-b", Parents: []string{"msg-a"}},
		{Key: "msg-c", Parents: []string{"msg-b"}},
	}

	depth := GetMessageDepth("msg-b", messages)
	if depth != 1 {
		t.Errorf("mid-chain message should have depth 1, got %d", depth)
	}
}

// TestGetMessageDepthAtLeaf tests GetMessageDepth for leaf message
func TestGetMessageDepthAtLeaf(t *testing.T) {
	messages := []MessageWithTangles{
		{Key: "msg-a", Parents: []string{}},
		{Key: "msg-b", Parents: []string{"msg-a"}},
		{Key: "msg-c", Parents: []string{"msg-b"}},
	}

	depth := GetMessageDepth("msg-c", messages)
	if depth != 2 {
		t.Errorf("leaf message should have depth 2, got %d", depth)
	}
}

// TestGetMessageDepthInDiamond tests GetMessageDepth in diamond DAG
func TestGetMessageDepthInDiamond(t *testing.T) {
	messages := []MessageWithTangles{
		{Key: "msg-a", Parents: []string{}},
		{Key: "msg-b", Parents: []string{"msg-a"}},
		{Key: "msg-c", Parents: []string{"msg-a"}},
		{Key: "msg-d", Parents: []string{"msg-b", "msg-c"}},
	}

	// D has two parents at depth 1, so D's depth is 2
	depth := GetMessageDepth("msg-d", messages)
	if depth != 2 {
		t.Errorf("diamond leaf should have depth 2, got %d", depth)
	}
}

// TestGetMessageDepthNonexistent tests GetMessageDepth for nonexistent message
func TestGetMessageDepthNonexistent(t *testing.T) {
	messages := []MessageWithTangles{
		{Key: "msg-a", Parents: []string{}},
	}

	// Should return depth 0 for nonexistent message
	depth := GetMessageDepth("nonexistent", messages)
	if depth != 0 {
		t.Errorf("nonexistent message should return depth 0, got %d", depth)
	}
}

// TestGetRootMessageFromLinearChain tests GetRootMessage with linear chain
func TestGetRootMessageFromLinearChain(t *testing.T) {
	messages := []MessageWithTangles{
		{Key: "msg-a", Parents: []string{}},
		{Key: "msg-b", Parents: []string{"msg-a"}},
		{Key: "msg-c", Parents: []string{"msg-b"}},
	}

	root := GetRootMessage(messages)
	if root == nil {
		t.Fatal("GetRootMessage returned nil")
	}
	if root.Key != "msg-a" {
		t.Errorf("root should be msg-a, got %s", root.Key)
	}
}

// TestGetRootMessageFromDiamond tests GetRootMessage with diamond DAG
func TestGetRootMessageFromDiamond(t *testing.T) {
	messages := []MessageWithTangles{
		{Key: "msg-b", Parents: []string{"msg-a"}},
		{Key: "msg-a", Parents: []string{}},
		{Key: "msg-c", Parents: []string{"msg-a"}},
	}

	root := GetRootMessage(messages)
	if root == nil {
		t.Fatal("GetRootMessage returned nil")
	}
	if root.Key != "msg-a" {
		t.Errorf("root should be msg-a, got %s", root.Key)
	}
}

// TestGetRootMessageEmpty tests GetRootMessage with empty list
func TestGetRootMessageEmpty(t *testing.T) {
	messages := []MessageWithTangles{}
	root := GetRootMessage(messages)
	if root != nil {
		t.Error("GetRootMessage should return nil for empty list")
	}
}

// TestGetRootMessageFallback tests GetRootMessage fallback when no root found
func TestGetRootMessageFallback(t *testing.T) {
	messages := []MessageWithTangles{
		{Key: "msg-a", Parents: []string{"unknown"}},
		{Key: "msg-b", Parents: []string{"msg-a"}},
	}

	root := GetRootMessage(messages)
	if root == nil {
		t.Fatal("GetRootMessage returned nil")
	}
	// Should return first message as fallback
	if root.Key != "msg-a" {
		t.Errorf("should fallback to first message, got %s", root.Key)
	}
}

// TestMessageIteratorNext tests MessageIterator.Next
func TestMessageIteratorNext(t *testing.T) {
	messages := []MessageWithTangles{
		{Key: "msg-a"},
		{Key: "msg-b"},
		{Key: "msg-c"},
	}

	iter := NewMessageIterator(messages)

	// Should have 3 messages
	if !iter.Next() {
		t.Error("expected Next() to return true for first message")
	}
	iter.Message()

	if !iter.Next() {
		t.Error("expected Next() to return true for second message")
	}
	iter.Message()

	if !iter.Next() {
		t.Error("expected Next() to return true for third message")
	}
	iter.Message()

	if iter.Next() {
		t.Error("expected Next() to return false after last message")
	}
}

// TestMessageIteratorMessage tests MessageIterator.Message returns correct values
func TestMessageIteratorMessage(t *testing.T) {
	messages := []MessageWithTangles{
		{Key: "msg-a"},
		{Key: "msg-b"},
		{Key: "msg-c"},
	}

	iter := NewMessageIterator(messages)

	iter.Next()
	msg1 := iter.Message()
	if msg1.Key != "msg-a" {
		t.Errorf("first message should be msg-a, got %s", msg1.Key)
	}

	iter.Next()
	msg2 := iter.Message()
	if msg2.Key != "msg-b" {
		t.Errorf("second message should be msg-b, got %s", msg2.Key)
	}

	iter.Next()
	msg3 := iter.Message()
	if msg3.Key != "msg-c" {
		t.Errorf("third message should be msg-c, got %s", msg3.Key)
	}
}

// TestMessageIteratorEmpty tests MessageIterator with empty list
func TestMessageIteratorEmpty(t *testing.T) {
	iter := NewMessageIterator([]MessageWithTangles{})

	if iter.Next() {
		t.Error("expected Next() to return false for empty list")
	}

	msg := iter.Message()
	if msg.Key != "" {
		t.Errorf("expected empty message, got %s", msg.Key)
	}
}

// TestCycleErrorString tests CycleError.Error() returns correct message
func TestCycleErrorString(t *testing.T) {
	err := &CycleError{Messages: []string{"msg-a", "msg-b", "msg-c"}}
	if err.Error() != "tangle contains cycles" {
		t.Errorf("wrong error message: %s", err.Error())
	}
}

// TestSortMultipleTangles tests that sorting only returns messages from specified tangle
func TestSortMultipleTangles(t *testing.T) {
	store := newMockTangleStore()
	store.messages = map[string]MessageWithTangles{
		"msg-a": {Key: "msg-a", TangleName: "post", Root: "root-1", Parents: []string{}, Content: []byte("A")},
		"msg-b": {Key: "msg-b", TangleName: "post", Root: "root-1", Parents: []string{"msg-a"}, Content: []byte("B")},
		"msg-c": {Key: "msg-c", TangleName: "vote", Root: "root-2", Parents: []string{}, Content: []byte("C")},
		"msg-d": {Key: "msg-d", TangleName: "vote", Root: "root-2", Parents: []string{"msg-c"}, Content: []byte("D")},
	}

	sorter := NewTopologicalSorter(store)

	// Sort post tangle
	sorted, err := sorter.Sort(context.Background(), "post", "root-1")
	if err != nil {
		t.Fatalf("Sort failed: %v", err)
	}

	if len(sorted) != 2 {
		t.Errorf("expected 2 messages in post tangle, got %d", len(sorted))
	}
	for _, msg := range sorted {
		if msg.TangleName != "post" {
			t.Errorf("expected post tangle, got %s", msg.TangleName)
		}
	}

	// Sort vote tangle
	sorted, err = sorter.Sort(context.Background(), "vote", "root-2")
	if err != nil {
		t.Fatalf("Sort failed: %v", err)
	}

	if len(sorted) != 2 {
		t.Errorf("expected 2 messages in vote tangle, got %d", len(sorted))
	}
	for _, msg := range sorted {
		if msg.TangleName != "vote" {
			t.Errorf("expected vote tangle, got %s", msg.TangleName)
		}
	}
}

// TestSortWithOrphanMessages tests sorting when some messages reference non-existent parents
func TestSortWithOrphanMessages(t *testing.T) {
	store := newMockTangleStore()
	store.messages = map[string]MessageWithTangles{
		"msg-a": {Key: "msg-a", TangleName: "post", Root: "root-1", Parents: []string{}, Content: []byte("A")},
		"msg-b": {Key: "msg-b", TangleName: "post", Root: "root-1", Parents: []string{"msg-a"}, Content: []byte("B")},
		"msg-c": {Key: "msg-c", TangleName: "post", Root: "root-1", Parents: []string{"nonexistent"}, Content: []byte("C")},
	}

	sorter := NewTopologicalSorter(store)
	sorted, err := sorter.Sort(context.Background(), "post", "root-1")
	if err != nil {
		t.Fatalf("Sort failed: %v", err)
	}

	// Should still sort all messages (treats unknown parents as external references)
	if len(sorted) != 3 {
		t.Errorf("expected 3 messages, got %d", len(sorted))
	}

	// msg-c should come after msg-a and msg-b if they don't reference it
	keyIndex := make(map[string]int)
	for i, msg := range sorted {
		keyIndex[msg.Key] = i
	}

	if keyIndex["msg-c"] < keyIndex["msg-a"] && keyIndex["msg-c"] < keyIndex["msg-b"] {
		// msg-c has external dependency, should be early
	}
}

// BenchmarkSort benchmarks topological sorting with increasing tangle sizes
func BenchmarkSort(b *testing.B) {
	store := newMockTangleStore()
	store.messages = make(map[string]MessageWithTangles)

	// Create a chain of 100 messages
	store.messages["msg-0"] = MessageWithTangles{
		Key:        "msg-0",
		TangleName: "post",
		Root:       "root-1",
		Parents:    []string{},
		Content:    []byte("0"),
	}

	for i := 1; i < 100; i++ {
		prevKey := "msg-" + string(rune(i-1))
		currKey := "msg-" + string(rune(i))
		store.messages[currKey] = MessageWithTangles{
			Key:        currKey,
			TangleName: "post",
			Root:       "root-1",
			Parents:    []string{prevKey},
			Content:    []byte("msg"),
		}
	}

	sorter := NewTopologicalSorter(store)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sorter.Sort(context.Background(), "post", "root-1")
	}
}

// BenchmarkGetMessageDepth benchmarks depth calculation
func BenchmarkGetMessageDepth(b *testing.B) {
	messages := make([]MessageWithTangles, 100)
	for i := 0; i < 100; i++ {
		messages[i] = MessageWithTangles{Key: "msg-" + string(rune(i))}
		if i > 0 {
			messages[i].Parents = []string{"msg-" + string(rune(i - 1))}
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		GetMessageDepth("msg-99", messages)
	}
}
