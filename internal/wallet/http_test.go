package wallet

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestHTTPPaymentFlow(t *testing.T) {
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
	handler := Handler(s)
	request := func(path, body string, status int) Payment {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		res := recorder.Result()
		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != status {
			t.Fatalf("%s: status %d, body %s", path, res.StatusCode, data)
		}
		var p Payment
		if status == 200 || status == 202 {
			if err := json.Unmarshal(data, &p); err != nil {
				t.Fatal(err)
			}
		}
		return p
	}
	chain.LoseNextResponse()
	p := request("/payments", `{"idempotency_key":"p1","to":"alice","amount":"20"}`, 202)
	if p.State != Unknown {
		t.Fatal(p.State)
	}
	retry := request("/payments", `{"idempotency_key":"p1","to":"alice","amount":"20.00"}`, 202)
	if retry.TxID != p.TxID {
		t.Fatal("retry changed transaction")
	}
	p = request("/payments/p1/reconcile", "", 200)
	if p.State != Confirmed {
		t.Fatal(p.State)
	}
	request("/payments", `{"idempotency_key":"p1","to":"bob","amount":"20"}`, 409)
	request("/payments", `{"idempotency_key":"p1","to":"alice","amount":"21"}`, 409)
	request("/payments", `{"idempotency_key":"p2","to":"alice","amount":"31"}`, 402)
	request("/payments", `{"idempotency_key":"p2","to":"alice","amount":5}`, 400)
	request("/payments", `{"idempotency_key":"p2","to":"alice","amount":"5","user":"bob"}`, 400)
	request("/payments", `{"idempotency_key":"p2","to":"alice","amount":"5"} {}`, 400)
	if chain.Count() != 1 {
		t.Fatal("unexpected ledger count")
	}
}
