# Database Schema

This document describes the PostgreSQL database schema used by the relay.

## Tables Overview

| Table | Description |
|-------|-------------|
| `event` | Nostr events (managed by fiatjaf/eventstore) |
| `t_events` | Blockchain event subscriptions |
| `t_logs_data` | Transaction log data cache |
| `t_sponsors_{chain_id}` | Sponsor keys per chain |
| `t_push_token_{chain_id}_{contract}` | Push notification tokens (created dynamically) |

## Table Schemas

### `event`

Stores Nostr events. Managed by the [fiatjaf/eventstore](https://github.com/fiatjaf/eventstore) PostgreSQL backend.

| Column | Type | Nullable | Description |
|--------|------|----------|-------------|
| id | text | NOT NULL | Event ID (unique, NIP-01) |
| pubkey | text | NOT NULL | Author's public key (hex) |
| created_at | integer | NOT NULL | Unix timestamp |
| kind | integer | NOT NULL | Event kind (NIP-01) |
| tags | jsonb | NOT NULL | Event tags array |
| content | text | NOT NULL | Event content |
| sig | text | NOT NULL | Schnorr signature |
| tagvalues | text[] | generated | Extracted tag values for indexing |

**Indexes:**
- `ididx` - Unique index on `id`
- `pubkeyprefix` - Index on `pubkey` for prefix matching
- `kindidx` - Index on `kind`
- `kindtimeidx` - Composite index on `(kind, created_at DESC)`
- `timeidx` - Index on `created_at DESC`
- `arbitrarytagvalues` - GIN index on `tagvalues`

### `t_events`

Stores blockchain event subscriptions that the indexer listens to.

| Column | Type | Nullable | Default | Description |
|--------|------|----------|---------|-------------|
| chain_id | text | NOT NULL | | Chain ID (e.g., "100" for Gnosis) |
| contract | text | NOT NULL | | Contract address |
| topic | text | NOT NULL | | Event topic hash (keccak256) |
| alias | text | NOT NULL | | Human-readable alias |
| event_signature | text | NOT NULL | | Solidity event signature |
| name | text | NOT NULL | | Event name |
| created_at | timestamp | NOT NULL | CURRENT_TIMESTAMP | |
| updated_at | timestamp | NOT NULL | CURRENT_TIMESTAMP | |

**Primary Key:** `(chain_id, contract, topic)`

**Indexes:**
- `idx_events_contract` - Index on `(chain_id, contract)`
- `idx_events_contract_signature` - Index on `(chain_id, contract, topic)`

### `t_logs_data`

Caches parsed transaction log data.

| Column | Type | Nullable | Default | Description |
|--------|------|----------|---------|-------------|
| hash | text | NOT NULL | | Transaction hash |
| data | jsonb | | | Parsed log data |
| created_at | timestamp | NOT NULL | CURRENT_TIMESTAMP | |
| updated_at | timestamp | NOT NULL | CURRENT_TIMESTAMP | |

**Primary Key:** `hash`

### `t_sponsors_{chain_id}`

Stores sponsor private keys for paymaster operations. Table name includes chain ID (e.g., `t_sponsors_100` for Gnosis).

| Column | Type | Nullable | Default | Description |
|--------|------|----------|---------|-------------|
| contract | text | NOT NULL | | Contract address |
| pk | text | NOT NULL | | Encrypted private key |
| created_at | timestamp | NOT NULL | CURRENT_TIMESTAMP | |
| updated_at | timestamp | NOT NULL | CURRENT_TIMESTAMP | |

**Primary Key:** `contract`

### `t_push_token_{chain_id}_{contract}`

Stores push notification tokens. Tables are created dynamically per chain and contract.

| Column | Type | Description |
|--------|------|-------------|
| account | text | Account address |
| token | text | Push notification token |
| created_at | timestamp | |
| updated_at | timestamp | |

## Dynamic Table Creation

The following tables are created automatically at runtime:

1. **Sponsor tables** (`t_sponsors_{chain_id}`) - Created when the relay starts for the configured chain
2. **Push token tables** (`t_push_token_{chain_id}_{contract}`) - Created when a new contract is registered for event indexing

Table creation logic is in `internal/db/db.go`.
