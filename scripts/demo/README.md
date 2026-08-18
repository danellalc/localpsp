# Demo recording

The fifteen seconds `docs/demo.gif` is made of: `docker run`, a customer and
a charge created, `trigger payment.confirmed`, and the webhook landing.

## Regenerating docs/demo.gif

One command, no screen recorder involved:

```bash
sh scripts/demo/record.sh
```

It builds a linux/amd64 `localpsp` from the current source, runs the exact
sequence in `typed-demo.sh` inside a container with `asciinema` recording
the terminal session, then renders that recording to `docs/demo.gif` with
`agg`. Needs Docker, nothing else. Run this again after any change that'd
show up in the demo (CLI output wording, JSON field names, timing) so the
GIF never drifts from what the CLI actually does.

`typed-demo.sh` is the actual script of the recording: starts the server,
registers a webhook, creates a customer and a PIX charge, triggers
`payment.confirmed`, then prints the receiver's log so the delivered event
shows up in the same terminal. Each `type_line` call is a fake keystroke
delay before a real command, everything after that is genuine output from
a real `localpsp` binary, nothing in the recording is staged or edited in.

## Trying the flow live instead of recording it

Two terminal panes, side by side. Left, the webhook receiver:

```bash
python3 scripts/demo/receiver.py
```

Right, the actual demo:

```bash
sh scripts/demo/run.sh
```

`run.sh` starts the published container, registers a webhook pointed at the
receiver, creates a customer and a PIX charge, then triggers
`payment.confirmed`. The left pane prints the event the moment it arrives.

Cleanup: `docker rm -f localpsp-demo`.
