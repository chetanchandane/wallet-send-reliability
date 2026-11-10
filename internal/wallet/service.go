// Package wallet implements a single-account mock send pipeline.
package wallet

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"chaingate/internal/durable"
	"chaingate/internal/money"
)

type State string

const (
	Reserved  State = "reserved"
	Submitted State = "submitted"
	Unknown   State = "unknown"
	Confirmed State = "confirmed"
	Failed    State = "failed"
)

var (
	ErrConflict     = errors.New("idempotency_conflict")
	ErrInsufficient = errors.New("insufficient_funds")
	ErrNotFound     = errors.New("not_found")
	ErrInvalid      = errors.New("invalid_request")
)

type Request struct {
	Key    string `json:"idempotency_key"`
	To     string `json:"to"`
	Amount string `json:"amount"`
}
type Transaction struct {
	ID    string `json:"id"`
	To    string `json:"to"`
	Units int64  `json:"units"`
}
type Transition struct {
	State State     `json:"state"`
	At    time.Time `json:"at"`
}
type Payment struct {
	Key     string       `json:"idempotency_key"`
	TxID    string       `json:"transaction_id"`
	To      string       `json:"to"`
	Units   int64        `json:"amount_units"`
	Asset   string       `json:"asset"`
	State   State        `json:"state"`
	History []Transition `json:"history"`
}
type Receipt struct {
	TxID  string
	State State
}

// A Failed receipt is authoritative proof of permanent no-effect rejection.
// Generic errors, missing transactions, and timeouts never imply rejection.
type Broadcaster interface {
	Broadcast(context.Context, Transaction) (Receipt, error)
	Lookup(context.Context, string) (Receipt, error)
}
type snapshot struct {
	Version  int                `json:"version"`
	Initial  int64              `json:"initial_units"`
	Payments map[string]Payment `json:"payments"`
}
type Balance struct {
	Available int64 `json:"available_units"`
	Held      int64 `json:"held_units"`
	Spent     int64 `json:"spent_units"`
}
type Service struct {
	mu    sync.Mutex
	file  *durable.File
	data  snapshot
	chain Broadcaster
}

