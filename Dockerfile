FROM golang:1.25.5 AS builder

ARG BUILD_DATE
ARG VCS_REF
ARG VERSION=dev

RUN apt-get update && apt-get install -y \
    git \
    ca-certificates \
    tzdata \
    make \
    build-essential \
    pkg-config \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY go.mod go.sum ./

# Copy the local SDK so the replace directive resolves inside Docker.
COPY ../shinzo-app-sdk /shinzo-app-sdk

RUN go mod download && go mod verify

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-X main.version=${VERSION} -X main.buildDate=${BUILD_DATE} -X main.gitCommit=${VCS_REF}" \
    -o /bin/scheduler \
    ./cmd/scheduler

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y \
    ca-certificates \
    tzdata \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -r -u 1001 -g root scheduler

WORKDIR /app

COPY --from=builder /bin/scheduler /app/scheduler
COPY config/config.yaml /app/config/config.yaml

RUN mkdir -p /app/.scheduler && chown -R scheduler:root /app

USER scheduler

EXPOSE 8090

ENTRYPOINT ["/app/scheduler"]
CMD ["--config", "/app/config/config.yaml"]
