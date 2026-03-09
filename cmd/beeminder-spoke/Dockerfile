FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY pkg ./pkg

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/beeminder-spoke ./cmd/beeminder-spoke

FROM alpine:3.21

RUN addgroup -S app && adduser -S app -G app

WORKDIR /app

COPY --from=builder /out/beeminder-spoke /usr/local/bin/beeminder-spoke

USER app

EXPOSE 8090

ENTRYPOINT ["/usr/local/bin/beeminder-spoke"]
