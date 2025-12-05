# System Design Decisions

## Architecture Overview

Three-tier caching with asynchronous writes to decouple database operations from request handling.

```
Read:  In-Memory → Redis → PostgreSQL
Write: Buffered Channel → Redis → PostgreSQL (batched every 60s)
```

---

## Core Design Decisions

### 1. **Three-Tier Cache for Reads**

All policy reads are served from in-memory cache (Go maps). Redis is the second tier, PostgreSQL is source of truth.

**Why:** Eliminate database calls from request path. Policies change infrequently but are read on every request.

**Trade-off:** 10-minute stale data acceptable for security policies.

---

### 2. **Async Audit Logging via Redis**

Audit logs go to buffered channel → worker goroutines → Redis → PostgreSQL (bulk sync every 60s).

**Why:** Database writes are expensive. Writing synchronously would require hundreds of database connections and block request handlers.

**Trade-off:** Audit logs appear in PostgreSQL within 60 seconds. If Redis crashes, lose up to 60 seconds of logs.

---

### 3. **Bulk PostgreSQL Inserts**

Use PostgreSQL COPY instead of individual INSERTs for audit logs (6M rows per batch).

**Why:** Individual INSERTs would take hours. COPY completes in seconds.

**Implementation:** `pq.CopyIn()` with transaction wrapping all rows.

---

## Configuration Design Decisions

### **Redis Pool Configuration**

```env
REDIS_POOL_SIZE=200
REDIS_MIN_IDLE=100
REDIS_POOL_TIMEOUT=2
REDIS_MAX_RETRIES=2
```

#### **REDIS_POOL_SIZE=200**

**Calculation:**
```
Target Throughput: 100,000 req/sec
Redis Operation Latency: ~1ms (network round-trip)
Required Concurrent Connections: 100,000 × 0.001 = 100 connections

Adding 100% buffer for:
- Burst traffic handling
- Audit log workers (100 workers writing to Redis)
- Policy cache operations

Total: 100 + 100 = 200 connections
```

**Why Not More?**
- Each connection consumes memory (~64KB per connection)
- 200 connections = ~13MB memory overhead (acceptable)
- Beyond 200, diminishing returns (network becomes bottleneck, not connections)

**Why Not Less?**
- At 100 connections, no buffer for traffic spikes
- Pool exhaustion would cause `REDIS_POOL_TIMEOUT` errors
- Workers would block waiting for available connections

---

#### **REDIS_MIN_IDLE=100**
---

## Configuration Decisions

### **REDIS_POOL_SIZE=200**

Need 100 connections for concurrent request reads at peak load. Add 100 for audit workers writing to Redis simultaneously.

**Why not 100?** No buffer for traffic spikes or audit workers.  
**Why not 300?** Network becomes bottleneck before connections do. Wastes memory.

---

### **REDIS_MIN_IDLE=100**

Keep half the pool warm to avoid connection establishment overhead on every request.

**Trade-off:** ~6MB memory for instant connection availability vs establishing connections on-demand.

---

### **REDIS_POOL_TIMEOUT=2s**

Fail fast when pool is exhausted. If waiting 2+ seconds for Redis connection, system is overloaded.

**Why not 4s+?** Long waits cause cascading failures and poor user experience. Better to return 504 immediately.

---

### **REDIS_MAX_RETRIES=2**

Balance between handling transient network issues and preventing retry storms.

**Why not 0?** Network packet loss happens. One retry solves most issues.  
**Why not 3+?** At 100K req/sec, retries amplify load 3-4x during incidents.

---

### **REDIS_SYNC_INTERVAL=60**

Sync audit logs from Redis to PostgreSQL every 60 seconds.

**Trade-offs:**
- **30s:** More frequent DB writes, lower data loss risk, but 2x more load spikes
- **60s:** Chosen - balances batch efficiency with acceptable data staleness
- **120s:** Larger batches but higher risk of data loss if Redis fails

**Decision:** 60 seconds means max 1 minute of audit logs lost if Redis crashes. Acceptable for compliance.

---

### **AUDIT_BUFFER_SIZE=500000**

Buffered channel holds 5 seconds of peak traffic (100K req/sec × 5s).

**Why 5 seconds?** Absorbs temporary Redis slowdowns without blocking request handlers.  
**Memory cost:** ~250MB for buffer.

---

### **AUDIT_WORKERS=100**

100 goroutines writing audit logs to Redis in parallel. Matches target throughput of 100K logs/sec.

**Why 100?** Each worker handles ~1K logs/sec. 100 workers = 100K logs/sec capacity.  
**Why not 50?** Would bottleneck at 50K logs/sec.  
**Why not 200?** Goroutines would compete for Redis connections. Diminishing returns.

---

### **DB_MAX_OPEN_CONNS=5**

PostgreSQL is ONLY used by background sync worker. Requests never hit the database.

**Usage:**
- 1 connection for audit log bulk COPY (every 60s)
- 1 connection for policy sync (rare)
- 1 connection for startup
- 2 spare for cache misses

**Why not 50?** No request hits database. Only 1 worker thread needs 1 connection at a time.

---

### **DB_MAX_IDLE_CONNS=2**

Keep sync worker connection and policy connection warm.

**Why 2?** Database accessed every 60 seconds. Worth keeping 2 connections alive vs reconnecting each time.