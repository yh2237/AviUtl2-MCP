# 開発

## 必要なもの

- Go 1.25以上
- Windows x64 / Visual Studio 2022
- CMake 3.24以上
- AviUtl2 Plugin SDK

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

シーン・プロジェクト・出力開始のSDK操作は、プラグインのメッセージ専用ウィンドウを経由して本体のメインスレッドで実行します。

## CIとリリース

通常のpushではCIを実行しません。`CI`ワークフローは手動実行できます。

`v*`タグをpushすると、最新のSDKミラーでGo/C++を検証します。全テスト成功後に限り、`.au2pkg.zip`をGitHub Releaseへ公開します。失敗時はタグだけが残ります。

## 設計上の境界

C++側はSDKとIPC、Go側はMCP・検証・PNG変換を担当します。object IDはプロセス内の一時IDで、プロジェクトまたはシーン変更時に失効します。
