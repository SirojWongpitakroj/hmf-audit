package canonicalingestion

import (
	"container/list"
	"sync"
)

// Memo is a byte/entry bounded optimization only; durable stores remain the
// authority. Load/Store mirror sync.Map to simplify replacing historical caches.
type Memo struct {
	mu                          sync.Mutex
	maxEntries, maxBytes, bytes int
	items                       map[string]*list.Element
	lru                         list.List
}
type memoItem struct {
	key   string
	value any
	bytes int
}

func NewMemo(entries, bytes int) *Memo {
	return &Memo{maxEntries: entries, maxBytes: bytes, items: map[string]*list.Element{}}
}
func (m *Memo) Load(key string) (any, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.items[key]
	if !ok {
		return nil, false
	}
	m.lru.MoveToFront(e)
	return e.Value.(memoItem).value, true
}
func (m *Memo) Store(key string, value any) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cost := len(key) + 128
	switch v := value.(type) {
	case string:
		cost += len(v)
	case int, int64, uint64, bool:
	default:
		return
	}
	if m.maxEntries <= 0 || cost > m.maxBytes {
		return
	}
	if old, ok := m.items[key]; ok {
		m.bytes -= old.Value.(memoItem).bytes
		m.lru.Remove(old)
		delete(m.items, key)
	}
	for len(m.items) >= m.maxEntries || m.bytes > m.maxBytes-cost {
		old := m.lru.Back()
		v := old.Value.(memoItem)
		delete(m.items, v.key)
		m.bytes -= v.bytes
		m.lru.Remove(old)
	}
	m.items[key] = m.lru.PushFront(memoItem{key, value, cost})
	m.bytes += cost
}
func (m *Memo) Usage() (int, int) { m.mu.Lock(); defer m.mu.Unlock(); return len(m.items), m.bytes }
