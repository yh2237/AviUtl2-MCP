# ネイティブブリッジ

AviUtl2公式Plugin SDKとGo製MCPサーバーをつなぐ`.aux2`プラグインです。SDK呼び出し、loopback IPC、値の変換だけを担当します。

## ビルド

必要なもの:

- Windows x64
- MSVC（C++20）
- CMake 3.24以上
- AviUtl2 Plugin SDK

```powershell
cmake -S plugin -B build/plugin `
  -DAVIUTL2_SDK_DIR=C:/path/to/aviutl2_sdk/include/aviutl2_sdk
cmake --build build/plugin --config Release
```

出力は`aviutl2-mcp-bridge.aux2`です。
