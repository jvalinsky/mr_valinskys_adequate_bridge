package tangle

import (
	"context"
	"encoding/json"
	"time"
)

type TangleStore interface {
	AddMessage(ctx context.Context, msgKey, tangleName, rootKey string, parentKeys []string) error
	GetTangle(ctx context.Context, name, root string) (*Tangle, error)
	GetTangleMessages(ctx context.Context, name, root string, sinceSeq int64) ([]MessageWithTangles, error)
	GetMessagesByParent(ctx context.Context, parentKey string) ([]MessageWithTangles, error)
	GetTangleTips(ctx context.Context, name, root string) ([]string, error)
	GetTangleMembership(ctx context.Context, msgKey string) (*TangleMembership, error)
	GetTangleMessageCount(ctx context.Context, name, root string) (int, error)
	Close() error
}

type Store struct {
	db interface {
		ExecContext(ctx context.Context, query string, args ...interface{}) (interface{}, error)
		QueryContext(ctx context.Context, query string, args ...interface{}) (Rows, error)
		QueryRowContext(ctx context.Context, query string, args ...interface{}) Row
	}
}

type Rows interface {
	Next() bool
	Scan(dest ...interface{}) error
	Close() error
}

type Row interface {
	Scan(dest ...interface{}) error
}

func NewStore(db interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (interface{}, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) Row
}) *Store {
	return &Store{db: db}
}

func (s *Store) AddMessage(ctx context.Context, msgKey, tangleName, rootKey string, parentKeys []string) error {
	parentJSON, _ := json.Marshal(parentKeys)
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO tangle_membership (message_key, tangle_name, root_key, parent_keys, created_at) VALUES (?, ?, ?, ?, ?)`,
		msgKey, tangleName, rootKey, string(parentJSON), time.Now().Unix())
	return err
}

func (s *Store) GetTangle(ctx context.Context, name, root string) (*Tangle, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, root, tips, created_at, updated_at FROM tangles WHERE name = ? AND root = ?`,
		name, root)

	var t Tangle
	var tipsJSON string
	err := row.Scan(&t.ID, &t.Name, &t.Root, &tipsJSON, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(tipsJSON), &t.Tips)
	return &t, nil
}

func (s *Store) GetTangleMessages(ctx context.Context, name, root string, sinceSeq int64) ([]MessageWithTangles, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT tm.message_key, tm.tangle_name, tm.root_key, tm.parent_keys, m.value_json
		 FROM tangle_membership tm
		 LEFT JOIN messages m ON m.key = tm.message_key
		 WHERE tm.tangle_name = ? AND tm.root_key = ?
		 ORDER BY m.seq ASC`,
		name, root)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []MessageWithTangles
	for rows.Next() {
		var m MessageWithTangles
		var parentJSON string
		var content []byte
		err := rows.Scan(&m.Key, &m.TangleName, &m.Root, &parentJSON, &content)
		if err != nil {
			continue
		}
		json.Unmarshal([]byte(parentJSON), &m.Parents)
		m.Content = content
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func (s *Store) GetMessagesByParent(ctx context.Context, parentKey string) ([]MessageWithTangles, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT message_key, tangle_name, root_key, parent_keys FROM tangle_membership WHERE parent_keys LIKE ?`,
		"%"+parentKey+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []MessageWithTangles
	for rows.Next() {
		var m MessageWithTangles
		var parentJSON string
		err := rows.Scan(&m.Key, &m.TangleName, &m.Root, &parentJSON)
		if err != nil {
			continue
		}
		json.Unmarshal([]byte(parentJSON), &m.Parents)
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func (s *Store) GetTangleTips(ctx context.Context, name, root string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT tm.message_key FROM tangle_membership tm
		 WHERE tm.tangle_name = ? AND tm.root_key = ?
		 AND NOT EXISTS (
		   SELECT 1 FROM tangle_membership tm2
		   WHERE tm2.parent_keys LIKE '%' || tm.message_key || '%'
		 )`,
		name, root)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tips []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err == nil {
			tips = append(tips, key)
		}
	}
	return tips, nil
}

func (s *Store) GetTangleMembership(ctx context.Context, msgKey string) (*TangleMembership, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, message_key, tangle_name, root_key, parent_keys, created_at FROM tangle_membership WHERE message_key = ?`,
		msgKey)

	var tm TangleMembership
	var parentJSON string
	err := row.Scan(&tm.ID, &tm.MessageKey, &tm.TangleName, &tm.RootKey, &parentJSON, &tm.CreatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(parentJSON), &tm.ParentKeys)
	return &tm, nil
}

func (s *Store) GetTangleMessageCount(ctx context.Context, name, root string) (int, error) {
	var count int
	row := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tangle_membership WHERE tangle_name = ? AND root_key = ?`,
		name, root)
	err := row.Scan(&count)
	return count, err
}

func (s *Store) Close() error {
	return nil
}

func ExtractTangleMetadata(content map[string]interface{}) (name, root string, parents []string) {
	if tangles, ok := content["tangles"].(map[string]interface{}); ok {
		for n, v := range tangles {
			if t, ok := v.(map[string]interface{}); ok {
				name = n
				if r, ok := t["root"].(string); ok {
					root = r
				}
				if p, ok := t["previous"].([]interface{}); ok {
					for _, pp := range p {
						if ps, ok := pp.(string); ok {
							parents = append(parents, ps)
						}
					}
				}
				return
			}
		}
	}
	return
}
