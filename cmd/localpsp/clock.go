package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type clockAdvanceRequestBody struct {
	Duration string `json:"duration"`
}

type clockTransition struct {
	ChargeID string    `json:"chargeId"`
	From     string    `json:"from"`
	To       string    `json:"to"`
	At       time.Time `json:"at"`
}

type clockAdvanceResponseBody struct {
	Now         time.Time         `json:"now"`
	Transitions []clockTransition `json:"transitions"`
}

func runClock(args []string) error {
	if len(args) < 1 || args[0] != "advance" {
		return fmt.Errorf("usage: localpsp clock advance <duration>")
	}
	if len(args) < 2 {
		return fmt.Errorf("usage: localpsp clock advance <duration>")
	}
	duration := args[1]

	fs := flag.NewFlagSet("clock advance", flag.ExitOnError)
	addr := fs.String("addr", defaultAddr, "address of a running localpsp serve instance")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	var resp clockAdvanceResponseBody
	err := adminRequest(ctx, *addr, http.MethodPost, "/_localpsp/clock/advance",
		clockAdvanceRequestBody{Duration: duration}, &resp)
	if err != nil {
		return err
	}

	fmt.Print(formatClockAdvance(resp))
	return nil
}

func formatClockAdvance(resp clockAdvanceResponseBody) string {
	now := resp.Now.UTC().Format("2006-01-02 15:04:05")
	if len(resp.Transitions) == 0 {
		return fmt.Sprintf("clock now at %s: nothing due\n", now)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "clock now at %s\n", now)
	for _, t := range resp.Transitions {
		fmt.Fprintf(&b, "  %s: %s -> %s\n", t.ChargeID, t.From, t.To)
	}
	return b.String()
}
