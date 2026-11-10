# Two-minute demo guide

## Before recording

Run `make check` and `make build`. Use a terminal large enough to show all of
`make demo` output. No server or Docker setup is needed.

## Walkthrough

**0:00–0:20 — State the problem**

“A wallet send can succeed even when the caller sees a timeout. Treating that
timeout as failure can release funds incorrectly or create another payment.”

**0:20–1:00 — Run make demo**

“The mock chain accepts 20 USDC, then loses the response. The wallet marks the
outcome unknown and keeps 20 held. A new 31 USDC payment is rejected. Retrying
the original key returns the same transaction, with one chain transfer.”

**1:00–1:25 — Explain recovery**

“The demo reopens both durable stores. Reconciliation looks up the original
transaction ID and confirms it. Available funds are 30, spent funds are 20,
and nothing remains held.”

**1:25–1:45 — Show the evidence**

Open internal/wallet/service_test.go. Point to the concurrent budget test:
50 requests for 5 USDC against 50 USDC accept exactly 10. Mention the subprocess
restart, conflicting retry, and storage failure tests.

**1:45–2:00 — State the boundary**

“This is a local mock-chain prototype. The next storage step is PostgreSQL.
Real-chain integration would need persisted signed transactions and chain-specific
confirmation and rebroadcast rules.”

## Outreach message draft

Hi [Name] — I’m interested in the Wallets, Liquidity and Bridging SWE role
(P77894). I built a small Go wallet-send prototype exploring idempotency,
concurrent reservations, and recovery when a broadcast succeeds but its response
is lost. The demo and failure tests show recovery without a duplicate transfer.

Repository: [add published repository link]  
Demo: [add recording link, or omit this line]

I’d appreciate your perspective on how this relates to the reliability problems
your team works on.

## Claims to keep precise

- The demo uses HTTP handlers in-process; cmd/wallet is the live HTTP service.
- Reopening stores demonstrates persisted recovery; a separate test exits a subprocess.
- Stable mock transaction IDs model deduplication, not real signed blockchain transactions.
- Tests do not establish production readiness or exactly-once network delivery.
- Unknown outcomes can keep funds held indefinitely. This version has explicit
  reconciliation, not an automatic background recovery worker.
