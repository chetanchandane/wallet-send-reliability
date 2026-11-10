package wallet

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

func TestProcessRestart(t *testing.T) {
	if dir := os.Getenv("CHAINGATE_CRASH_TEST_DIR"); dir != "" {
		chain, err := OpenMock(filepath.Join(dir, "chain.json"))
		if err != nil {
			t.Fatal(err)
		}
		s, err := Open(filepath.Join(dir, "wallet.json"), 50_000_000, chain)
		if err != nil {
			t.Fatal(err)
		}
		chain.LoseNextResponse()
		p, err := s.Send(context.Background(), Request{Key: "crash", To: "alice", Amount: "20"})
		if err != nil || p.State != Unknown {
			t.Fatalf("send: %v %v", p, err)
		}
		// Exit without closing stores: OS releases locks; fsynced state survives.
		os.Exit(0)
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcessRestart$")
	cmd.Env = append(os.Environ(), "CHAINGATE_CRASH_TEST_DIR="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %s %v", out, err)
	}
	chain, err := OpenMock(filepath.Join(dir, "chain.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	s, err := Open(filepath.Join(dir, "wallet.json"), 50_000_000, chain)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.Reconcile(context.Background(), "crash")
	if err != nil || p.State != Confirmed || chain.Count() != 1 {
		t.Fatalf("recovery: %+v %v", p, err)
	}
}

func TestDefinitiveRejectionReleasesExactlyOnce(t *testing.T) {
	dir := t.TempDir()
	chain, err := OpenMock(filepath.Join(dir, "chain.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	s, err := Open(filepath.Join(dir, "wallet.json"), 50_000_000, chain)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.Send(context.Background(), Request{Key: "rejected", To: "reject", Amount: "50"})
	if err != nil || p.State != Failed {
		t.Fatalf("reject: %+v %v", p, err)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.Reconcile(context.Background(), "rejected"); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := s.Balance()
	if b.Available != 50_000_000 || b.Held != 0 || b.Spent != 0 || chain.Count() != 0 {
		t.Fatalf("balance: %+v", b)
	}
}

func TestStorageFailurePreventsBroadcast(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wallet.json")
	chain, err := OpenMock(filepath.Join(dir, "chain.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	s, err := Open(path, 50_000_000, chain)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := os.Rename(path, path+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := s.Send(context.Background(), Request{Key: fmtKey(i), To: "alice", Amount: "5"}); err == nil {
			t.Fatal("storage failure hidden")
		}
	}
	if chain.Count() != 0 {
		t.Fatal("broadcast despite failed persistence")
	}
}

func fmtKey(i int) string { return fmt.Sprintf("payment-%d", i) }

func TestRecoverReservedIntentAndPreserveInitialBalance(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wallet.json")
	chain, err := OpenMock(filepath.Join(dir, "chain.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	s, err := Open(path, 50_000_000, chain)
	if err != nil {
		t.Fatal(err)
	}
	// Model a crash at the durable boundary before submission was recorded.
	p := transition(Payment{Key: "reserved", TxID: "stable", To: "alice", Units: 5_000_000, Asset: "USDC"}, Reserved)
	if err := s.save(p); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if changed, err := Open(path, 100_000_000, chain); err == nil {
		changed.Close()
		t.Fatal("balance reset allowed")
	}
	s, err = Open(path, 50_000_000, chain)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err = s.Reconcile(context.Background(), "reserved")
	if err != nil || p.TxID != "stable" || p.State != Submitted {
		t.Fatalf("resume: %+v %v", p, err)
	}
	p, err = s.Reconcile(context.Background(), "reserved")
	if err != nil || p.State != Confirmed || chain.Count() != 1 {
		t.Fatalf("confirm: %+v %v", p, err)
	}
}

type mismatchedChain struct{}

type interruptedBroadcaster struct {
	chain     *Mock
	interrupt func()
}

func (b interruptedBroadcaster) Broadcast(ctx context.Context, tx Transaction) (Receipt, error) {
	r, err := b.chain.Broadcast(ctx, tx)
	b.interrupt()
	return r, err
}
func (b interruptedBroadcaster) Lookup(ctx context.Context, id string) (Receipt, error) {
	return b.chain.Lookup(ctx, id)
}

func TestInterruptionAfterBroadcastBeforeSettlement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wallet.json")
	chain, err := OpenMock(filepath.Join(dir, "chain.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	var s *Service
	adapter := interruptedBroadcaster{chain: chain, interrupt: func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	}}
	s, err = Open(path, 50_000_000, adapter)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(context.Background(), Request{Key: "interrupt", To: "alice", Amount: "20"}); err == nil {
		t.Fatal("closed store acknowledged settlement")
	}
	s, err = Open(path, 50_000_000, chain)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b, _ := s.Balance()
	if b.Held != 20_000_000 {
		t.Fatal("lost reservation")
	}
	p, err := s.Reconcile(context.Background(), "interrupt")
	if err != nil || p.State != Confirmed || chain.Count() != 1 {
		t.Fatalf("recover: %+v %v", p, err)
	}
}
func (mismatchedChain) Broadcast(context.Context, Transaction) (Receipt, error) {
	return Receipt{TxID: "wrong", State: Failed}, nil
}
func (mismatchedChain) Lookup(context.Context, string) (Receipt, error) {
	return Receipt{TxID: "wrong", State: Confirmed}, nil
}

func TestMismatchedReceiptCannotSettle(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "wallet.json"), 50_000_000, mismatchedChain{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.Send(context.Background(), Request{Key: "k", To: "alice", Amount: "50"})
	if err != nil || p.State != Unknown {
		t.Fatalf("send: %+v %v", p, err)
	}
	p, err = s.Reconcile(context.Background(), "k")
	if err != nil || p.State != Unknown {
		t.Fatalf("reconcile: %+v %v", p, err)
	}
	b, _ := s.Balance()
	if b.Held != 50_000_000 || b.Spent != 0 || b.Available != 0 {
		t.Fatal("mismatched receipt affected accounting")
	}
}

func TestLostResponseRestartAndReconcile(t *testing.T) {
	dir := t.TempDir()
	chain, err := OpenMock(filepath.Join(dir, "chain.json"))
	if err != nil {
		t.Fatal(err)
	}
	chain.LoseNextResponse()
	s, err := Open(filepath.Join(dir, "wallet.json"), 50_000_000, chain)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Send(context.Background(), Request{Key: "payment-1", To: "alice", Amount: "20.00"})
	if err != nil || p.State != Unknown {
		t.Fatalf("send: %+v %v", p, err)
	}
	b, _ := s.Balance()
	if b.Available != 30_000_000 || b.Held != 20_000_000 {
		t.Fatalf("funds released: %+v", b)
	}
	retry, err := s.Send(context.Background(), Request{Key: "payment-1", To: "alice", Amount: "20"})
	if err != nil || retry.TxID != p.TxID || chain.Count() != 1 {
		t.Fatal("retry duplicated payment")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := chain.Close(); err != nil {
		t.Fatal(err)
	}
	chain, err = OpenMock(filepath.Join(dir, "chain.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	s, err = Open(filepath.Join(dir, "wallet.json"), 50_000_000, chain)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err = s.Reconcile(context.Background(), "payment-1")
	if err != nil || p.State != Confirmed || chain.Count() != 1 {
		t.Fatalf("reconcile: %+v %v", p, err)
	}
	p, err = s.Reconcile(context.Background(), "payment-1")
	if err != nil || p.State != Confirmed {
		t.Fatal(err)
	}
	b, _ = s.Balance()
	if b.Spent != 20_000_000 || b.Held != 0 || b.Available != 30_000_000 {
		t.Fatalf("settlement: %+v", b)
	}
}

func TestConcurrentBudgetAndConflicts(t *testing.T) {
	dir := t.TempDir()
	chain, _ := OpenMock(filepath.Join(dir, "chain.json"))
	defer chain.Close()
	s, err := Open(filepath.Join(dir, "wallet.json"), 50_000_000, chain)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.Send(context.Background(), Request{Key: fmtKey(i), To: "alice", Amount: "5"})
			if err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			} else if !errors.Is(err, ErrInsufficient) {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if successes != 10 || chain.Count() != 10 {
		t.Fatalf("successes=%d ledger=%d", successes, chain.Count())
	}
	b, _ := s.Balance()
	if b.Available != 0 || b.Held != 50_000_000 {
		t.Fatalf("balance=%+v", b)
	}
}

func TestDuplicateConflictAndExclusiveWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wallet.json")
	chain, _ := OpenMock(filepath.Join(dir, "chain.json"))
	defer chain.Close()
	s, _ := Open(path, 50_000_000, chain)
	defer s.Close()
	if other, err := Open(path, 50_000_000, chain); err == nil {
		other.Close()
		t.Fatal("second writer accepted")
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Send(context.Background(), Request{Key: "same", To: "alice", Amount: "5"})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if chain.Count() != 1 {
		t.Fatal("duplicate broadcast")
	}
	_, err := s.Send(context.Background(), Request{Key: "same", To: "bob", Amount: "5"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict: %v", err)
	}
}

type absentChain struct{}

func (absentChain) Broadcast(context.Context, Transaction) (Receipt, error) {
	return Receipt{}, errors.New("network unavailable")
}
func (absentChain) Lookup(context.Context, string) (Receipt, error) { return Receipt{}, ErrNotFound }

func TestAbsentTransactionNeverReleasesFunds(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "wallet.json"), 50_000_000, absentChain{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.Send(context.Background(), Request{Key: "k", To: "alice", Amount: "50"})
	if err != nil || p.State != Unknown {
		t.Fatal(err)
	}
	p, err = s.Reconcile(context.Background(), "k")
	if err != nil || p.State != Unknown {
		t.Fatal(err)
	}
	b, _ := s.Balance()
	if b.Held != 50_000_000 || b.Available != 0 {
		t.Fatal("unknown funds released")
	}
}
