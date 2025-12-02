-- Initial schema for prompt gateway

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Policies table
CREATE TABLE policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    pattern_type VARCHAR(50) NOT NULL,  -- 'regex', 'keyword'
    pattern_value TEXT NOT NULL,
    severity VARCHAR(20) NOT NULL,      -- 'low', 'medium', 'high', 'critical'
    action VARCHAR(20) NOT NULL,        -- 'log', 'block', 'redact'
    enabled BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Audit logs table
CREATE TABLE audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id UUID NOT NULL,
    client_id VARCHAR(255),
    prompt_hash VARCHAR(64),
    response_hash VARCHAR(64),
    policies_triggered UUID[],
    action_taken VARCHAR(20),
    latency_ms INTEGER,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Indexes
CREATE INDEX idx_audit_logs_client ON audit_logs(client_id);
CREATE INDEX idx_audit_logs_created ON audit_logs(created_at DESC);
CREATE INDEX idx_policies_enabled ON policies(enabled) WHERE enabled = true;

-- Seed some sample policies
INSERT INTO policies (name, description, pattern_type, pattern_value, severity, action) VALUES
    ('Prompt Injection - Ignore', 'Detects ignore previous instructions pattern', 'regex', '(?i)ignore\s+(previous|above|all)\s+(instructions|prompts)', 'high', 'block'),
    ('Prompt Injection - System', 'Detects system prompt extraction attempts', 'regex', '(?i)(show|reveal|print|display)\s+(system\s+prompt|instructions)', 'high', 'block'),
    ('PII - Email', 'Detects email addresses', 'regex', '[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}', 'medium', 'redact'),
    ('Jailbreak - DAN', 'Detects DAN jailbreak attempts', 'keyword', 'DAN', 'high', 'block'),
    ('Sensitive - API Key', 'Detects potential API keys', 'regex', '(?i)(api[_-]?key|secret[_-]?key)\s*[:=]\s*\S+', 'critical', 'block');
