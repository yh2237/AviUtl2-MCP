# Native bridge protocol

The Go server connects to `127.0.0.1:28552` by default. Each message consists of
a four-byte little-endian unsigned payload length followed by one UTF-8 JSON
document. Requests and responses are limited to 4 MiB.

A request contains a unique `id`, a `method`, optional `params`, and, for
mutations, an expected context:

```json
{
  "id": "1",
  "method": "delete_object",
  "context": {
    "session_id": "opaque-session",
    "generation": 4,
    "scene_id": 0
  },
  "params": { "object_id": "opaque-object" }
}
```

Successful responses contain a method-specific `result`. Errors contain an
`error` object with a stable `code`, human-readable `message`, retry hint, and
optional structured `details`.

The session changes when the native bridge is reloaded. The generation changes
when a project is loaded or the active scene changes. These tokens prevent a
delayed MCP call from editing a different context than the one inspected.

The wire schema is internal and may change before a stable release. Go DTOs in
`internal/protocol` are the source of truth.
