# Shinzo Scheduler

[![CI](https://github.com/shinzonetwork/shinzo-scheduler-service/actions/workflows/ci.yml/badge.svg)](https://github.com/shinzonetwork/shinzo-scheduler-service/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/shinzonetwork/shinzo-scheduler-service/branch/main/graph/badge.svg)](https://codecov.io/gh/shinzonetwork/shinzo-scheduler-service)

Coordination layer between indexers and hosts in the Shinzo ecosystem. The scheduler manages task assignment, lifecycle tracking, and settlement between participants in the network.

## Quick Start

```bash
# Clone and build
git clone https://github.com/shinzonetwork/shinzo-scheduler-service.git
cd shinzo-scheduler-service
go build -o scheduler ./cmd/scheduler

# Configure minimum required env vars
export SCHEDULER_CHAIN=ethereum
export SCHEDULER_NETWORK=mainnet
export DEFRA_KEYRING_SECRET=your-keyring-secret

# Run
./scheduler --config config/config.yaml
```

Or with Docker:

```bash
docker compose up --build
```

## Documentation

- [Getting Started](docs/getting-started.md) — setup, configuration, and first run
- [API Reference](docs/api-reference.md) — endpoints and WebSocket protocol

## Configuration

All settings live in `config/config.yaml` and can be overridden with environment variables. See the [Getting Started](docs/getting-started.md) guide for the full configuration reference.

## Testing

```bash
# Unit tests
go test ./...

# Integration tests
bash test/run_integration.sh
```

## License

[MIT](LICENSE)
