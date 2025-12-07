# Blossom Media Upload

This relay supports the [Blossom protocol](https://github.com/hzrd149/blossom) for media storage, with S3 as the backend and NIP-29 group-based organization.

The relay serves both nostr websocket connections and Blossom HTTP endpoints on the same port (3334).

## Upload Flow

### 1. Calculate the file hash

```javascript
const fileBuffer = await file.arrayBuffer();
const hashBuffer = await crypto.subtle.digest('SHA-256', fileBuffer);
const sha256 = Array.from(new Uint8Array(hashBuffer))
  .map(b => b.toString(16).padStart(2, '0'))
  .join('');
```

### 2. Create and sign an authorization event (kind 24242)

```javascript
const authEvent = {
  kind: 24242,
  created_at: Math.floor(Date.now() / 1000),
  tags: [
    ["t", "upload"],                    // Action type
    ["x", sha256],                      // File hash
    ["h", "your-group-id"],             // Group ID (NIP-29) - required for group uploads
    ["expiration", String(Math.floor(Date.now() / 1000) + 300)]  // REQUIRED: expires in 5 min
  ],
  content: ""
};

// Sign the event with the user's nostr private key
const signedAuth = await nostr.signEvent(authEvent);
```

### 3. Upload the file via HTTP PUT

```javascript
const response = await fetch(`https://your-relay.com/upload`, {
  method: 'PUT',
  headers: {
    'Content-Type': file.type,
    'Authorization': `Nostr ${btoa(JSON.stringify(signedAuth))}`
  },
  body: file
});

const result = await response.json();
// result.url contains the blob URL: https://your-relay.com/{sha256}
```

### 4. Retrieve the blob

```
GET https://your-relay.com/{sha256}
GET https://your-relay.com/{sha256}.jpg  // with extension
```

## Complete Example (TypeScript)

```typescript
import { finalizeEvent } from 'nostr-tools';

async function uploadImage(file: File, groupId: string, privateKey: string) {
  // 1. Calculate SHA-256 hash
  const buffer = await file.arrayBuffer();
  const hashBuffer = await crypto.subtle.digest('SHA-256', buffer);
  const sha256 = Array.from(new Uint8Array(hashBuffer))
    .map(b => b.toString(16).padStart(2, '0'))
    .join('');

  // 2. Create auth event
  const authEvent = finalizeEvent({
    kind: 24242,
    created_at: Math.floor(Date.now() / 1000),
    tags: [
      ["t", "upload"],
      ["x", sha256],
      ["h", groupId],  // Your NIP-29 group ID
      ["size", String(file.size)],
    ],
    content: "",
  }, privateKey);

  // 3. Upload
  const relayUrl = "https://your-relay.com";
  const response = await fetch(`${relayUrl}/upload`, {
    method: "PUT",
    headers: {
      "Content-Type": file.type || "application/octet-stream",
      "Authorization": `Nostr ${btoa(JSON.stringify(authEvent))}`,
    },
    body: file,
  });

  if (!response.ok) {
    throw new Error(`Upload failed: ${response.statusText}`);
  }

  const result = await response.json();
  return result.url; // e.g., "https://your-relay.com/abc123..."
}
```

## Auth Event Tags

| Tag | Required | Description |
|-----|----------|-------------|
| `t` | Yes | Action type: `upload`, `get`, `list`, or `delete` |
| `x` | Yes | SHA-256 hash of the file |
| `expiration` | **Yes** | Unix timestamp when auth expires (returns 404 if missing!) |
| `h` | For groups | NIP-29 group ID - stores under group folder |
| `size` | No | File size hint |

## Validation

The relay validates uploads against:

1. **Authentication**: The signed nostr event proves the user's identity
2. **Group membership**: If `h` tag is present, verifies the pubkey is a member of that NIP-29 group
3. **File size**: Must be under **50MB**
4. **Hash match**: The uploaded file's hash must match the `x` tag

## Storage Location

Files are stored in S3 based on the `h` tag:

- **With group ID**: `s3://bucket/blobs/{groupId}/{sha256}`
- **Without group ID**: `s3://bucket/blobs/{sha256}`

## Other Operations

### List blobs

```javascript
const authEvent = finalizeEvent({
  kind: 24242,
  tags: [["t", "list"]],
  content: "",
}, privateKey);

const response = await fetch(`${relayUrl}/list/${pubkey}`, {
  headers: {
    "Authorization": `Nostr ${btoa(JSON.stringify(authEvent))}`,
  },
});
```

### Delete a blob

```javascript
const authEvent = finalizeEvent({
  kind: 24242,
  tags: [
    ["t", "delete"],
    ["x", sha256],
  ],
  content: "",
}, privateKey);

const response = await fetch(`${relayUrl}/${sha256}`, {
  method: "DELETE",
  headers: {
    "Authorization": `Nostr ${btoa(JSON.stringify(authEvent))}`,
  },
});
```

