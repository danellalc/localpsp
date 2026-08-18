# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/localpsp ./cmd/localpsp

FROM scratch
COPY --from=build /out/localpsp /localpsp
EXPOSE 8420
USER 65532:65532
ENTRYPOINT ["/localpsp"]
CMD ["serve", "--addr", ":8420"]
