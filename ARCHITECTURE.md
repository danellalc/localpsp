# Architecture

## Shape

One binary (Go), one Docker image, one CLI. No external dependencies at runtime — state lives in embedded SQLite (in-memory by default, file-backed with `--persist`).

```
localpsp serve          the emulator: HTTP API + webhook dispatcher
localpsp trigger        fire lifecycle events at your app
localpsp chaos          fire the scenarios that hurt in production
localpsp state          inspect / reset the emulator state
```

Go because: single static binary, tiny Docker image, cross-platform CLI for free, and the webhook dispatcher is concurrency-shaped work. (Also the author's second ecosystem — see [autoseed](https://github.com/danellalc/autoseed).)

## The three layers

```
1. Provider facade      HTTP routes matching the real API, per provider
                        /asaas/v3/customers, /asaas/v3/payments, ...
2. Core engine          entities, lifecycles, state machine, clock, seed
3. Webhook dispatcher   delivery, signatures, retry policy, queue semantics
```

**The core is provider-agnostic.** A charge has a lifecycle (created → confirmed → received → overdue → refunded); providers differ in JSON shape, field names, signature scheme and event names. The facade translates; the engine owns truth. This is what makes Mercado Pago an adapter, not a rewrite — the same core/adapter discipline as autoseed and Attest.

## The hard problems

### Fidelity is the product

An emulator that diverges from the real API is worse than none — it green-lights code that breaks in production. Three defenses:

1. **Contract fixtures**: real sandbox responses (sanitized) recorded as golden files; facade responses are asserted byte-compatible in CI.
2. **The divergence ledger**: a public `FIDELITY.md` listing every known difference from the real API, honest and versioned. Stripe warns mocks "may differ in nuanced and potentially dangerous ways" — this project writes those nuances down.
3. **Provider version pinning**: the facade declares which API version it mirrors; provider API changes are tracked as issues.

### Webhook semantics, exactly right

This is where the author's fintech scar tissue becomes code:

- **Signatures**: Asaas sends `asaas-access-token` header; the emulator signs identically, so your verification code runs unmodified.
- **Delivery policy**: response status >= 200 and < 300 counts as delivered; anything else triggers the real retry schedule.
- **Queue interruption**: after 15 consecutive failures, the queue pauses — new events accumulate but are NOT delivered, exactly like production Asaas. `localpsp state` shows the paused queue; un-pausing mirrors the real reactivation flow.
- **Duplicates and ordering**: `chaos duplicate-delivery` re-sends a delivered event with the same event id (idempotency test); `chaos out-of-order` delivers CONFIRMED after RECEIVED.

### Time

Payment lifecycles are time-shaped (boleto expires in days, card settles D+32). Real waiting is useless in tests, so the engine owns a **virtual clock**:

```bash
localpsp clock advance 3d      # boleto now overdue; OVERDUE webhook fires
```

Deterministic: clock + seed fully determine event order and payloads.

### Determinism

Same seed, same everything. All ids derive from the seed (`pay_` + seeded hash), all timestamps from the virtual clock. Map iteration never affects output (the autoseed lesson, applied). A dedicated CI test runs the same scenario 100 times and asserts byte-identical webhook logs.

## Design decisions

**Why a server, not a library.** Any language, zero code changes in the app under test, and the webhook direction (emulator calls YOU) only works as a real process. localstripe validated this shape.

**Why stateful.** Stripe's own stateless mock can't test flows, and Stripe declined to add state, pointing to the community. State is the whole point: create, pay, query, verify.

**Why chaos is first-class, not an afterthought.** The API-mirroring part is table stakes (any OpenAPI mocker does 60% of it). The moat is operational behavior: queue interruption, duplicates, retry storms — knowledge that comes from operating payments in production, not from reading docs.

**Why Asaas first.** The author integrates it this month (Selar, desapega.do) — dogfooding from week one. It also has the best-documented webhook semantics to mirror.

**Why SQLite embedded.** Zero-dependency container, `--persist` for local dev sessions, `:memory:` for CI speed. State small enough that anything heavier is ceremony.

**Why MIT.** Category tools win by adoption (LocalStack, localstripe). Monetization, if ever, comes later via a pro scenario pack — not by gating the core.

## Roadmap

**v1 — Asaas, whole loop**
Customers, charges (PIX/boleto/card), subscriptions. Webhook dispatch with real signatures and retry policy. `trigger` for the full lifecycle. `chaos`: duplicate, retry-storm, queue-interrupt, out-of-order. Virtual clock. Deterministic seeds. Docker + single binary.

**v1.x**
`localpsp state` inspection UI (terminal first). Golden-file contract tests published. FIDELITY.md.

**v2**
Mercado Pago adapter (proves the facade/core split). Scenario files: declarative YAML describing a full payment saga for CI replay.

**v3**
Efí. Raw PIX (BACEN-style) if demand shows. Testcontainers module (`localpsp` as a first-class Testcontainers provider — meets .NET/Java/Go devs where they test).

**Out of scope, permanently:** real API proxying, real money, hosted service, providers outside payments, replacing homologation.
