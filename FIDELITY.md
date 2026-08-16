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
