# Fidelity

Every known difference between localpsp and the real Asaas API, and every operational
detail we've confirmed against Asaas's own docs. If something localpsp does isn't
listed here as confirmed, don't trust it as fact yet, it may be a reasonable guess
waiting on real verification (sandbox golden files, in Fase 2, or firsthand production
experience).

## Confirmed against Asaas's official docs

Sourced from docs.asaas.com, checked in English and Portuguese where both exist.

- **Successful delivery**: HTTP status `>= 200` and `< 300` counts as delivered. Anything
  else, including timeouts, counts as a failure.
  Source: [About webhooks](https://docs.asaas.com/docs/about-webhooks),
  [Introdução - Webhooks](https://docs.asaas.com/docs/sobre-os-webhooks).
- **Response timeout**: Asaas waits up to 10 seconds for your endpoint to respond.
  No response in that window counts as a failure (logged as a "Read Timed Out" error
  on the real platform).
  Source: [Erro Read Timed Out](https://docs.asaas.com/docs/erro-read-timed-out).
- **Queue interruption**: after 15 consecutive delivery failures for a given webhook
  configuration, its sync queue pauses. Events keep being generated and stay queued;
  they are not delivered while paused. Other webhook configurations on the same
  account are unaffected.
  Source: [Webhooks with queue paused](https://docs.asaas.com/docs/webhooks-queue-paused).
- **Reactivation**: happens through the dashboard (Integrations > Webhooks) or via the
  update webhook API endpoint, sending `interrupted: false`. Once reactivated, every
  event that piled up while paused is resent in chronological order.
  Source: [How to reactivate interrupted queue?](https://docs.asaas.com/docs/how-to-reactivate-interrupted-queue).
- **Event retention**: events stuck in a paused queue for more than 14 days are
  permanently deleted.
  Source: [Webhooks with queue paused](https://docs.asaas.com/docs/webhooks-queue-paused).
- **Delivery is at least once**: the same event can be delivered more than once as a
  normal consequence of the retry mechanism, not only as a deliberately triggered
  chaos scenario.
  Source: search result summary over docs.asaas.com (webhook delivery model).
- **Authentication is a static token, not a computed signature**: the `authToken` you
  configure on a webhook (32 to 255 characters) is sent back verbatim in the
  `asaas-access-token` header on every delivery. If you don't set one, Asaas generates
  one for you and shows it once. There's no HMAC or request signing involved, unlike
  Stripe.
  Source: [Webhooks](https://docs.asaas.com/docs/webhooks-3),
  [Criar novo webhook](https://docs.asaas.com/reference/criar-novo-webhook).
- **Webhook config fields**: `authToken`, `url`, `events`, `enabled`, `interrupted`,
  `name`, `email`, `apiVersion`, `sendType`.
  Source: [Criar novo webhook](https://docs.asaas.com/reference/criar-novo-webhook).
- **Customer fields**: required `name`, `cpfCnpj`. Optional `email`, `phone`,
  `mobilePhone`, `address`, `addressNumber`, `complement`, `province`, `postalCode`,
  `externalReference`, `notificationDisabled`, `additionalEmails`,
  `municipalInscription`, `stateInscription`, `observations`, `groupName`, `company`,
  `foreignCustomer`. Response adds `object: "customer"`, `id`, `dateCreated`, `city`,
  `cityName`, `state`, `country`, `personType` (`FISICA` or `JURIDICA`), `deleted`.
  Source: [Criar novo cliente](https://docs.asaas.com/reference/criar-novo-cliente).
- **Payment (charge) fields**: required `customer`, `billingType`
  (`UNDEFINED`/`BOLETO`/`CREDIT_CARD`/`PIX`), `value`, `dueDate`. Optional
  `description`, `externalReference`, `installmentCount`, `discount`, `interest`,
  `fine`, `postalService`, `split`, `callback`, among others. Response adds
  `object: "payment"`, `id`, `dateCreated`, `netValue`, `status`, `paymentDate`,
  `invoiceUrl`, `bankSlipUrl`, `pixQrCodeId`, `deleted`, `anticipated`, and more.
  Source: [Criar nova cobrança](https://docs.asaas.com/reference/criar-nova-cobranca),
  [Create new payment](https://docs.asaas.com/reference/create-new-payment).
- **Payment status values**: `PENDING`, `RECEIVED`, `CONFIRMED`, `OVERDUE`,
  `REFUNDED`, `RECEIVED_IN_CASH`, `REFUND_REQUESTED`, `REFUND_IN_PROGRESS`,
  `CHARGEBACK_REQUESTED`, `CHARGEBACK_DISPUTE`, `AWAITING_CHARGEBACK_REVERSAL`,
  `DUNNING_REQUESTED`, `DUNNING_RECEIVED`, `AWAITING_RISK_ANALYSIS`. localpsp's
  engine only models the five that matter for a normal charge lifecycle
  (`created`/`PENDING`, `confirmed`/`CONFIRMED`, `received`/`RECEIVED`,
  `overdue`/`OVERDUE`, `refunded`/`REFUNDED`); the rest are chargeback/dunning/risk
  states outside v1's scope, not modeled yet.
  Source: [Create new payment](https://docs.asaas.com/reference/create-new-payment).
- **Sandbox payment confirmation**: `POST /v3/sandbox/payment/{id}/confirm`, sandbox
  only, empty request body, returns the full updated payment object. This is the
  mechanism behind the dashboard's "simulate payment received" button, and what
  localpsp's own payment simulation endpoint mirrors.
  Source: [(Apenas sandbox) Confirmar o pagamento](https://docs.asaas.com/reference/confirmar-pagamento).
- **Subscription fields**: required `customer`, `billingType`, `value`, `nextDueDate`,
  `cycle` (`WEEKLY`/`BIWEEKLY`/`MONTHLY`/`BIMONTHLY`/`QUARTERLY`/`SEMIANNUALLY`/`YEARLY`).
  Optional `discount`, `interest`, `fine`, `description`, `endDate`, `maxPayments`,
  `externalReference`, `split`, `callback`. Response adds `object: "subscription"`,
  `id`, `dateCreated`, `status` (`ACTIVE`/`EXPIRED`/`INACTIVE`), `deleted`,
  `checkoutSession`. engine.Interval matches the full `cycle` enum.
  Source: [Criar nova assinatura](https://docs.asaas.com/reference/criar-nova-assinatura).

## Known gaps (confirmed real behavior, not built yet)

These aren't open questions, the real behavior is confirmed above, localpsp just
doesn't implement it yet. Listed here instead of silently diverging.

- **No 14 day stale event eviction.** Real Asaas deletes events that have sat in a
  paused queue for more than 14 days (see Event retention, above). localpsp's queue
  has no eviction at all: an interrupted webhook's pending events, and the delivery
  log itself, accumulate for as long as the process runs (or forever, with
  `--persist`). Fine for a test run measured in minutes; a long-lived `localpsp serve`
  process left interrupted for weeks would grow both without bound, unlike real Asaas.
- **No cap on webhook registrations, and no delete route.** Real Asaas lets you list
  and delete webhook configs; localpsp only has create and update. Nothing stops
  registering an unbounded number of webhooks in a single run.
- **Webhook URLs are trusted as-is.** Delivering to whatever URL a webhook config
  names, including a private/internal address, is the entire point of an emulator
  that calls your app back, so there's no SSRF-style URL filtering here, unlike a
  service that should never let a caller point it at its own internal network.
  Fine for local/CI use against your own app; don't expose a running `localpsp serve`
  to untrusted callers.

## Open, not yet verified

- **Exact interval between retry attempts.** Docs are inconsistent: one search summary
  says a flat 30 second sync interval, another describes a "progressive penalty
  mechanism" (suggesting some kind of backoff), and no page we found states a precise
  schedule with a citable quote. localpsp uses a flat 30 second interval as a
  documented placeholder (`dispatch.DefaultRetryInterval`) until this gets checked
  against real behavior, ideally by recording it against the sandbox once Fase 2 sets
  that up, or from firsthand production observation. Don't treat the exact spacing
  between retries as fidelity-accurate yet, only the 2xx-or-fail policy, the 10 second
  timeout and the 15-failure threshold are confirmed.
- **The whole `providers/asaas` facade is pending golden files.** Every field listed
  above comes from Asaas's documentation, summarized by an AI fetch of docs.asaas.com,
  not from a byte-for-byte recorded sandbox response. Docs can lag or omit fields the
  real API actually returns (optional fields that are always present in practice,
  slightly different casing, fields specific to account configuration). Treat the
  facade's JSON shape as a solid, doc-grounded first pass, not fidelity-verified,
  until real sandbox responses get recorded as golden files under
  `testdata/golden/asaas/v3/` and the facade's tests assert against them byte for byte.
  This is the single biggest open item before this project's fidelity claim is real.
- **Disclosed gaps in the facade, real fields with no real content behind them yet.**
  Rather than fabricate a value for something the docs confirm exists but don't
  describe the shape of, these are left empty/null on purpose:
  - `invoiceUrl` on a payment points back at localpsp's own `GET .../invoices/{id}`,
    which shows the payment's live JSON state, not a real hosted checkout page (real
    Asaas has an actual invoice UI with PIX/boleto/card options; localpsp doesn't
    render one). It's a real, resolvable URL, checked against desapega.do's actual
    Asaas client code, which reads this field to redirect the user, an empty one broke
    that redirect. `bankSlipUrl` and `pixQrCodeId` are still always `null`: those would
    need to fabricate an actual boleto number or PIX QR payload, which nothing sourced
    here describes the real shape of.
  - `city`, `cityName`, `state`, `country` on a customer are never populated. Real
    Asaas derives them from `postalCode` via its own CEP lookup service; localpsp
    doesn't touch real external services, so it can't honestly fill these in either.
  - A subscription's `status` is always `"ACTIVE"` and `checkoutSession` is always
    `null`: engine has no subscription lifecycle yet (no `EXPIRED`/`INACTIVE`
    transition exists to drive it).
  - A refunded charge never gets a `paymentDate`, even though real Asaas keeps
    reporting the original payment date after a refund. There's no refund route wired
    up yet, and no separate "first paid at" timestamp in engine to report honestly
    once one exists (the charge's `UpdatedAt` gets re-stamped by the refund itself, so
    it can't be reused for this).
  - The webhook config id's `wh_` prefix is a guess, unlike `pay_`/`cus_`/`sub_`
    (confirmed from real Asaas examples) and `evt_` (used in the earlier webhook
    payload example), nothing sourced here shows what a real webhook config id looks
    like.
  - HTTP status `200` on every create (customers, payments, subscriptions, webhooks,
    sandbox confirm) is unconfirmed; no page sourced here states it outright, `200`
    matches the author's general recollection of Asaas's REST behavior, not a citable
    quote.
- **Ids inside delivered webhook payloads are deterministic, on purpose.** An event's
  `id` and an auto-generated webhook's `id`/`authToken` are minted from the engine's
  own seeded id source (`Engine.NextID`/`Engine.NextToken`), the same one entity ids
  come from, specifically so the same seed and call sequence produce byte-identical
  webhook bodies. An earlier draft of this facade minted them from an unseeded random
  source; that was a real bug, caught in review, not a design that's still open.
