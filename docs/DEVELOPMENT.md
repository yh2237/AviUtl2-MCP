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

## CIとリリース

通常のpushではCIを実行しません。`CI`ワークフローは手動実行できます。

`v*`タグをpushすると、最新のSDKミラーでGo/C++を検証します。全テスト成功後に限り、`.au2pkg.zip`をGitHub Releaseへ公開します。失敗時はタグだけが残ります。

## 設計上の境界

C++側はSDKとIPC、Go側はMCP・検証・PNG変換を担当します。object IDはプロセス内の一時IDで、プロジェクトまたはシーン変更時に失効します。
