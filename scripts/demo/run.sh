#!/bin/sh
set -eu

docker rm -f localpsp-demo >/dev/null 2>&1 || true

docker run -d --name localpsp-demo -p 8420:8420 \
  --add-host=host.docker.internal:host-gateway \
  danellaclaudioluiz/localpsp >/dev/null

for i in $(seq 1 20); do
  curl -sf http://localhost:8420/_localpsp/state >/dev/null && break
  sleep 0.25
done

curl -s -X POST http://localhost:8420/asaas/v3/webhooks \
  -H "Content-Type: application/json" \
  -d '{"url":"http://host.docker.internal:9000/webhook","events":["PAYMENT_CONFIRMED"]}' >/dev/null

customer=$(curl -s -X POST http://localhost:8420/asaas/v3/customers \
  -H "Content-Type: application/json" \
  -d '{"name":"Ana Souza","cpfCnpj":"12345678901"}' | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)

charge=$(curl -s -X POST http://localhost:8420/asaas/v3/payments \
  -H "Content-Type: application/json" \
  -d "{\"customer\":\"$customer\",\"billingType\":\"PIX\",\"value\":150.00,\"dueDate\":\"2026-01-10\"}" \
  | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)

echo "cobranca $charge criada, disparando payment.confirmed..."

curl -s -X POST http://localhost:8420/_localpsp/trigger \
  -H "Content-Type: application/json" \
  -d "{\"event\":\"payment.confirmed\",\"charge\":\"$charge\"}" >/dev/null
