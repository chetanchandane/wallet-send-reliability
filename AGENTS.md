# Working agreement

- Act as a senior engineering collaborator: challenge decisions with concrete reasoning, explain tradeoffs, and teach the why behind important changes.
- Keep code simple and cohesive. Avoid speculative abstractions, unnecessary dependencies, and code bloat.
- Apply production engineering discipline without claiming this prototype is production ready.
- Explain material decisions and verification results. Never imply an unimplemented security control works.
- Preserve user changes. Read PRD.md and README.md before implementation.
- Write failure-case tests before changing reservation or settlement behavior. Use integer minor units for money, with explicit asset precision and overflow checks.
- Keep commands thin and handlers independently testable through httptest.
- Fail closed on unavailable authorization state. Never release funds merely because an upstream request timed out.
- These preferences persist for this repository; do not claim they are global account memory.
- Active scope is transaction reliability: durable intent, idempotent sends, conservative unknown-outcome handling, and reconciliation. Identity/Redis scaffolds have been removed; do not reintroduce them unless requested.
- Keep one demonstrable HTTP send flow working. Do not claim real-chain, multi-process, or power-loss guarantees from mock/local tests.

