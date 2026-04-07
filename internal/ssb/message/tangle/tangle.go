package tangle

import (
	"time"
)

type Tangle struct {
	ID        int64
	Name      string
	Root      string
	Tips      []string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type TangleMembership struct {
	ID         int64
	MessageKey string
	TangleName string
	RootKey    string
	ParentKeys []string
	CreatedAt  time.Time
}

type TangleInfo struct {
	Name         string
	Root         string
	Depth        int
	TipCount     int
	MessageCount int
}

type MessageWithTangles struct {
	Key        string
	TangleName string
	Root       string
	Parents    []string
	Content    []byte
}

type TopologicalSortResult struct {
	Messages []MessageWithTangles
	Cycles   [][]string
}

const (
	TangleNamePost    = "post"
	TangleNameVote    = "vote"
	TangleNameContact = "contact"
)
