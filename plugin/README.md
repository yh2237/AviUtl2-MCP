# ネイティブブリッジ

AviUtl2公式Plugin SDKとGo製MCPサーバーをつなぐ`.aux2`プラグインです。SDK呼び出し、loopback IPC、値の変換を担当します。

## ビルド

必要なもの:

- Windows x64
- MSVC（C++20）
- CMake 3.24以上
- AviUtl2 Plugin SDK 2026-10-10以降（レイヤー背景色APIが必要）

```powershell
cmake -S plugin -B build/plugin `
  -DAVIUTL2_SDK_DIR=C:/path/to/aviutl2_sdk/include/aviutl2_sdk
cmake --build build/plugin --config Release
```

出力は`aviutl2-mcp-bridge.aux2`です。

新しいSDK関数は`EDIT_SECTION`と`EDIT_HANDLE`の末尾に追記されます。古い本体では関数ポインタ自体が存在しないため、`InitializePlugin`で受け取った本体バージョンで判定してから呼び出します。詳細は[開発ドキュメント](../docs/DEVELOPMENT.md)を参照してください。
