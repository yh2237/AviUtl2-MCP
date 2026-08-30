# Native bridge

This directory contains the small native half of AviUtl2 MCP. It is an
independent implementation against the official AviUtl2 Plugin SDK.

Responsibilities are intentionally limited to:

- plugin lifecycle and `EDIT_HANDLE` access;
- bounded, length-prefixed loopback IPC;
- conversion between SDK values and protocol DTOs;
- host-safe exception and socket handling.

MCP behavior and editing workflows belong in the Go process.

## Build requirements

- Windows x64
- MSVC with C++20 support
- CMake 3.24 or newer
- official AviUtl2 Plugin SDK headers

Configure with the directory that directly contains `plugin2.h`:

```powershell
cmake -S plugin -B build/plugin `
  -DAVIUTL2_SDK_DIR=C:/path/to/aviutl2_sdk/include/aviutl2_sdk
cmake --build build/plugin --config Release
```

The resulting file is named `aviutl2-mcp-bridge.aux2`.
