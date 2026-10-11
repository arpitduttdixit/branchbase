# BranchBase Risk Gate

> Automated schema migration risk classification with typed decision intelligence and offline fallback.

BranchBase Risk Gate (`branchbase risk`) assesses changed SQL migrations against repository policy. The asynchronous Git hook evaluates migrations before prewarming a branch database; `branchbase risk check` provides synchronous exit codes for local scripts and CI pipelines. Note that the Git `post-checkout` hook executes after checkout has completed and cannot cancel the checkout itself.

---

## Key Principles

1. **Deterministic Decisions, Configurable Actions:** The classifier determines technical risk and confidence, while `.branchbase/risk-policy.yml` decides whether to allow, warn, confirm, or block.
2. **Offline-First Resilience:** Works offline using a lightweight statement splitter and deterministic rules. It is not a full dialect AST parser. SQL statements it cannot classify are escalated to HIGH for manual review. Remote decision models (TypeSafe Jev / OpenRouter) enrich analysis when network connectivity and API keys are present.
3. **Local Analysis Cache:** Results are cached by `SHA256(migration_sql + table_schema_signature)` in `.branchbase/cache/risk_cache.json`; cache hits return in under 1ms without repeating analysis.
4. **Foreign Key Awareness:** When the configured active database is reachable, BranchBase introspects its catalog for inbound foreign keys. If database catalog metadata is unavailable, a dropped column is marked as at least HIGH because FK impact cannot be verified.

---

## Configuration (`.branchbase/risk-policy.yml`)

```yaml
version: 1

# Actions: allow | warn | confirm | block
on_risk:
  LOW: allow
  MEDIUM: warn
  HIGH: confirm
  CRITICAL: block

# Auto-escalate if confidence is below threshold
confidence_threshold: 0.75

# Fallback when AI provider is unreachable
on_jev_unavailable: warn

# Immediate critical rules
always_critical:
  - "DROP TABLE"
  - "TRUNCATE"
  - "DROP DATABASE"
  - "DROP SCHEMA"

exclude_paths:
  - "migrations/seeds/**"
  - "migrations/test_data/**"

report_format: pretty
save_report: .branchbase/last-risk-report.json
```

---

## CLI Usage

### Analyze a Migration File
```bash
# Terminal human-readable report
branchbase risk analyze migrations/003_drop_email.sql

# Structured JSON output
branchbase risk analyze migrations/003_drop_email.sql --json

# Force offline evaluation
branchbase risk analyze migrations/003_drop_email.sql --offline

# GitHub Actions workflow annotations
branchbase risk analyze migrations/003_drop_email.sql --github
```

### Check Pending Migrations
```bash
# Check migrations on current branch against default branch
branchbase risk check

# Check migrations for a specific branch
branchbase risk check --branch feature/drop-email

# Use custom policy file
branchbase risk check --policy custom-policy.yml

# Bypass blocking actions (caution: forces execution)
branchbase risk check --force

# Force offline heuristic evaluation
branchbase risk check --offline

# Output report as JSON
branchbase risk check --json
```

### Exit Codes
- `0`: Permitted (LOW / MEDIUM risk, confirmed HIGH, or bypassed with `--force`).
- `1`: Blocked (CRITICAL risk, user declined interactive confirmation, or non-interactive shell without `--force`).
- `2`: CLI usage or argument syntax error.
- `3`: File read I/O error or internal analysis failure.
