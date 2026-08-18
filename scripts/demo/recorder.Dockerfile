FROM python:3-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends curl ca-certificates fonts-dejavu-core \
    && rm -rf /var/lib/apt/lists/* \
    && pip install --no-cache-dir asciinema \
    && curl -fsSL -o /usr/local/bin/agg https://github.com/asciinema/agg/releases/download/v1.9.0/agg-x86_64-unknown-linux-gnu \
    && chmod +x /usr/local/bin/agg

WORKDIR /demo