func Open(path string, initial int64, chain Broadcaster) (*Service, error) {
	if initial <= 0 || initial > money.MaxUnits || chain == nil {
		return nil, ErrInvalid
	}
	s := &Service{chain: chain}
	f, exists, err := durable.Open(path, &s.data)
	if err != nil {
		return nil, err
	}
	s.file = f
	if !exists {
		s.data = snapshot{Version: 1, Initial: initial, Payments: map[string]Payment{}}
		err = f.Save(s.data)
	} else {
		err = s.validate(initial)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return s, nil
}
func (s *Service) validate(initial int64) error {
	if s.data.Version != 1 || s.data.Initial != initial || s.data.Payments == nil {
		return errors.New("incompatible wallet state")
	}
	var used int64
	ids := map[string]bool{}
	for key, p := range s.data.Payments {
		if key == "" || p.Key != key || p.TxID == "" || ids[p.TxID] || p.To == "" || p.Asset != "USDC" || p.Units <= 0 || p.Units > initial || len(p.History) == 0 || p.History[len(p.History)-1].State != p.State {
			return errors.New("invalid payment state")
		}
		ids[p.TxID] = true
		switch p.State {
		case Reserved, Submitted, Unknown, Confirmed:
			if p.Units > initial-used {
				return errors.New("persisted overspend")
			}
			used += p.Units
		case Failed:
		default:
			return errors.New("invalid payment status")
		}
	}
	return nil
}
func (s *Service) Close() error { s.mu.Lock(); defer s.mu.Unlock(); return s.file.Close() }
func (s *Service) balance() Balance {
	b := Balance{Available: s.data.Initial}
	for _, p := range s.data.Payments {
		switch p.State {
		case Confirmed:
			b.Spent += p.Units
		case Reserved, Submitted, Unknown:
			b.Held += p.Units
		}
	}
	b.Available -= b.Held + b.Spent
	return b
}
func (s *Service) Balance() (Balance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.file.Err(); err != nil {
		return Balance{}, err
	}
	return s.balance(), nil
}
func clone(p Payment) Payment { p.History = append([]Transition(nil), p.History...); return p }
func (s *Service) Get(key string) (Payment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.file.Err(); err != nil {
		return Payment{}, err
	}
	p, ok := s.data.Payments[key]
	if !ok {
		return Payment{}, ErrNotFound
	}
	return clone(p), nil
}
func transition(p Payment, state State) Payment {
	p = clone(p)
	p.State = state
	p.History = append(p.History, Transition{State: state, At: time.Now().UTC()})
	return p
}
func (s *Service) save(p Payment) error { s.data.Payments[p.Key] = p; return s.file.Save(s.data) }
func (s *Service) Send(ctx context.Context, r Request) (Payment, error) {
	units, err := money.ParseUSDC(r.Amount)
	if err != nil || r.Key == "" || len(r.Key) > 128 || r.To == "" || len(r.To) > 256 {
		return Payment{}, ErrInvalid
	}
	s.mu.Lock()
	if err = s.file.Err(); err != nil {
		s.mu.Unlock()
		return Payment{}, err
	}
	if old, ok := s.data.Payments[r.Key]; ok {
		s.mu.Unlock()
		if old.To != r.To || old.Units != units {
			return Payment{}, ErrConflict
		}
		return clone(old), nil
	}
	if units > s.balance().Available {
		s.mu.Unlock()
		return Payment{}, ErrInsufficient
	}
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil {
		s.mu.Unlock()
		return Payment{}, err
	}
	p := transition(Payment{Key: r.Key, TxID: hex.EncodeToString(id), To: r.To, Units: units, Asset: "USDC"}, Reserved)
	if err = s.save(p); err != nil {
		s.mu.Unlock()
		return Payment{}, err
	}
	// Persist the broadcast attempt before any external side effect.
	p = transition(p, Submitted)
	if err = s.save(p); err != nil {
		s.mu.Unlock()
		return Payment{}, err
	}
	s.mu.Unlock()
	receipt, broadcastErr := s.chain.Broadcast(ctx, Transaction{ID: p.TxID, To: p.To, Units: p.Units})
	return s.finish(r.Key, receipt, broadcastErr)
}
func (s *Service) finish(key string, r Receipt, networkErr error) (Payment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.file.Err(); err != nil {
		return Payment{}, err
	}
	p := s.data.Payments[key]
	if p.State == Confirmed || p.State == Failed {
		return clone(p), nil
	}
	state := Unknown
	if networkErr == nil && r.TxID == p.TxID {
		switch r.State {
		case Confirmed, Failed, Submitted:
			state = r.State
		}
	}
	if p.State != state {
		p = transition(p, state)
		if err := s.save(p); err != nil {
			return Payment{}, err
		}
	}
	return clone(p), nil
}

// Reconcile looks up the original transaction. Absence never releases a hold.
// A reserved intent can safely begin its first broadcast; submitted/unknown
// payments require positive evidence from lookup and are not re-created.
func (s *Service) Reconcile(ctx context.Context, key string) (Payment, error) {
	s.mu.Lock()
	if err := s.file.Err(); err != nil {
		s.mu.Unlock()
		return Payment{}, err
	}
	p, ok := s.data.Payments[key]
	if !ok {
		s.mu.Unlock()
		return Payment{}, ErrNotFound
	}
	if p.State == Confirmed || p.State == Failed {
		s.mu.Unlock()
		return clone(p), nil
	}
	first := p.State == Reserved
	if first {
		p = transition(p, Submitted)
		if err := s.save(p); err != nil {
			s.mu.Unlock()
			return Payment{}, err
		}
	}
	s.mu.Unlock()
	var r Receipt
	var err error
	if first {
		r, err = s.chain.Broadcast(ctx, Transaction{ID: p.TxID, To: p.To, Units: p.Units})
	} else {
		r, err = s.chain.Lookup(ctx, p.TxID)
	}
	return s.finish(key, r, err)
}
