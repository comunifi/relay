# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Nostr relay written in Go that also processes Ethereum user operations (ERC-4337). It combines:
- A Nostr relay (using khatru/fiatjaf libraries) for decentralized messaging
- NIP-29 group enforcement for closed groups with admin/member roles
- NIP-28 channel support within groups
- EVM integration for processing user operations and blockchain interactions
- Blossom media storage via S3-compatible backends

## Build and Run Commands

```bash
# Run the main relay with all services (API, indexer, queue processing)
go run cmd/main.go -env .env

# Run the stripped-down relay-only mode (Nostr relay + NIP-29 groups)
go run cmd/relay/main.go -env ../.env

# Run with Docker
docker-compose up db relay

# Run tests
go test ./...

# Run a single test file
go test ./internal/groups/...

# Run a specific test
go test -run TestChannelCreate ./internal/groups/...
```

## Command Line Flags

For `cmd/main.go`:
- `-port`: API port (default: 3001)
- `-env`: Path to .env file (default: `.env`)
- `-polling`: Use HTTP polling instead of WebSocket for EVM
- `-noindex`: Disable the blockchain indexer
- `-buffer`: User operation queue buffer size (default: 1000)
- `-notify`: Enable Discord webhook notifications

For `cmd/relay/main.go`:
- `-env`: Path to .env file (default: `.env`)

## Architecture

### Entry Points (cmd/)
- **cmd/main.go**: Full relay with API server (port 3001), Nostr relay (port 3334), indexer, and queue services
- **cmd/relay/main.go**: Lightweight Nostr-only relay (port 3334) with NIP-29 groups

### Core Services (internal/)
- **groups/nip29.go**: NIP-29 closed group enforcement with validation hooks and relay-generated metadata events (kinds 39000-39004)
- **hooks/hooks.go**: Khatru relay hooks connecting Nostr events to user operation processing
- **queue/**: Message queue system for processing user operations and push notifications in batches
- **indexer/**: Blockchain event indexer that listens to configured contract events
- **api/**: REST API server using chi router with JSON-RPC handlers for paymaster and chain operations
- **nostr/**: Nostr service for publishing events and managing relay connections
- **blossom/**: Media upload service implementing Blossom protocol with S3 backend

### Data Layer
- **PostgreSQL** for both Nostr events (via fiatjaf/eventstore) and application data
- **internal/db/**: Custom tables for events, sponsors, push tokens, and log data
- Tables are created dynamically per chain ID and contract address

### Key Interfaces (pkg/relay/)
- `EVMRequester`: Interface for EVM RPC calls
- `Message`: Queue message type with response channel
- `Processor`: Interface for queue batch processing

## Nostr Event Kinds

NIP-29 moderation events: 9000-9009, 9021-9022
Group content events: 9-12 (with `h` tag)
Relay metadata events: 39000 (group), 39001 (admins), 39002 (members), 39004 (channels)
NIP-28 channels: 40 (create), 41 (metadata), 42 (message)

## Environment Variables

Required variables are defined in `internal/config/config.go`. Key ones:
- `RELAY_URL`, `RELAY_PRIVATE_KEY`: Nostr relay configuration
- `RPC_URL`, `RPC_WS_URL`: Ethereum RPC endpoints
- `DB_*`: PostgreSQL connection settings
- `AWS_*`: S3 credentials for Blossom media storage (optional)

## Testing

The codebase uses standard Go testing with testify assertions. Tests for NIP-29 groups are in `internal/groups/nip29_channels_test.go`.
