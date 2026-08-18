#!/bin/sh
set -eu

script_dir=$(cd "$(dirname "$0")" && pwd)
repo_root=$(cd "$script_dir/../.." && pwd)

if (cd "$script_dir" && pwd -W) >/dev/null 2>&1; then
  script_dir=$(cd "$script_dir" && pwd -W)
  repo_root=$(cd "$repo_root" && pwd -W)
fi
export MSYS_NO_PATHCONV=1

echo "building the linux/amd64 binary to record against..."
(cd "$repo_root" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$script_dir/localpsp" ./cmd/localpsp)

echo "building the recorder image..."
docker build -f "$script_dir/recorder.Dockerfile" -t localpsp-demo-recorder "$script_dir"

echo "recording..."
docker run --rm -t \
  -v "${script_dir}:/demo" \
  localpsp-demo-recorder \
  sh -c "cp /demo/localpsp /usr/local/bin/localpsp && chmod +x /demo/typed-demo.sh && stty rows 16 cols 100 && asciinema rec --command 'sh typed-demo.sh' /demo/demo.cast"

echo "rendering the gif..."
docker run --rm \
  -v "${script_dir}:/demo" \
  localpsp-demo-recorder \
  agg --font-family "DejaVu Sans Mono" --font-size 18 --theme monokai --last-frame-duration 3 /demo/demo.cast /demo/demo.gif

mkdir -p "$repo_root/docs"
mv "$script_dir/demo.gif" "$repo_root/docs/demo.gif"
rm -f "$script_dir/demo.cast" "$script_dir/localpsp"

echo "done: docs/demo.gif"
