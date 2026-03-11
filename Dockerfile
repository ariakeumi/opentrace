# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS builder

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=$TARGETARCH go build -o /out/opentrace-web ./cmd/server

FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

ARG TARGETARCH

COPY --from=builder /out/opentrace-web /app/opentrace-web
COPY nexttrace-amd64 /tmp/nexttrace-amd64
COPY nexttrace-arm64 /tmp/nexttrace-arm64

RUN case "$TARGETARCH" in \
      amd64) cp /tmp/nexttrace-amd64 /app/nexttrace ;; \
      arm64) cp /tmp/nexttrace-arm64 /app/nexttrace ;; \
      *) echo "unsupported TARGETARCH: $TARGETARCH" >&2; exit 1 ;; \
    esac \
    && chmod +x /app/opentrace-web /app/nexttrace \
    && rm -f /tmp/nexttrace-amd64 /tmp/nexttrace-arm64

ENV NEXTTRACE_BIN=/app/nexttrace

EXPOSE 8080

ENTRYPOINT ["/app/opentrace-web"]
CMD ["--addr", ":8080", "--nexttrace-bin", "/app/nexttrace"]
