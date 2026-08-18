package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
)

type triggerRequestBody struct {
	Charge string `json:"charge"`
	Event  string `json:"event"`
}

type triggerResponseBody struct {
	Event  string `json:"event"`
	Charge struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"charge"`
}

func runTrigger(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: localpsp trigger <event> --charge <id>")
	}
	event := args[0]

	fs := flag.NewFlagSet("trigger", flag.ExitOnError)
	addr := fs.String("addr", defaultAddr, "address of a running localpsp serve instance")
	charge := fs.String("charge", "", "id of the charge to fire the event against")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *charge == "" {
		return fmt.Errorf("usage: localpsp trigger <event> --charge <id>: --charge is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	var resp triggerResponseBody
	err := adminRequest(ctx, *addr, http.MethodPost, "/_localpsp/trigger",
		triggerRequestBody{Charge: *charge, Event: event}, &resp)
	if err != nil {
		return err
	}

	fmt.Printf("fired %s: %s is now %s\n", resp.Event, resp.Charge.ID, resp.Charge.Status)
	return nil
}
