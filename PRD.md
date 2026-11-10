# PRD: Chaingate — Reliable Wallet Sends

Author: Chetan Chandane  
Status: First demonstrable flow implemented; validation results live in README  
Audience: Coinbase Wallets, Liquidity and Bridging engineering outreach

## 1. Objective

Build a small Go wallet-send prototype that demonstrates concurrency-safe spending,
idempotent payment requests, durable transaction intent, and recovery from ambiguous
broadcast outcomes. The target is a clear, reproducible engineering demonstration,
not a production wallet or an identity platform.

The core question: **If a transaction succeeds but its response is lost, can we
recover without creating a second payment or releasing funds incorrectly?**

## 2. Scope

One seeded custodial account starts with 50 mock USDC. A local HTTP API accepts
send requests. The service reserves funds, persists a stable transaction identity,
and calls an independently persisted mock blockchain adapter. Reconciliation
looks up that same transaction and records its outcome.

Included:
- Go service, exact integer money, strict input validation.
- Durable payment intent and state transitions.
- Idempotency key bound to recipient and canonical amount.
- Atomic concurrent balance reservations within one process.
- Unknown-outcome handling and explicit reconciliation.
- Mock ledger with persistent transaction deduplication.
- Runnable demo and failure-case tests.

Deferred:
- Agent registration, delegation, JWT enforcement, JWKS, and Redis accounting.
- Real transaction construction/signing, private-key custody, gas and nonces.
- Real blockchains, chain reorganizations, bridges, liquidity routing, and fees.
- Multi-user authentication and authorization.
- PostgreSQL, multiple replicas, workers, automatic reconciliation, HA.
- Kafka, OPA, distributed tracing, UI, and performance targets.

Earlier identity experiments have been removed from the active repository.
This project does not claim to implement the team's entire wallet infrastructure.

## 3. Demonstration

`make demo` must:
1. Create fresh wallet and mock-chain stores with 50 USDC.
2. Send 20 USDC; have the chain persist acceptance but return a timeout.
3. Show unknown status, 20 held, and 30 available.
4. Reject a new 31 USDC payment.
5. Retry the original key and return the same transaction, with one ledger transfer.
6. Close and reopen both stores, then reconcile by transaction ID.
7. Show confirmed status, 20 spent, zero held, and 30 available.
8. Reject the original key with a changed recipient.
9. Exit nonzero if any assertion fails.

The demo exercises HTTP handlers in-process without opening sockets. A separate
wallet command exposes the same handlers on loopback. Process-exit recovery is
tested separately; the demo does not claim to simulate a power outage.

## 4. Architecture and storage

```text
HTTP request -> wallet service -> durable wallet snapshot
                       |
                       v
               mock chain adapter -> independent durable mock ledger
                       ^
                       |
            reconciliation by transaction ID
```

The local snapshot store serializes updates under a service mutex, writes a
temporary JSON file, fsyncs it, atomically renames it, then fsyncs the directory.
An exclusive advisory file lock rejects another cooperating writer. A failed
write poisons the open store: no further requests may trust its in-memory state.
Existing corrupt or incompatible state fails closed and is never reset silently.

Funds and payment intent live in one snapshot. Balances are derived from payment
states, avoiding a separate counter that could diverge from payment records.
Network calls occur outside the service mutex.

This deliberately bounded implementation rewrites the snapshot and scans payments;
it is not suitable for large ledgers. PostgreSQL with transactional reservation,
a unique account/idempotency constraint, and durable job scheduling is the next
step for scaling. Redis is not a second accounting authority.

Supported environment: local Unix filesystem on macOS/Linux. Filesystem durability
depends on fsync/rename semantics and the storage device. No Windows, network
filesystem, multi-region, power-loss, or database-grade durability claim.

## 5. API

The command binds to a loopback IP only. There is no authentication; use mock funds
on a trusted local machine. Client-provided user/grant fields are rejected.

| Endpoint | Behavior |
|---|---|
| POST /payments | Validate, reserve, persist intent, broadcast |
| GET /payments/{key} | Return payment and transition history |
| POST /payments/{key}/reconcile | Look up the original transaction |
| GET /balance | Available, held, spent in integer USDC millionths |
| GET /healthz | Process liveness, not durable-store readiness |

Request:
```json
{"idempotency_key":"payment-1","to":"alice","amount":"20.00"}
```

USDC is the sole asset. Amounts have at most six decimal places, are positive,
and are bounded to the existing exact-integer money range. JSON numbers for
amounts, unknown fields, trailing JSON, and oversized bodies are rejected.

