FROM golang:1.24-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY pkg ./pkg

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/finance-spoke ./cmd/finance-spoke

FROM alpine:3.21

RUN addgroup -S app && adduser -S app -G app

WORKDIR /app

COPY --from=builder /out/finance-spoke /usr/local/bin/finance-spoke

USER app

EXPOSE 8091

ENTRYPOINT ["/usr/local/bin/finance-spoke"]
