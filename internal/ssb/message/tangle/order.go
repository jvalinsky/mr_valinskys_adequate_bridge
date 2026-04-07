package tangle

import (
	"context"
	"sort"
)

type TopologicalSorter struct {
	store TangleStore
}

func NewTopologicalSorter(store TangleStore) *TopologicalSorter {
	return &TopologicalSorter{store: store}
}

func (ts *TopologicalSorter) Sort(ctx context.Context, name, root string) ([]MessageWithTangles, error) {
	messages, err := ts.store.GetTangleMessages(ctx, name, root, 0)
	if err != nil {
		return nil, err
	}

	if len(messages) == 0 {
		return messages, nil
	}

	msgMap := make(map[string]MessageWithTangles)
	childrenMap := make(map[string][]string)
	inDegree := make(map[string]int)

	for _, msg := range messages {
		msgMap[msg.Key] = msg
		inDegree[msg.Key] = 0
		childrenMap[msg.Key] = nil
	}

	for _, msg := range messages {
		for _, parent := range msg.Parents {
			if _, exists := msgMap[parent]; exists {
				childrenMap[parent] = append(childrenMap[parent], msg.Key)
				inDegree[msg.Key]++
			}
		}
	}

	var sorted []MessageWithTangles
	queue := make([]string, 0)

	for key, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, key)
		}
	}

	sort.Slice(queue, func(i, j int) bool {
		return i < j
	})

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		sorted = append(sorted, msgMap[current])

		for _, child := range childrenMap[current] {
			inDegree[child]--
			if inDegree[child] == 0 {
				queue = append(queue, child)
				sort.Slice(queue, func(i, j int) bool {
					return i < j
				})
			}
		}
	}

	if len(sorted) != len(messages) {
		var cycles []string
		for key, degree := range inDegree {
			if degree > 0 {
				cycles = append(cycles, key)
			}
		}
		return sorted, &CycleError{Messages: cycles}
	}

	return sorted, nil
}

type CycleError struct {
	Messages []string
}

func (e *CycleError) Error() string {
	return "tangle contains cycles"
}

type MessageIterator struct {
	messages []MessageWithTangles
	index    int
}

func NewMessageIterator(messages []MessageWithTangles) *MessageIterator {
	return &MessageIterator{messages: messages, index: 0}
}

func (mi *MessageIterator) Next() bool {
	return mi.index < len(mi.messages)
}

func (mi *MessageIterator) Message() MessageWithTangles {
	if mi.index >= len(mi.messages) {
		return MessageWithTangles{}
	}
	msg := mi.messages[mi.index]
	mi.index++
	return msg
}

func GetRootMessage(messages []MessageWithTangles) *MessageWithTangles {
	for _, msg := range messages {
		if len(msg.Parents) == 0 {
			return &msg
		}
	}
	if len(messages) > 0 {
		return &messages[0]
	}
	return nil
}

func GetMessageDepth(msgKey string, messages []MessageWithTangles) int {
	msgMap := make(map[string]MessageWithTangles)
	for _, m := range messages {
		msgMap[m.Key] = m
	}

	visited := make(map[string]bool)
	var dfs func(key string, depth int) int
	dfs = func(key string, depth int) int {
		if visited[key] {
			return depth
		}
		visited[key] = true
		msg, ok := msgMap[key]
		if !ok {
			return depth
		}
		if len(msg.Parents) == 0 {
			return depth
		}
		maxDepth := depth
		for _, parent := range msg.Parents {
			d := dfs(parent, depth+1)
			if d > maxDepth {
				maxDepth = d
			}
		}
		return maxDepth
	}

	return dfs(msgKey, 0)
}
