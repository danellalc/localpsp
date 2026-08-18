// Command localpsp runs the emulator and drives it from the terminal:
// serve stands up the HTTP API, state/trigger/clock talk to a running
// serve instance over localpsp's own admin API.
package main

import (
	"fmt"
	"os"
)

// version is overridden at build time via -ldflags "-X main.version=v0.1.0",
// the release workflow does this for every published binary and Docker
// image. A plain "go build"/"go run" (local dev, "go install") keeps "dev".
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage())
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "serve":
		err = runServe(os.Args[2:])
	case "state":
		err = runState(os.Args[2:])
	case "trigger":
		err = runTrigger(os.Args[2:])
	case "clock":
		err = runClock(os.Args[2:])
	case "chaos":
		err = runChaos(os.Args[2:])
	case "version", "-v", "--version":
		fmt.Println("localpsp " + version)
		return
	case "-h", "--help", "help":
		fmt.Println(usage())
		return
	default:
		err = fmt.Errorf("unknown command %q\n\n%s", os.Args[1], usage())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "localpsp:", err)
		os.Exit(1)
	}
}

func usage() string {
	return `usage: localpsp <command>

commands:
  serve                        run the emulator's HTTP API
  state                        show customers, charges and webhook queues from a running server
  trigger <event> --charge ID  fire a payment lifecycle event against a charge
  clock advance <duration>     move the virtual clock forward
  chaos <scenario> --charge ID fire one of the chaos scenarios below
  version                      print the localpsp version

events for trigger: payment.confirmed, payment.received
durations for clock advance are Go duration strings, like 72h or 30m, or a
number of days, like 3d

` + chaosUsage()
}
