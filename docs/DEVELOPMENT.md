# Development

## Requirements

- Go 1.25 or newer
- Windows x64 and Visual Studio 2022 C++ tools
- CMake 3.24 or newer
- official AviUtl2 Plugin SDK headers

## Verification

```powershell
go test ./...
go vet ./...
cmake -S plugin -B build/plugin `
  -DAVIUTL2_SDK_DIR=C:\path\to\aviutl2_sdk\include\aviutl2_sdk
cmake --build build/plugin --config Release
ctest --test-dir build/plugin -C Release --output-on-failure
```

The C++ test includes the production dispatch implementation and supplies a
small fake `EDIT_HANDLE`. It checks both happy paths and stale-context rejection
without launching AviUtl2.

## Architecture boundaries

The native `.aux2` bridge owns SDK calls and loopback IPC. It does not interpret
MCP. The Go process owns MCP schemas, validation, workflow descriptions, image
encoding, and future policy. This keeps native code small and lets most behavior
be tested without loading the editor.

Do not persist object IDs. SDK object handles are represented by an in-memory
registry and discarded when project/scene lifecycle events change generation.
