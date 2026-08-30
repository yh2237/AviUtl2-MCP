# AviUtl2 MCP

AviUtl2をMCPから操作するためのサーバーです。Go製MCPサーバーと公式SDKを使う小さなC++プラグインで構成してます。

```text
MCPクライアント --stdio--> aviutl2-mcp.exe --TCP--> aviutl2-mcp-bridge.aux2 --> AviUtl2
```

タイムラインの取得、複製・整列・間隔調整、テキスト・メディア・エフェクトの編集、一括操作、複数フレームのプレビューに対応しています。まだ実機検証中のため重要なプロジェクトではバックアップを取ってください。

## インストール

Releaseの`AviUtl2-MCP-<version>.au2pkg.zip`をAviUtl2のプレビュー画面へD&Dします。その後、MCPクライアントから次の実行ファイルを起動するよう設定します。

```text
Plugin\AviUtl2-MCP\aviutl2-mcp.exe
```

環境変数:

- `AVIUTL2_MCP_BRIDGE_ADDR`: 接続先。既定値は`127.0.0.1:28552`
- `AVIUTL2_MCP_BRIDGE_TIMEOUT`: タイムアウト。既定値は`5s`

## 開発

```powershell
go test ./...
.\scripts\package.ps1 `
  -Version 0.1.0 `
  -AviUtl2SdkDir C:\path\to\aviutl2_sdk\include\aviutl2_sdk
```

- [MCPツール](docs/TOOLS.md)
- [開発・ビルド](docs/DEVELOPMENT.md)
- [内部プロトコル](docs/PROTOCOL.md)
