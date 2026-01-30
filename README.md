<h1 align="center">
  Relay
</h1>

A nostr relay that can also process user ops

## Local Development Setup

### 1. Start the PostgreSQL database

```bash
# Start database using .env.local credentials
docker-compose --env-file .env.local up db
```

### 2. Run the relay

```bash
# Run the main relay with all services (API on port 3001, Nostr relay on port 3334)
go run cmd/main.go -env .env.local

# Or run the lightweight Nostr-only relay (port 3334)
go run cmd/relay/main.go -env ../.env.local
```

### Fresh Database Setup

If you need to reset the database:

```bash
docker-compose down
rm -rf .relay/data
docker-compose --env-file .env.local up db
```
