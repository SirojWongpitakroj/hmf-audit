package peerdht

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"dhtbacked/internal/hashing"
)

type PublicationBatch struct {
	Overlay string
	Entries []Publication
}
type Publisher struct {
	client  *Client
	byOwner [4][]Publication
	bytes   int
}

func NewPublisher(c *Client) *Publisher { return &Publisher{client: c} }
func (p *Publisher) Put(key hashing.Digest, value []byte) error {
	l := p.client.Config.Limits
	cost := len(value)*2 + 256
	if len(value) > l.MaxValueBytes || cost > l.QueueBytes || cost > l.MaxRequestBytes-128 {
		return errors.New("peerdht: publication exceeds queue/request budget")
	}
	if p.bytes+cost > l.QueueBytes {
		if err := p.Flush(); err != nil {
			return err
		}
	}
	owner := Owner(key)
	p.byOwner[owner] = append(p.byOwner[owner], Publication{Key: key, Value: append([]byte(nil), value...)})
	p.bytes += cost
	return nil
}
func (p *Publisher) Flush() error {
	for owner, entries := range p.byOwner {
		if len(entries) == 0 {
			continue
		}
		err := post(p.client.Ctx, p.client.http, p.client.Config.Peers[owner]+"/put-many", PublicationBatch{Overlay: p.client.Config.Overlay, Entries: entries}, p.client.Config.WriteSecret, p.client.Config.Limits.MaxRequestBytes, nil)
		if err != nil {
			return err
		}
		p.byOwner[owner] = nil
	}
	p.bytes = 0
	return nil
}
func (s *LocalStore) PutMany(ctx context.Context, entries []Publication) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var query strings.Builder
	query.WriteString(`INSERT INTO dht_peer.entries AS stored(key,value) VALUES `)
	args := make([]any, 0, len(entries)*2)
	for i, e := range entries {
		if len(e.Value) == 0 || len(e.Value) > s.MaxValueBytes {
			return errors.New("peerdht: entry exceeds value budget")
		}
		if Owner(e.Key) != s.Node {
			return errors.New("peerdht: wrong persistent owner")
		}
		if i > 0 {
			query.WriteByte(',')
		}
		fmt.Fprintf(&query, "($%d,$%d)", i*2+1, i*2+2)
		args = append(args, e.Key[:], e.Value)
	}
	query.WriteString(` ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value WHERE stored.value=EXCLUDED.value`)
	s.Writes.Add(1)
	result, err := tx.ExecContext(ctx, query.String(), args...)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != int64(len(entries)) {
		return errors.New("peerdht: conflicting finalized entry replay")
	}
	return tx.Commit()
}
