#!/bin/sh
set -eu

type_line() {
  printf '$ %s' "$1"
  sleep 1
  printf '\n'
}

clear

type_line "docker run -p 8420:8420 danellaclaudioluiz/localpsp"
localpsp serve --addr :8420 &
server_pid=$!
for i in $(seq 1 20); do
  curl -sf http://localhost:8420/_localpsp/state >/dev/null 2>&1 && break
  sleep 0.25
done
sleep 0.5

type_line "curl -X POST localhost:8420/asaas/v3/webhooks -d '{\"url\":\"http://localhost:9000\",\"events\":[\"PAYMENT_CONFIRMED\"]}'"
curl -s -X POST http://localhost:8420/asaas/v3/webhooks \
  -H "Content-Type: application/json" \
  -d '{"url":"http://localhost:9000/webhook","events":["PAYMENT_CONFIRMED"]}'
echo
sleep 1

nohup python3 receiver.py >/tmp/receiver.out 2>&1 &
receiver_pid=$!
sleep 0.5

type_line "curl -X POST localhost:8420/asaas/v3/customers -d '{\"name\":\"Ana Souza\",\"cpfCnpj\":\"12345678901\"}'"
customer_resp=$(curl -s -X POST http://localhost:8420/asaas/v3/customers \
  -H "Content-Type: application/json" \
  -d '{"name":"Ana Souza","cpfCnpj":"12345678901"}')
echo "$customer_resp"
customer=$(echo "$customer_resp" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
sleep 1

type_line "curl -X POST localhost:8420/asaas/v3/payments -d '{\"customer\":\"$customer\",\"billingType\":\"PIX\",\"value\":150.00,\"dueDate\":\"2026-01-10\"}'"
charge_resp=$(curl -s -X POST http://localhost:8420/asaas/v3/payments \
  -H "Content-Type: application/json" \
  -d "{\"customer\":\"$customer\",\"billingType\":\"PIX\",\"value\":150.00,\"dueDate\":\"2026-01-10\"}")
echo "$charge_resp"
charge=$(echo "$charge_resp" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
sleep 1

type_line "localpsp trigger payment.confirmed --charge $charge"
curl -s -X POST http://localhost:8420/_localpsp/trigger \
  -H "Content-Type: application/json" \
  -d "{\"event\":\"payment.confirmed\",\"charge\":\"$charge\"}" >/dev/null
echo "fired PAYMENT_CONFIRMED: $charge is now CONFIRMED"

sleep 1.5
cat /tmp/receiver.out
sleep 2

kill $server_pid $receiver_pid 2>/dev/null || true
