# Chaingate — Reliable Wallet Sends

A Go prototype showing how a wallet service handles **a successful transfer whose
broadcast response is lost**, without duplicating the payment or refunding its
reserved funds.

Mock USDC only; no private keys, real blockchain, or real money.

## Run the demonstration

Requires Go 1.22+ on macOS or Linux. Uses only the Go standard library.
No dependency downloads, Docker, Redis, or credentials are needed.

```sh
make demo
make check
make race
make build
```

The demo calls the real HTTP handlers in-process and checks:

1. A 20 USDC payment is accepted by the mock chain, but the response times out.
2. Its funds remain held; a 31 USDC send against the remaining 30 is rejected.
3. A retry returns the original transaction; there is one ledger transfer.
4. Reopening both stores and reconciling confirms that same transaction.
5. Changing the recipient under the original key returns conflict.

Expected ending:

```text
PASS: spent 20 USDC, held 0, available 30; exactly one chain transfer
```

Each run prints a fresh temporary evidence directory containing wallet.json and
chain.json. Files are retained for inspection; nothing touches real funds.

## Run the HTTP service

```sh
go run ./cmd/wallet -addr 127.0.0.1:8081 -data .local/wallet
```

In a second terminal:

```sh
curl -s http://127.0.0.1:8081/payments \
  -H 'Content-Type: application/json' \
  -d '{"idempotency_key":"payment-1","to":"alice","amount":"20.00"}'
curl -s -X POST http://127.0.0.1:8081/payments/payment-1/reconcile
curl -s http://127.0.0.1:8081/balance
```

The first send normally returns 202 submitted. Reconciliation returns 200
confirmed. Repeating the same payment key never creates a second payment.
Reusing it with a different recipient/amount returns 409. The initial balance is
50 USDC, persisted across restarts. Use a new data directory for a fresh account.

This is a single-account local API with no authentication. The command refuses
non-loopback bindings. Amounts are decimal strings; balance fields are integer
USDC millionths. The special mock recipient "reject" returns permanent rejection.
Lost-response injection is available to the demo/tests, not exposed as an HTTP endpoint.

## Why the design works

```text
HTTP API -> wallet state machine -> atomic wallet snapshot
                    |
                    v
             mock broadcaster -> separate durable mock-chain ledger
                    ^
                    |
           lookup by original transaction ID
```

Payment intent, reservation, and stable transaction identity are saved before
broadcasting. The external operation happens outside the service mutex. A
timeout is an unknown outcome, so funds remain held. Reconciliation asks about
the original transaction instead of constructing a replacement.

Idempotency keys identify requests; transaction IDs identify mock-chain effects.
Both layers deduplicate, and terminal states cannot settle twice.

The wallet derives available/held/spent balances from its durable payment states.
Snapshot writes use file sync, atomic rename, and directory sync, with an
exclusive process lock. Storage errors stop subsequent operations on that handle.
The wallet snapshot is the accounting authority.

## Code map

| Location | Responsibility |
|---|---|
| cmd/demo | Self-checking lost-response demonstration |
| cmd/wallet | Loopback HTTP service |
| internal/wallet/service.go | Reservation, idempotency, and reconciliation |
| internal/wallet/mock.go | Persistent simulated chain receipts |
| internal/wallet/http.go | HTTP validation and responses |
| internal/durable | Snapshot replacement, sync, and exclusive writer lock |
| internal/money | Exact USDC parsing |
| internal/server | HTTP timeouts and graceful shutdown |

Start with the demo, then read service.go alongside service_test.go.
The [PRD](PRD.md) specifies the state machine and failure contracts.
The [demo guide](docs/demo.md) provides a two-minute walkthrough.

## Tests

[GitHub Actions CI](.github/workflows/ci.yml) runs on pushes and pull requests,
with a manual trigger available. It checks formatting, vet, race-enabled tests,
build, and the self-checking demo on Linux with Go 1.22.x and the current stable
Go release. The stable job also repeats wallet tests 20 times. The workflow
uses read-only repository permissions and needs no secrets or external services.
Its first hosted result will be available after the workflow is pushed.

| Test | Evidence |
|---|---|
| TestConcurrentBudgetAndConflicts | Exactly 10 of 50 concurrent 5-USDC sends fit a 50-USDC balance |
| TestDuplicateConflictAndExclusiveWriter | Concurrent retries produce one transfer; conflicting payload and second writer rejected |
| TestLostResponseRestartAndReconcile | Lost response, persisted hold, reopen, reconciliation, no double settlement |
| TestProcessRestart | Subprocess exits without closing stores; parent recovers the payment |
| TestAbsentTransactionNeverReleasesFunds | Missing lookup does not refund an uncertain payment |
| TestDefinitiveRejectionReleasesExactlyOnce | Permanent rejection restores availability once |
| TestStorageFailurePreventsBroadcast | Failed reservation persistence prevents broadcast |
| TestInterruptionAfterBroadcastBeforeSettlement | Reopening recovers a transfer whose local settlement was interrupted |
| TestRecoverReservedIntentAndPreserveInitialBalance | A pre-broadcast intent resumes; restart cannot reseed the balance |
| TestMismatchedReceiptCannotSettle | A receipt for another transaction cannot change accounting |
| TestHTTPPaymentFlow | HTTP statuses, conflict, insufficient balance, and strict body validation |
| Durable-store tests | Corrupt snapshots fail closed; lock and replacement persist state |

`make race` repeats wallet tests 20 times with the Go race detector.
Tests use handler recorders and temporary directories, so no TCP listener is
required. The command's real network listener is separate from these tests.

Local validation: build, vet, race-enabled tests, and the demo passed. The
concurrent budget, duplicate-request, lost-response, and process-restart tests
also passed 20 repeated race-enabled runs. The author manually verified the
live loopback send, reconciliation, and balance endpoints: the same transaction
became confirmed, with 30 USDC available, zero held, and 20 spent. Automated
HTTP tests use handler recorders because the development sandbox restricts sockets.

## Scope and limitations

This is a local prototype, not production payment infrastructure:

- Snapshots scan/rewrite the entire account and support one cooperating process
  on a local Unix filesystem. PostgreSQL is the next step for multi-replica use.
- The mock chain uses a random stable ID and trusted simulated receipts, not
  signed blockchain transactions, gas, nonces, finality, or reorganizations.
- Unknown outcomes keep funds held. Reconciliation is explicit; there is no
  background worker, reservation expiry, or automatic rebroadcast.
- A crash between recording submission and actually broadcasting can leave an
  unresolved hold. This favors safety over automatic recovery.
- Tests cover process exit and injected storage/network failures, not power loss.
- Local state/history is not tamper-evident; authentication and real custody
  are not implemented.
- Service liveness does not imply storage readiness.

## Outreach description

> I built a Go wallet-send prototype focused on idempotency, concurrent balance
> reservations, and recovery from ambiguous broadcast outcomes. Its demo drops
> a response after the mock chain accepts a transfer, then recovers by the
> original transaction ID without creating another payment. Failure tests cover
> concurrency, process restart, storage errors, and conflicting retries.

