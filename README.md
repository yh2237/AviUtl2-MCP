# AviUtl2 MCP

A clean, independent MCP server for live AviUtl2 editing.

The MCP server and application logic are written in Go. A small native
`.aux2` plugin is responsible only for calling the official AviUtl2 Plugin SDK
and exposing those calls over a loopback connection.

```text
MCP client --stdio--> aviutl2-mcp.exe --TCP--> aviutl2-mcp-bridge.aux2 --> AviUtl2
```

## Status

Phases 0 through 4 are implemented. The server can inspect a project, perform
generation-guarded edits, batch related operations into one Undo unit, manage
effects, inspect selection and media, and return PNG previews to an MCP client.

The bridge has automated tests against a fake SDK host, but still needs broader
testing inside real AviUtl2 projects. Keep project backups and review generated
operations before allowing an agent to edit important work.

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

To build both programs and create an AviUtl2 package:

```powershell
.\scripts\package.ps1 `
  -Version 0.1.0 `
  -AviUtl2SdkDir C:\path\to\aviutl2_sdk\include\aviutl2_sdk
```

The package is written to `dist/AviUtl2-MCP-<version>.au2pkg.zip`. After
installing it in AviUtl2, configure the MCP client to start
`Plugin/AviUtl2-MCP/aviutl2-mcp.exe` beneath the AviUtl2 application-data
directory.

## Protocol

Each bridge message is a four-byte little-endian payload length followed by a
UTF-8 JSON document. Payloads are limited to 4 MiB. See
[`internal/protocol`](internal/protocol) for the source-of-truth DTOs and
framing behavior.

See [Tool reference](docs/TOOLS.md), [Bridge protocol](docs/PROTOCOL.md), and
[development notes](docs/DEVELOPMENT.md) for the implemented surface.
