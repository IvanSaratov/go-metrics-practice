# syntax=docker/dockerfile:1

FROM golang:1.26.4-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/server ./cmd/server \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/agent ./cmd/agent

FROM alpine:3.20 AS server

RUN addgroup -S metrics \
    && adduser -S -G metrics metrics \
    && mkdir -p /data \
    && chown metrics:metrics /data

COPY --from=builder /out/server /usr/local/bin/server

USER metrics

EXPOSE 8080

ENTRYPOINT ["server"]

FROM alpine:3.20 AS agent

RUN addgroup -S metrics \
    && adduser -S -G metrics metrics

COPY --from=builder /out/agent /usr/local/bin/agent

USER metrics

ENTRYPOINT ["agent"]
