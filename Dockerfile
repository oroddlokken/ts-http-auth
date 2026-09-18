FROM golang:1.24-alpine AS builder

ARG VERSION=dev

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build -ldflags "-s -w -X main.version=$VERSION" -o http-auth ./cmd/http_auth.go

FROM alpine:3.24

RUN apk add --no-cache curl

ARG TS_HTTP_AUTH_BUILD_SHA="dev"
ARG TS_HTTP_AUTH_BUILD_BRANCH="unknown"
ARG TS_HTTP_AUTH_BUILD_COMMIT_DATE="unknown"
ENV TS_HTTP_AUTH_BUILD_SHA=$TS_HTTP_AUTH_BUILD_SHA
ENV TS_HTTP_AUTH_BUILD_BRANCH=$TS_HTTP_AUTH_BUILD_BRANCH
ENV TS_HTTP_AUTH_BUILD_COMMIT_DATE=$TS_HTTP_AUTH_BUILD_COMMIT_DATE

RUN adduser -D -u 1000 app

WORKDIR /app

COPY --from=builder /app/http-auth /app/http-auth

USER app

EXPOSE 12999

ENTRYPOINT ["/app/http-auth"]
