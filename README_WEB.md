# OpenTrace Web MVP

This repository now includes a minimal self-hosted web version backed by Go.

## What it does

- serves a browser UI
- starts `nexttrace` on the server
- streams hop updates over SSE
- renders hops in a table and on an OpenStreetMap view

## Run

1. Install `nexttrace` and make sure it is in your `PATH`, or pass its path explicitly.
2. Start the server from the repository root:

```bash
go run ./cmd/server
```

Or with an explicit binary path:

```bash
go run ./cmd/server --nexttrace-bin /path/to/nexttrace
```

Or via environment variable:

```bash
NEXTTRACE_BIN=/path/to/nexttrace go run ./cmd/server
```

3. Open [http://127.0.0.1:8080](http://127.0.0.1:8080)

## Flags

- `-addr`
  - listen address, default `:8080`
- `-nexttrace-bin`
  - explicit path to the `nexttrace` executable

## API

- `POST /api/traces`
- `GET /api/traces/{id}/events`
- `DELETE /api/traces/{id}`
- `GET /api/health`

## Docker

Build a local image:

```bash
docker buildx build --platform linux/amd64,linux/arm64 -t opentrace-web:latest .
```

Run it:

```bash
docker run --rm -p 8080:8080 opentrace-web:latest
```

Publish a multi-arch image:

```bash
docker buildx build --platform linux/amd64,linux/arm64 -t your-registry/opentrace-web:latest --push .
```

Notes:

- the image runs as `root`
- it bundles `nexttrace-amd64` and `nexttrace-arm64`
- the final image auto-selects the right `nexttrace` binary for `amd64` or `arm64`
- GitHub Actions workflow pushes to Docker Hub using `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN`

Example create request:

```json
{
  "target": "1.1.1.1",
  "protocol": "icmp",
  "timeoutSeconds": 120,
  "mtr": false
}
```

## Notes

- traces run from the server, not the browser client
- TCP/UDP or some ICMP modes may require extra privileges on Linux, such as `CAP_NET_RAW`
- this is an MVP alongside the desktop app, not a full feature-for-feature replacement yet
