# AviUtl2 MCP

AviUtl2をMCPから操作するサーバーです。Go製MCPサーバーと、公式SDKを使う小さなC++プラグインで構成しています。

```text
MCPクライアント --stdio--> aviutl2-mcp.exe --TCP--> aviutl2-mcp-bridge.aux2 --> AviUtl2
```

タイムラインの検索・分析、テキストや素材の追加、一括編集、整列、区間・マーカー・BPM操作、プレビュー比較、接続診断に対応します。変更前の検査と`dry_run`も利用できます。

## インストール

Releaseの`.au2pkg.zip`をAviUtl2のプレビュー画面へD&Dします。MCPクライアントには次の実行ファイルを登録してください。

```text
Plugin\AviUtl2-MCP\aviutl2-mcp.exe
```

環境変数:

- `AVIUTL2_MCP_BRIDGE_ADDR`: 接続先。既定値は`127.0.0.1:28552`
- `AVIUTL2_MCP_BRIDGE_TIMEOUT`: タイムアウト。既定値は`5s`

まだ実機検証中です。重要なプロジェクトはバックアップしてから利用してください。

## 開発

```powershell
go test ./...
.\scripts\package.ps1 -Version 0.0.3 `
  -AviUtl2SdkDir C:\path\to\aviutl2_sdk\include\aviutl2_sdk
```

- [MCPツール](docs/TOOLS.md)
- [開発・ビルド](docs/DEVELOPMENT.md)
- [内部プロトコル](docs/PROTOCOL.md)
