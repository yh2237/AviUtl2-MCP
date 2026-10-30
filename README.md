# AviUtl2 MCP

AviUtl2をMCPから操作するサーバーです。Go製のMCPサーバーと、公式SDKを使うC++プラグインで構成します。

```text
MCPクライアント --stdio--> aviutl2-mcp.exe --TCP--> aviutl2-mcp-bridge.aux2 --> AviUtl2
```

タイムラインの検索と分析、テキストや素材の追加、一括編集、整列、区間・マーカー・BPM操作、プレビュー比較、シーン・プロジェクト・出力、接続診断に対応します。編集ツールは`dry_run`と事前検査を備えます。

全体像は`inspect_scene`、変更前の確認は`dry_run`、複数操作は`execute_batch`を使ってください。

## インストール

Releaseの`.au2pkg.zip`をAviUtl2のプレビュー画面へD&Dします。MCPクライアントには次の実行ファイルを登録してください。

```text
Plugin\AviUtl2-MCP\aviutl2-mcp.exe
```

環境変数:

- `AVIUTL2_MCP_BRIDGE_ADDR`: 接続先。既定値は`127.0.0.1:28552`
- `AVIUTL2_MCP_BRIDGE_TIMEOUT`: タイムアウト。既定値は`5s`

AviUtl2 2.1.10以降が必要です。

## 開発

```powershell
go test ./...
.\scripts\package.ps1 -Version 0.3.0-test `
  -AviUtl2SdkDir C:\path\to\aviutl2_sdk\include\aviutl2_sdk
```

- [MCPツール](docs/TOOLS.md)
- [内部プロトコル](docs/PROTOCOL.md)
