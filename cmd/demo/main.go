package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"chaingate/internal/wallet"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	dir, err := os.MkdirTemp("", "chaingate-demo-")
	if err != nil {
		return err
	}
	fmt.Println("Chaingate: reliable mock wallet sends")
	fmt.Println("Durable evidence:", dir)
	chain, err := wallet.OpenMock(filepath.Join(dir, "chain.json"))
	if err != nil {
		return err
	}
	defer func() {
		if chain != nil {
			_ = chain.Close()
		}
	}()
	s, err := wallet.Open(filepath.Join(dir, "wallet.json"), 50_000_000, chain)
	if err != nil {
		return err
	}
	defer func() {
		if s != nil {
			_ = s.Close()
		}
	}()
	handler := wallet.Handler(s)
	call := func(path, body string, want int) (wallet.Payment, error) {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		res := recorder.Result()
		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		if err != nil {
			return wallet.Payment{}, err
		}
		if res.StatusCode != want {
			return wallet.Payment{}, fmt.Errorf("expected HTTP %d, got %d: %s", want, res.StatusCode, data)
		}
		var p wallet.Payment
		if want == 200 || want == 202 {
			err = json.Unmarshal(data, &p)
		}
		return p, err
	}
	body := `{"idempotency_key":"outreach-payment","to":"alice","amount":"20.00"}`
	chain.LoseNextResponse()
	p, err := call("/payments", body, 202)
	if err != nil {
		return err
	}
	if p.State != wallet.Unknown {
		return fmt.Errorf("expected unknown outcome, got %s", p.State)
	}
	tx := p.TxID
	fmt.Printf("1. Chain accepted 20 USDC; response lost -> %s (tx %s)\n", p.State, tx)
	b, err := s.Balance()
	if err != nil {
		return err
	}
	if b.Held != 20_000_000 || b.Available != 30_000_000 {
		return fmt.Errorf("unsafe balance: %+v", b)
	}
	if _, err = call("/payments", `{"idempotency_key":"overspend","to":"alice","amount":"31"}`, 402); err != nil {
		return err
	}
	fmt.Println("2. Funds remain held; a 31 USDC send is rejected (HTTP 402)")
	p, err = call("/payments", body, 202)
	if err != nil {
		return err
	}
	if p.TxID != tx || chain.Count() != 1 {
		return fmt.Errorf("duplicate payment")
	}
	fmt.Println("3. Retry returns the same transaction; chain ledger entries: 1")
	if err = s.Close(); err != nil {
		return err
	}
	if err = chain.Close(); err != nil {
		return err
	}
	chain, err = wallet.OpenMock(filepath.Join(dir, "chain.json"))
	if err != nil {
		return err
	}
	s, err = wallet.Open(filepath.Join(dir, "wallet.json"), 50_000_000, chain)
	if err != nil {
		return err
	}
	handler = wallet.Handler(s)
	p, err = call("/payments/outreach-payment/reconcile", "", 200)
	if err != nil {
		return err
	}
	if p.State != wallet.Confirmed || p.TxID != tx || chain.Count() != 1 {
		return fmt.Errorf("recovery invariant failed")
	}
	fmt.Println("4. Reopened wallet and chain stores; lookup confirms the original transaction")
	if _, err = call("/payments", `{"idempotency_key":"outreach-payment","to":"bob","amount":"20"}`, 409); err != nil {
		return err
	}
	fmt.Println("5. Same key with a different recipient rejected (HTTP 409)")
	b, err = s.Balance()
	if err != nil {
		return err
	}
	if b.Spent != 20_000_000 || b.Held != 0 || b.Available != 30_000_000 {
		return fmt.Errorf("incorrect settlement")
	}
	fmt.Println("PASS: spent 20 USDC, held 0, available 30; exactly one chain transfer")
	return nil
}