Responses:
- 202: reserved/submitted/unknown; payment is not yet confirmed.
- 200: confirmed, or successful GET.
- 422: permanent, authoritative rejection.
- 400: malformed request.
- 409: idempotency key reused with different payment details.
- 402: insufficient available funds.
- 404: unknown payment.
- 503: storage unavailable; caller must inspect/retry the same key because a
  failure after broadcast can mean the transaction already happened.

## 6. Payment identity and state machine

An idempotency key identifies the API-level payment request. A transaction ID
identifies its persisted mock-chain transaction. They are distinct: the service
generates the transaction ID once and stores it before any external side effect.

The mock ID is a random identifier, not a blockchain transaction hash or signed
transaction. The immutable tuple (ID, recipient, amount) models transaction
identity for this demonstration. A real adapter must persist signed transaction
bytes and use chain-specific identity/rebroadcast rules.

| State | Meaning | Accounting |
|---|---|---|
| reserved | Durable intent, no broadcast attempt recorded yet | held |
| submitted | Broadcast attempt recorded; acceptance may be uncertain | held |
| unknown | Transport failed, lookup absent, or response unusable | held |
| confirmed | Adapter reports confirmed transfer | spent |
| failed | Adapter reports permanent rejection with no transfer | available |

Persist reserved before submitted; persist submitted before calling the adapter.
Timeouts, missing lookup results, malformed receipts, and generic errors never
release funds. Only a matching authoritative receipt may confirm or fail a
payment. Terminal states cannot be reversed or settled twice.

Reconciliation:
- Confirmed/failed: return the stored outcome.
- Reserved: safely initiate the first broadcast after persisting submitted.
- Submitted/unknown: query the original transaction ID.
- Missing/ambiguous lookup: remain unknown with funds held.

A crash after persisting submitted but before broadcasting can leave a hold that
lookup cannot resolve. Automatic rebroadcast is intentionally deferred rather
than inventing a new transaction or refunding on a timer. No reservation TTL.

## 7. Idempotency and concurrency

- Normalize amounts to integer units before comparing requests.
- Same key, recipient, and amount returns the current stored payment without a
  second broadcast. Status can advance after reconciliation; immutable payment
  identity remains unchanged.
- Same key with changed recipient or amount returns conflict in every state.
- Concurrent duplicates create one reservation and one broadcast attempt.
- The mock chain deduplicates identical transaction IDs and rejects changed
  transaction content under an existing ID.
- With a 50 USDC account, 50 simultaneous distinct sends of 5 USDC accept exactly
  10 and never make available funds negative.
- A restart loads existing balances and records; it never reseeds spent funds.
- Initial balance changes against an existing store are rejected.

## 8. Traceability and failure behavior

Each payment persists its state-transition history with timestamps in the same
snapshot as the payment itself. It exposes the API key and transaction ID for
debugging. This is not a tamper-evident audit ledger.

Storage failure before broadcasting prevents an external call. Storage failure
after broadcasting returns unavailable and requires reopening/reconciliation.
No claim of exactly-once network delivery is made: the demonstrated property is
one mock-chain transfer per stable transaction identity, with idempotent requests.

Mock receipts are trusted for this simulation. Real confirmation depth, chain
reorganizations, finality, and receipt authentication remain out of scope.

## 9. Verification and release gate

Required checks:
- Strict amount parsing and input validation.
- Concurrent overspend: exactly 10 of 50 accepted.
- Concurrent duplicate requests: one mock-chain transfer.
- Conflicting recipient/amount: 409.
- Lost broadcast response: hold retained, one transfer, reconciliation confirms.
- Repeated reconciliation: no double settlement.
- Absent lookup: funds stay held.
- Authoritative rejection: release once.
- Store write failure: no broadcast before persistence.
- Reopen and subprocess exit: persisted state recovers.
- Corrupt storage and second writer: fail closed.
- HTTP flow and demo assertions.
- Race-enabled tests, vet, and build.

Run `make check`, `make race`, `make build`, and `make demo`.
No passing test count is inferred from empty legacy attack packages.

## 10. Outreach acceptance criteria

- A fresh checkout with Go runs the demo without third-party modules, Redis,
  Docker, external credentials, or real funds.
- The demo prints the lost-response recovery story and checks its own result.
- The test suite provides concrete evidence for the invariants above.
- README gives commands, expected behavior, architecture, and honest limitations.
- No placeholder command is described as a completed feature.
- The pitch claims only a local mock-wallet reliability prototype.

## 11. Next milestones

1. Complete and verify this local flow.
2. Replace snapshot storage with PostgreSQL transactions and unique constraints.
3. Add a durable reconciliation worker and controlled retry/rebroadcast policy.
4. Add a testnet adapter with persisted signed transactions and finality handling.
5. Add authentication, authorization, and operational telemetry as required.

These are future milestones, not prerequisites for showing the current demo.
