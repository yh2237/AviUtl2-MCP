# 内部プロトコル

Goサーバーは既定で`127.0.0.1:28552`へ接続します。通信は「4バイトlittle-endianの長さ + UTF-8 JSON」で、上限は4 MiBです。

```json
{
  "id": 1,
  "version": 1,
  "method": "update_object",
  "context": {
    "session_id": "session",
    "generation": 4,
    "scene_id": 0
  },
  "params": { "object_id": 12 }
}
```

成功時は`result`、失敗時は`error`を返します。変更操作では`session_id`、`generation`、`scene_id`を照合し、古いコンテキストからの編集を拒否します。

`object_id`はブリッジ内の一時IDです。プロジェクトとシーンの変更で失効し、同じプロセス内で再利用しません。失効したIDは`STALE_OBJECT`になります。

応答が4 MiBを超えると`RESPONSE_TOO_LARGE`を返します。取得件数、aliasデータ、プレビューサイズを減らして再要求してください。

Go側で通信をキャンセルすると、待機中のI/Oを中断して接続を閉じます。次の要求で再接続します。

## 読み取り

`get_context`、`inspect_scene`、`inspect_timeline`、`inspect_object`、`inspect_object_values`、`get_selection`、`preflight_media`、`get_markers`、`get_bpm_grid`、`diagnostics`、`list_scenes`、`list_output_plugins`、`get_output_status`が読み取りです。

`inspect_scene`は1回で`context`、レイヤーごとの`object_count`と`first_frame` / `last_frame`、`color`、`markers`、`bpm`を返します。object本体を返さないため、シーン全体の把握とポーリングを軽くできます。

objectとエフェクトの`native_id`はSDKの64bit IDを10進文字列で返します。編集にはブリッジ側の`id`（`object_id`）を使います。

## レイヤーの背景色

`inspect_timeline`と`find_layers`の`layers[].color`で`RRGGBBAA`を返します。既定色の場合はキーを省略します。

設定はbatch操作`set_layer_state`の`layer_color`で行います。空文字は既定色へ戻す指定で、キーがない場合は変更しません。8桁のうちアルファ値が`ff`以外の場合は既定色として扱われます。

## 本体バージョン

`diagnostics`は`host_version`（数値と`host_version_text`）と`capabilities`を返します。各capabilityは`supported`と`requires_aviutl2_version`を持ちます。

```json
{
  "host_version": 2011500,
  "host_version_text": "2.1.15",
  "capabilities": {
    "layer_color": {
      "supported": true,
      "requires_aviutl2_version": 2011300,
      "requires_aviutl2_version_text": "2.1.13"
    }
  }
}
```

## プロジェクト未作成の扱い

AviUtl2は、プロジェクトを開くまで編集データを初期化しません。その状態でSDKの編集情報へアクセスすると本体が不正参照で終了します。ブリッジはプロジェクト読み込みコールバックで利用可能になったことを記録し、それまではSDKに触れずに`NO_PROJECT`（`retryable: true`）を返します。`ping`、`diagnostics`、`list_output_plugins`はこの状態でも応答します。

## 変更操作

`execute_batch`は最大100操作を1つの編集区間で実行します。objectの作成・複製、設定変更、区間、レイヤー、シーン設定、マーカー、BPMを同じbatchに含められます。Go側は実行前にobject、素材、エフェクト項目、配置衝突を検査します。

`select_scene`、`create_scene`、`create_project`、`open_project`、`save_project`、`output_file`は通常の編集batchとは別に、読み取り区間でコンテキストを照合した後、SDKの読み取り・編集ロックの外で実行します。`context.scene_id`は変更前の照合用で、対象IDとは区別します。

`set_object_flags`はbatch操作で、指定したフラグだけを変更します。参照時はSDKのnative IDも照合し、UIでの削除やハンドルの再利用を検出すると`STALE_OBJECT`を返します。

`output_file`は任意の`save_project_file`へ保存してから出力を開始し、`job`を返します。`get_output_status`は`params.job_id`（省略時は最新）に対応します。`CHANGE_EDIT_STATE`イベントと状態取得で`starting`、`running`、`ended`を記録します。出力開始の拒否は`rejected`です。SDKは出力の成否を返さないため、開始後の`outcome`は`unknown`です。

安定版前の内部仕様です。正確な定義は`internal/protocol`を参照してください。
