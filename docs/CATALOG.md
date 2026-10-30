# AviUtl2 MCP

AviUtl2をMCPから操作するサーバーです。Go製のMCPサーバーと、公式SDKを使うC++プラグインで構成します。

タイムラインの検索と分析、テキストや素材の追加、一括編集、整列、区間・マーカー・BPM操作、プレビュー比較、シーン・プロジェクト・出力、接続診断に対応します。編集ツールは`dry_run`と事前検査を備えます。

ツールは用途別に40個へまとめています。全体像は`inspect_scene`、変更前の確認は`dry_run`、複数操作は`execute_batch`を使ってください。

MCPクライアントには次の実行ファイルを登録してください。

```text
...\Plugin\AviUtl2-MCP\aviutl2-mcp.exe
```

環境変数:

- `AVIUTL2_MCP_BRIDGE_ADDR`: 接続先。既定値は`127.0.0.1:28552`
- `AVIUTL2_MCP_BRIDGE_TIMEOUT`: タイムアウト。既定値は`5s`

AviUtl2 2.1.10以降が必要です。2.1.13以降ではレイヤーの背景色、2.1.14以降では`high_dpi_ratio`も使えます。

---

- [MCPツール](https://github.com/yh2237/AviUtl2-MCP/blob/main/docs/TOOLS.md)
- [旧ツールからの移行](https://github.com/yh2237/AviUtl2-MCP/blob/main/docs/MIGRATION.md)
- [開発・ビルド](https://github.com/yh2237/AviUtl2-MCP/blob/main/docs/DEVELOPMENT.md)
- [内部プロトコル](https://github.com/yh2237/AviUtl2-MCP/blob/main/docs/PROTOCOL.md)
