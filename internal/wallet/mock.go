package wallet

import (
	"chaingate/internal/durable"
	"context"
	"errors"
	"sync"
)

type mockEntry struct {
	Transaction Transaction
	State       State
}
type Mock struct {
	mu       sync.Mutex
	file     *durable.File
	entries  map[string]mockEntry
	loseNext bool
}

func OpenMock(path string) (*Mock, error) {
	m := &Mock{}
	f, exists, err := durable.Open(path, &m.entries)
	if err != nil {
		return nil, err
	}
	m.file = f
	if !exists {
		m.entries = map[string]mockEntry{}
		err = f.Save(m.entries)
	}
	if m.entries == nil {
		err = errors.New("invalid mock ledger")
	}
	for id, e := range m.entries {
		if id == "" || e.Transaction.ID != id || e.Transaction.Units <= 0 || e.Transaction.To == "" || (e.State != Confirmed && e.State != Failed) {
			err = errors.New("invalid mock receipt")
		}
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return m, nil
}
func (m *Mock) Close() error      { m.mu.Lock(); defer m.mu.Unlock(); return m.file.Close() }
func (m *Mock) LoseNextResponse() { m.mu.Lock(); defer m.mu.Unlock(); m.loseNext = true }
func (m *Mock) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, e := range m.entries {
		if e.State == Confirmed {
			n++
		}
	}
	return n
}
func (m *Mock) Broadcast(ctx context.Context, tx Transaction) (Receipt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	if err := m.file.Err(); err != nil {
		return Receipt{}, err
	}
	if existing, ok := m.entries[tx.ID]; ok {
		if existing.Transaction != tx {
			return Receipt{}, ErrConflict
		}
		return Receipt{TxID: tx.ID, State: existing.State}, nil
	}
	state := Confirmed
	// A deterministic, terminal no-effect rejection for failure tests/demo.
	if tx.To == "reject" {
		state = Failed
	}
	m.entries[tx.ID] = mockEntry{Transaction: tx, State: state}
	if err := m.file.Save(m.entries); err != nil {
		return Receipt{}, err
	}
	if m.loseNext {
		m.loseNext = false
		return Receipt{}, context.DeadlineExceeded
	}
	if state == Failed {
		return Receipt{TxID: tx.ID, State: Failed}, nil
	}
	return Receipt{TxID: tx.ID, State: Submitted}, nil
}
func (m *Mock) Lookup(ctx context.Context, id string) (Receipt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	if err := m.file.Err(); err != nil {
		return Receipt{}, err
	}
	e, ok := m.entries[id]
	if !ok {
		return Receipt{}, ErrNotFound
	}
	return Receipt{TxID: id, State: e.State}, nil
}
