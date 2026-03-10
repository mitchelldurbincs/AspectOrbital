FROM rust:1.94-alpine AS builder

WORKDIR /src

COPY cmd/kalshi-spoke ./cmd/kalshi-spoke

RUN cargo build --release --manifest-path cmd/kalshi-spoke/Cargo.toml

FROM alpine:3.21

RUN addgroup -S app && adduser -S app -G app

WORKDIR /app

COPY --from=builder /src/cmd/kalshi-spoke/target/release/kalshi-spoke /usr/local/bin/kalshi-spoke

USER app

EXPOSE 8092

ENTRYPOINT ["/usr/local/bin/kalshi-spoke"]
