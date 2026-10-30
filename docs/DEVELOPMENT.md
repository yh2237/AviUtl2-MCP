# 開発

## 必要なもの

- Go 1.25以上
- Windows x64 / Visual Studio 2022
- CMake 3.24以上
- AviUtl2 Plugin SDK **2026-10-10以降**（レイヤー背景色APIが必要）

SDKは[aviutl2_sdk_mirror](https://github.com/aviutl2/aviutl2_sdk_mirror)の最新を使います。`plugin/CMakeLists.txt`が必要なAPIの有無を検査します。

## テスト

```powershell
go test ./...
go vet ./...
cmake -S plugin -B build/plugin `
  -DAVIUTL2_SDK_DIR=C:\path\to\aviutl2_sdk\include\aviutl2_sdk
cmake --build build/plugin --config Release
ctest --test-dir build/plugin -C Release --output-on-failure
```

C++テストは模擬`EDIT_HANDLE`を使うため、AviUtl2を起動せず実行できます。

## 実機スモークテスト

配置済みのサーバーとAviUtl2を起動してから実行します。読み取りのみの接続確認:

```powershell
go run ./scripts/live-smoke.go
```

専用プロジェクトで編集・シーン操作・保存／読み込み・WAV出力を検証する場合:

```powershell
go run ./scripts/live-smoke.go -edit `
  -work-dir C:\path\to\existing\test-folder `
  -output-plugin "WAVファイル出力 (16bit short)"
```

編集前に元のプロジェクトをスナップショットへ保存し、終了時に読み戻します。出力ではMCPプロセスを再起動してジョブ状態の保持を確認し、WAVの形式・サンプルレート・音声データも検査します。`-server`で配置先の実行ファイルを指定できます。中断時はログに表示された`-original.aup2`を本体で開くか、`-restore`で指定してください。

`-edit`は`inspect_scene`の内容も検証します。レイヤー背景色は`diagnose_connection`の`capabilities.layer_color.supported`を見て、対応していれば設定・取得・解除まで実行し、AviUtl2 2.1.13未満なら`SKIP`と表示して続行します。

## プロジェクト未作成の扱い

AviUtl2 2.1.11aでは、プロジェクトが開かれていない状態で`get_edit_state()`や`call_read_section_param()`を呼ぶと本体がアクセス違反（0xc0000005）で終了します。本体の起動直後はこの状態になることがあるため、ブリッジは`register_project_load_handler`でプロジェクトが利用可能になったことを記録し、それまではSDKへ触れずに`NO_PROJECT`を返します（`project_loaded`と`require_project()`）。実機テストでは、このガードがない場合に実際に本体が落ちることを確認しています。

シーン・プロジェクト・出力開始のSDK操作は、プラグインのメッセージ専用ウィンドウを経由して本体のメインスレッドで実行します。

## CIとリリース

通常のpushではCIを実行しません。`CI`ワークフローは手動実行できます。

`v*`タグをpushすると、最新のSDKミラーでGo/C++を検証します。全テスト成功後に限り、`.au2pkg.zip`をGitHub Releaseへ公開します。失敗時はタグだけが残ります。

`go build`だけでは`server_version`が`dev`になります。配布用は`scripts/package.ps1`を使い、`-X main.version`でバージョンを埋め込んでください（`.au2pkg.zip`も同じコマンドで作成されます）。

```powershell
.\scripts\package.ps1 -Version 0.2.0-test `
  -AviUtl2SdkDir .\build\aviutl2-sdk\include\aviutl2_sdk
```

`.au2pkg.zip`をプレビュー画面へD&Dすると、`Plugin\AviUtl2-MCP`が置き換わりパッケージ登録も更新されます。古いパッケージが登録されたままだと、再インストール時にそのバージョンへ戻るので注意してください。

## MCPツールの登録

ツールは`addTool`（`internal/mcpserver/server.go`）経由で登録します。`mcp.AddTool`を直接使うと、戻り値の型から出力スキーマが生成されます。ほぼ全てのツールが同じ大きな型（`protocol.Context`、object一覧、`[]protocol.BatchOperation`）を返すため、その分が`tools/list`で重複し、82ツールで約290KB（約7万トークン）になっていました。

`addTool`は出力スキーマを付けず、結果を`structuredContent`のJSONとして返します。これで`tools/list`は約42KB（約1.1万トークン）です。`TestToolSchemasStayCompact`が出力スキーマの再追加と64KiB超えを検出します。

```go
addTool(server, &mcp.Tool{Name: "get_context", Description: "..."},
    func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, contextOutput, error) {
        result, err := client.GetContext(ctx)
        return nil, contextOutput{Context: result}, err
    })
```

## 設計上の境界

C++側はSDKとIPC、Go側はMCP・検証・PNG変換を担当します。object IDはプロセス内の一時IDで、プロジェクトまたはシーン変更時に失効します。

## SDKの新機能を追加するとき

AviUtl2のSDKは本体リリースに合わせて週次で更新されます。`EDIT_SECTION` / `EDIT_HANDLE`などの関数テーブルには、新しい関数が**末尾に追記**されます。古い本体で新しい関数ポインタへ触れると不正なメモリ参照になるため、必ず本体バージョンで判定してください。

1. `build/aviutl2-sdk`の`CHANGELOG.md`と`git log`で追加APIを確認する。
2. 本体の更新履歴（[docs.aviutl2.jp/changelog](https://docs.aviutl2.jp/changelog)）で、そのAPIが入った本体バージョンを特定する。
3. `plugin/src/bridge.cpp`の`kRequiredVersion`付近に`k...Version`定数を追加し、`supports_...()`を書く。
4. 呼び出し前に`supports_...()`を判定し、未対応時は`UNSUPPORTED_VERSION`を返す。
5. `diagnostics`の`capabilities`へ対応状況を追加する。
6. 模擬`EDIT_SECTION`を使うC++テストで、対応時と未対応時の両方を検証する。

| 定数 | バージョン | API |
| --- | --- | --- |
| `kRequiredVersion` | 2.1.10 | シーン・プロジェクト・出力 |
| `kLayerColorVersion` | 2.1.13 | `get_layer_color` / `set_layer_color` |
| `kHighDpiRatioVersion` | 2.1.14 | `get_high_dpi_ratio` |

本体バージョン番号は`メジャー*1000000 + マイナー*10000 + ビルド*100 + 改訂`の形式で、2.1.13は`2011300`です。実際に動いている本体の値は`diagnose_connection`の`bridge.host_version`と`bridge.host_version_text`で確認できます。
