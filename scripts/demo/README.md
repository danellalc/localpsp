# Demo recording

The fifteen seconds the README's GIF placeholder is waiting for: `docker run`,
a customer and a charge created, `trigger payment.confirmed`, and the webhook
landing in a second terminal.

## Setup

Two terminal panes, side by side.

Left pane, the webhook receiver:

```bash
python3 scripts/demo/receiver.py
```

Right pane, the actual demo:

```bash
sh scripts/demo/run.sh
```

`run.sh` starts the container, registers a webhook pointed at the receiver,
creates a customer and a PIX charge, then triggers `payment.confirmed`. The
left pane prints the event the moment it arrives.

## Recording it

Any terminal recorder works (asciinema, a plain screen capture cropped to
both panes, whatever you already have). A few things that make the result
actually watchable:

- Bump the terminal font size before recording, GIFs get shrunk hard in
  READMEs and small text disappears.
- Clear both panes right before hitting record.
- Run `sh scripts/demo/run.sh` once uncounted first, so the Docker image is
  already pulled locally and the real recording isn't stalled on a download.
- Trim the recording so it starts right at the `docker run` and ends right
  after the webhook prints, that's the fifteen-second story.

## Cleanup

```bash
docker rm -f localpsp-demo
```
