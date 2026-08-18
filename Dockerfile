FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/localpsp ./cmd/localpsp

FROM scratch
COPY --from=build /out/localpsp /localpsp
EXPOSE 8420
ENTRYPOINT ["/localpsp"]
CMD ["serve", "--addr", ":8420"]
