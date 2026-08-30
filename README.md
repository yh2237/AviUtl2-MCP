# AviUtl2 MCP

A clean, independent MCP server for live AviUtl2 editing.

The MCP server and application logic are written in Go. A small native
`.aux2` plugin is responsible only for calling the official AviUtl2 Plugin SDK
and exposing those calls over a loopback connection.

```text
MCP client --stdio--> aviutl2-mcp.exe --TCP--> aviutl2-mcp-bridge.aux2 --> AviUtl2
```

## Status

Initial vertical slice:

- Go MCP server using the official MCP Go SDK
- bounded length-prefixed JSON bridge protocol
- `ping` and `get_context` tools
- Go mock-bridge tests
- native plugin skeleton using the official AviUtl2 SDK

This is not yet ready for normal editing work.

## Go server

```powershell
go test ./...
go run ./cmd/aviutl2-mcp
```

Environment variables:

- `AVIUTL2_MCP_BRIDGE_ADDR`: bridge address; default `127.0.0.1:28552`
- `AVIUTL2_MCP_BRIDGE_TIMEOUT`: Go duration; default `5s`

## Native plugin

The plugin requires a local copy of the official AviUtl2 Plugin SDK and a C++20
compiler. It deliberately does not use code from another AviUtl2 MCP project.

```powershell
cmake -S plugin -B build/plugin `
  -DAVIUTL2_SDK_DIR=C:/path/to/aviutl2_sdk/include/aviutl2_sdk
cmake --build build/plugin --config Release
```

The build fetches `nlohmann/json` as a third-party build dependency.

## Protocol

Each bridge message is a four-byte little-endian payload length followed by a
UTF-8 JSON document. Payloads are limited to 4 MiB. See
[`internal/protocol`](internal/protocol) for the source-of-truth DTOs and
framing behavior.
