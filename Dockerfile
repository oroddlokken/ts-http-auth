FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY cmd ./cmd

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build -o http-auth ./cmd/http_auth.go

FROM alpine:latest

RUN apk add --no-cache curl

WORKDIR /app

COPY --from=builder /app/http-auth /app/http-auth

EXPOSE 12999

ENTRYPOINT ["/app/http-auth"]
