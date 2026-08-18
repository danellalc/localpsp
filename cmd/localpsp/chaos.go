package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"strings"
)

func runChaos(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: localpsp chaos <scenario> --charge <id>\n\n%s", chaosUsage())
	}
	scenario, rest := args[0], args[1:]

	switch scenario {
	case "duplicate-delivery":
		return runChaosDuplicateDelivery(rest)
	case "retry-storm":
		return runChaosRetryStorm(rest)
	case "queue-interrupt":
		return runChaosQueueInterrupt(rest)
	case "out-of-order":
		return runChaosOutOfOrder(rest)
	default:
		return fmt.Errorf("unknown chaos scenario %q\n\n%s", scenario, chaosUsage())
	}
}

func chaosUsage() string {
	return `scenarios:
  duplicate-delivery --charge ID          resend the charge's last webhook event with the same id
  retry-storm --charge ID --failures N    inject N synthetic delivery failures, then a real retry
  queue-interrupt --charge ID             inject enough failures to interrupt the webhook queue
  out-of-order --charge ID                deliver PAYMENT_RECEIVED before PAYMENT_CONFIRMED`
}

type chaosChargeRequestBody struct {
	Charge string `json:"charge"`
}

type chaosDuplicateDeliveryResponseBody struct {
	EventID   string   `json:"eventId"`
	Endpoints []string `json:"endpoints"`
}

func runChaosDuplicateDelivery(args []string) error {
	fs := flag.NewFlagSet("chaos duplicate-delivery", flag.ExitOnError)
	addr := fs.String("addr", defaultAddr, "address of a running localpsp serve instance")
	charge := fs.String("charge", "", "id of the charge whose last webhook event gets resent")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *charge == "" {
		return fmt.Errorf("usage: localpsp chaos duplicate-delivery --charge <id>: --charge is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	var resp chaosDuplicateDeliveryResponseBody
	err := adminRequest(ctx, *addr, http.MethodPost, "/_localpsp/chaos/duplicate-delivery",
		chaosChargeRequestBody{Charge: *charge}, &resp)
	if err != nil {
		return err
	}

	fmt.Printf("resent %s (same event id) to %s\n", resp.EventID, strings.Join(resp.Endpoints, ", "))
	return nil
}

type chaosRetryStormRequestBody struct {
	Charge   string `json:"charge"`
	Failures int    `json:"failures"`
}

type chaosEndpointFailuresBody struct {
	Endpoint string `json:"endpoint"`
	Injected int    `json:"injected"`
}

type chaosRetryStormResponseBody struct {
	EventID   string                      `json:"eventId"`
	Endpoints []chaosEndpointFailuresBody `json:"endpoints"`
}

func runChaosRetryStorm(args []string) error {
	fs := flag.NewFlagSet("chaos retry-storm", flag.ExitOnError)
	addr := fs.String("addr", defaultAddr, "address of a running localpsp serve instance")
	charge := fs.String("charge", "", "id of the charge whose last webhook event gets replayed")
	failures := fs.Int("failures", 0, "how many synthetic delivery failures to inject before the real retry")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *charge == "" {
		return fmt.Errorf("usage: localpsp chaos retry-storm --charge <id> --failures <n>: --charge is required")
	}
	if *failures <= 0 {
		return fmt.Errorf("usage: localpsp chaos retry-storm --charge <id> --failures <n>: --failures must be a positive integer")
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	var resp chaosRetryStormResponseBody
	err := adminRequest(ctx, *addr, http.MethodPost, "/_localpsp/chaos/retry-storm",
		chaosRetryStormRequestBody{Charge: *charge, Failures: *failures}, &resp)
	if err != nil {
		return err
	}

	fmt.Printf("queued %s to retry through synthetic failures before delivering for real:\n", resp.EventID)
	for _, ep := range resp.Endpoints {
		fmt.Printf("  %s: %d synthetic failure(s) injected\n", ep.Endpoint, ep.Injected)
	}
	return nil
}

type chaosQueueInterruptResponseBody struct {
	EventID          string   `json:"eventId"`
	Endpoints        []string `json:"endpoints"`
	InjectedFailures int      `json:"injectedFailures"`
}

func runChaosQueueInterrupt(args []string) error {
	fs := flag.NewFlagSet("chaos queue-interrupt", flag.ExitOnError)
	addr := fs.String("addr", defaultAddr, "address of a running localpsp serve instance")
	charge := fs.String("charge", "", "id of the charge whose webhook queue gets interrupted")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *charge == "" {
		return fmt.Errorf("usage: localpsp chaos queue-interrupt --charge <id>: --charge is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	var resp chaosQueueInterruptResponseBody
	err := adminRequest(ctx, *addr, http.MethodPost, "/_localpsp/chaos/queue-interrupt",
		chaosChargeRequestBody{Charge: *charge}, &resp)
	if err != nil {
		return err
	}

	fmt.Printf("injected %d synthetic failure(s) on %s to interrupt the queue delivering %s\n",
		resp.InjectedFailures, strings.Join(resp.Endpoints, ", "), resp.EventID)
	fmt.Println("run `localpsp state` once delivery catches up to confirm the queue is now interrupted")
	return nil
}

type chaosFiredEventBody struct {
	Event     string   `json:"event"`
	EventID   string   `json:"eventId"`
	Endpoints []string `json:"endpoints"`
}

type chaosOutOfOrderResponseBody struct {
	Charge struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"charge"`
	Events []chaosFiredEventBody `json:"events"`
}

func runChaosOutOfOrder(args []string) error {
	fs := flag.NewFlagSet("chaos out-of-order", flag.ExitOnError)
	addr := fs.String("addr", defaultAddr, "address of a running localpsp serve instance")
	charge := fs.String("charge", "", "id of the charge to confirm and receive out of order")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *charge == "" {
		return fmt.Errorf("usage: localpsp chaos out-of-order --charge <id>: --charge is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	var resp chaosOutOfOrderResponseBody
	err := adminRequest(ctx, *addr, http.MethodPost, "/_localpsp/chaos/out-of-order",
		chaosChargeRequestBody{Charge: *charge}, &resp)
	if err != nil {
		return err
	}

	fmt.Printf("%s is now %s (confirmed, then received, both for real)\n", resp.Charge.ID, resp.Charge.Status)
	fmt.Println("delivery order:")
	for i, ev := range resp.Events {
		fmt.Printf("  %d. %s (%s) -> %s\n", i+1, ev.Event, ev.EventID, strings.Join(ev.Endpoints, ", "))
	}
	return nil
}
