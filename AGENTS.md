# AGENTS.md

Guidance for AI coding agents working in this repository.

If you are consuming this tool rather than developing it, read [llms.txt](llms.txt) instead.

## What this project is

A stateful local emulator of Brazilian payment providers (Asaas first), in the mold of localstripe and LocalStack. Apps point their PSP base URL at it and run unmodified: same endpoints, same payloads, same webhook signatures, same delivery semantics. The differentiator is operational fidelity: the behaviors that only break in production (duplicate deliveries, retry storms, Asaas's queue interruption after 15 consecutive failures) are first-class, triggerable scenarios.

## Golden rules (these win over everything below)

1. Never mention Claude, AI, "Co-Authored-By" or assisted generation anywhere: commits, PRs, issues, release notes, code, docs.
2. Human, friendly, plain language in all text. No inflated words (robust, seamless, leverage, delve, comprehensive).
3. No em dashes anywhere. Commas, colons, parentheses or a new sentence.
4. No code comments. Descriptive names instead. Exception: godoc on exported identifiers, kept to one or two dry sentences.
5. Commits look like a dev wrote them: short, direct, conventional, English, small and frequent.

## Build and test

```bash
go build ./...
go test ./...              # everything
go test ./... -short       # skips anything needing network fixtures
go test -run TestQueueInterrupt ./...
go vet ./... && golangci-lint run
```

## The three layers

1. `engine`: entities, lifecycles (state machine), virtual clock, seeded ids. Knows NO provider.
2. `dispatch`: webhook delivery, signatures, retry policy, queue semantics.
3. `providers/asaas`: HTTP facade translating the real API surface to engine calls.

New code belongs to exactly one layer. Provider-specific JSON, field names, event names and signature schemes live ONLY in the facade.

## Hard rules

**Fidelity is the product.** Facade responses are asserted against golden files (sanitized real sandbox responses) in CI. Never invent a field, event or behavior: if the provider's docs don't state it and the sandbox doesn't show it, it does not exist here. When in doubt, test against the real sandbox and record a golden. Every known divergence goes in FIDELITY.md, public and versioned.

**Webhook semantics are sacred.** Delivery succeeds only on HTTP 2xx. Failures follow the provider's real retry schedule. After 15 consecutive failures the queue PAUSES: new events accumulate undelivered, exactly like production Asaas. Duplicate delivery re-sends the SAME event id. Each of these behaviors has a dedicated test.

**Determinism.** Same seed + same virtual clock = byte-identical ids, payloads and delivery order. No `time.Now()` in any event or id generation path: the virtual clock owns time. No map iteration affecting output (sort keys first). A CI test runs a full scenario 100 times and diffs the webhook logs byte for byte. Changing generated output for a given seed is a breaking change.

**Errors, never panics.** Typed sentinel errors wrapped with `%w`, naming the entity involved, in English.

**No real anything.** The emulator never contacts real APIs, never handles real credentials meaningfully (any key is accepted), never moves money.

## Code style

- Go, two latest minors. `gofmt`, `go vet`, `golangci-lint` clean.
- Short lowercase package names: `engine`, `dispatch`, `chaos`, `asaas`, `clock`.
- `context.Context` first parameter on cancelable operations. Error as last return.
- Table-driven tests as the default. Golden files under `testdata/golden/<provider>/<version>/`.

## Commits

Conventional commits in English. Scopes: `engine`, `dispatch`, `chaos`, `asaas`, `cli`, `clock`.

```
feat(dispatch): pause queue after 15 consecutive delivery failures
fix(asaas): match sandbox payload for PIX qrcode field casing
```

## Out of scope

Do not implement, and close issues requesting: real API proxying or traffic recording, real money movement, hosted/cloud versions, non-payment providers, replacing provider homologation. New payment providers only with demonstrated demand (Mercado Pago is committed for v2).
