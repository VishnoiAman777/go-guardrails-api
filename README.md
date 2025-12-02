# Prompt Analysis Gateway

A high-throughput API gateway that analyzes prompts/responses flowing to LLMs, detects potential security issues, and logs everything for audit.

## Project Overview

Build a security gateway that:
1. Receives prompt/response pairs via API
2. Analyzes them against configurable security policies
3. Returns allow/block/redact decisions
4. Logs all activity for audit

## Getting Started

```bash
# Start dependencies
docker-compose up -d

# Run migrations
psql $DATABASE_URL -f migrations/001_initial.sql

# Run the service
go run cmd/gateway/main.go
```

## Environment Variables

```
DATABASE_URL=postgres://postgres:postgres@localhost:5432/gateway?sslmode=disable
REDIS_URL=redis://localhost:6379
PORT=8080
LOG_LEVEL=debug
```

## API Specification

### POST /v1/analyze

Analyze a prompt/response pair against security policies.

**Request:**
```json
{
  "client_id": "string",
  "prompt": "string",
  "response": "string (optional)",
  "context": {
    "model": "string",
    "session_id": "string"
  }
}
```

**Response:**
```json
{
  "request_id": "uuid",
  "allowed": true,
  "action": "allow | block | redact",
  "triggered_policies": [
    {
      "policy_id": "uuid",
      "policy_name": "string",
      "severity": "low | medium | high | critical",
      "matched_pattern": "string"
    }
  ],
  "redacted_prompt": "string (if action is redact)",
  "latency_ms": 0
}
```

### GET /v1/policies

List all active policies.

**Response:**
```json
{
  "policies": [
    {
      "id": "uuid",
      "name": "string",
      "pattern_type": "regex | keyword",
      "severity": "string",
      "action": "log | block | redact",
      "enabled": true
    }
  ]
}
```

### POST /v1/policies

Create a new policy.

**Request:**
```json
{
  "name": "string",
  "description": "string",
  "pattern_type": "regex | keyword",
  "pattern_value": "string",
  "severity": "low | medium | high | critical",
  "action": "log | block | redact"
}
```

### GET /v1/health

Health check endpoint.

**Response:**
```json
{
  "status": "healthy",
  "timestamp": "ISO8601",
  "version": "string"
}
```

## Day 1 Goals

- [ ] HTTP server with routing
- [ ] Database connection and models
- [ ] Implement all 4 endpoints
- [ ] Basic pattern matching (regex, keyword)
- [ ] Error handling

## Day 2 Goals

- [ ] In-memory policy cache with background refresh
- [ ] Concurrent policy matching
- [ ] Async audit logging (don't block requests)
- [ ] Connection pooling optimization
- [ ] Request timeouts and context propagation

## Day 3 & 4 Goals

- [ ] Cache invalidation
- [ ] Load testing (target: 10K req/sec, p99 < 50ms)
- [ ] Profiling and optimization
- [ ] Prometheus metrics
- [ ] Graceful shutdown
- [ ] Failure scenario handling

## Testing

```bash
# Run tests
go test ./...

# Load test (after installing k6)
k6 run tests/load.js
```