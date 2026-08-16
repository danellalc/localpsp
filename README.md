# localpsp

A fake but stateful server for Brazilian payment providers that you can run locally. Develop and test payment flows without ngrok, without sandbox accounts, without touching the internet.

Like [localstripe](https://github.com/adrienverge/localstripe) and [LocalStack](https://localstack.cloud), but for the PSPs Brazilian applications actually integrate: Asaas first, Mercado Pago and Efí on the roadmap.

> Status: in development. This README describes the design being built.

> [GIF placeholder: will be recorded from the first real run before launch.]

## The problem

Every developer integrating payments in Brazil lives the same loop: create a sandbox account, configure a webhook URL, expose your laptop with ngrok (the official Asaas docs literally recommend this), create a test charge, simulate payment in their dashboard, wait, and hope the event arrives. For every flow. Every time.

And the sandbox can't simulate what actually breaks in production: duplicate webhook deliveries hitting your idempotency logic, the retry storm after your deploy window, or Asaas silently pausing your webhook queue after 15 consecutive failures. New events keep queueing but stop being delivered, and nothing tells you.

Stripe solved local development years ago with `stripe listen` and `stripe trigger`. Stripe's own mock is stateless and doesn't send webhooks, and Stripe explicitly points to community tools for anything more. In Brazil, there is no community tool. Until now.

## Usage

```bash
docker run -p 8420:8420 danellalc/localpsp
```

Point your app at it instead of the real API. Same endpoints, same payloads, same webhook signatures:

```
ASAAS_API_URL=http://localhost:8420/asaas/v3
```

Create customers and charges exactly as you would against the real Asaas API. localpsp is **stateful**: what you create persists and behaves.

Then make things happen:

```bash
# simulate a PIX payment against a charge, fires the real webhook at your app
localpsp trigger payment.confirmed --charge pay_123

# the full lifecycle, with realistic timing
localpsp trigger payment.received --charge pay_123

# the tests nobody can run today:
localpsp chaos duplicate-delivery --charge pay_123   # idempotency, finally testable
localpsp chaos retry-storm --failures 5              # your backoff handling
localpsp chaos queue-interrupt                       # Asaas pauses after 15 fails. Does your app notice?
```

Deterministic: same seed, same sequence of events, byte-identical payloads. Built for CI.

## What makes it different

### Stateful, like the real thing

Create a charge, it exists. Pay it, its status changes. Query it, you get what a real PSP would return. Your integration code runs unmodified: no `if (testing)` branches, no mock injection, any language.

### It simulates production behavior, not just the API

Mocking endpoints is easy. What hurts in production is operational behavior:

| Behavior | Real consequence | Testable today? |
|---|---|---|
| Duplicate webhook delivery | double-crediting a customer | no |
| `PAYMENT_CONFIRMED` vs `PAYMENT_RECEIVED` | releasing product before money settles (D+32 on card!) | barely |
| Queue interrupted after 15 failures | webhooks silently stop; orders stuck forever | **no** |
| Retry with backoff | thundering herd after your deploy | no |
| Out-of-order delivery | status regression in your DB | no |

localpsp makes every row testable, locally, in CI.

### Deterministic

Same seed, same events, same payloads, same order. A flaky payment test is a useless payment test. (Same discipline as [EFCore.AutoSeed](https://github.com/danellalc/EFCore.AutoSeed), same author.)

### Honest about what it is

**Develop and run CI against localpsp. Homologate against the real sandbox once, at the end.** A mock can never behave exactly like the real API and may differ in nuanced ways. Stripe says this about their own mock, and it applies here too. localpsp kills the two hundred sandbox round-trips during development; it does not replace the final homologation checklist.

## Providers

| Provider | Status |
|---|---|
| Asaas (charges, PIX, boleto, card, subscriptions, webhooks) | **v1** |
| Mercado Pago | roadmap |
| Efí (Gerencianet) | roadmap |
| Raw PIX (BACEN API patterns) | investigating |

One container, all providers, each under its own route prefix.

**Providers outside Brazil are not on the roadmap, by design.** This project's depth is Brazilian payment semantics; that focus is the whole point. The engine is provider-agnostic though, and the facade contract is public: if you want Razorpay, Paystack or anything else, an adapter PR is welcome. (For Stripe, [localstripe](https://github.com/adrienverge/localstripe) already exists and is great.)

## What it does not do

- **It does not replace homologation.** Final validation happens against the provider's real sandbox.
- **It does not touch real APIs.** Fully offline by design.
- **It is not a payment gateway.** It's a development tool. No real money, ever.
- **It does not proxy or record real traffic** (that's a different tool).

## Compared to

- **ngrok + real sandbox**: the current official workflow. Works, but every test is a network round-trip, sandbox state is shared and drifts, and chaos scenarios are impossible.
- **stripe-mock**: stateless, no webhooks, Stripe-only. Stripe points to community tools for more.
- **localstripe**: the proof this category works: stateful, webhook-firing... and Stripe-only.
- **hookmock / generic mock servers**: you hand-write every payload; they know nothing about PSP semantics, signatures, or lifecycles.
- **WireMock / Prism**: great generic HTTP mocking; no state, no event lifecycle, no PSP behavior.

## License

MIT
