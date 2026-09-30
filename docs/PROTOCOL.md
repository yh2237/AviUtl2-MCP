# 内部プロトコル

Goサーバーは既定で`127.0.0.1:28552`へ接続します。通信形式は「4バイトlittle-endianの長さ + UTF-8 JSON」で、上限は4 MiBです。

```json
{
  "id": 1,
  "version": 1,
  "method": "delete_object",
  "context": {
    "session_id": "session",
    "generation": 4,
    "scene_id": 0
  },
  "params": { "object_id": 12 }
}
```

成功時は`result`、失敗時は`error`を返します。変更操作では`session_id`、`generation`、`scene_id`を照合し、古いコンテキストからの編集を拒否します。

`object_id`はブリッジ独自の一時IDです。プロジェクト・シーン変更時に失効し、同一プロセス内では再利用しません。失効したIDの参照は`STALE_OBJECT`になります。

応答が4 MiBを超える場合は`RESPONSE_TOO_LARGE`を返します。取得対象の数、aliasデータ、プレビューサイズを減らして再要求してください。

Go側で通信をキャンセルすると、待機中のI/Oを中断して接続を閉じます。次の要求で再接続します。

`execute_batch`は最大100操作を1つの編集区間で実行します。object作成・複製、設定変更、中間点、レイヤー、シーン、マーカー、BPMなどを同じbatchに含められます。Go側は実行前にobject、素材、エフェクト項目、配置衝突を検査します。

読み取りにはマーカー、BPM、モジュール診断、`list_scenes`、`list_output_plugins`、`get_output_status`を含みます。objectとエフェクトの`native_id`はSDKの64bit IDを10進文字列で返します。コンテキストの`background`は8桁の`RRGGBBAA`です。

`select_scene` / `create_scene` / `create_project` / `open_project` / `save_project` / `output_file`は通常の編集batchとは別に、読み取り区間でコンテキストを照合した後、SDKの読み取り／編集ロックの外で実行します。`select_scene`の対象は`params.target_scene_id`または`params.name`です。`context.scene_id`は変更前の照合用で、対象IDとは区別します。

`set_object_flags`はbatch操作で、`flags`の指定項目だけを変更します。参照時はSDKのnative IDも照合し、UI削除やハンドルの再利用を検出した場合に`STALE_OBJECT`を返します。

`output_file`は任意の`save_project_file`へ保存後、出力を開始し`job`を返します。`get_output_status`は`params.job_id`（省略時は最新）に対応します。`CHANGE_EDIT_STATE`イベントと状態取得で`starting`→`running`→`ended`を記録します。出力開始の拒否は`rejected`です。SDKは出力の成否を返さないため、開始後の`outcome`は`unknown`です。

Undo実行、トラック値の組み立てはプロトコルで代替しません。

安定版前の内部仕様です。正確な定義は`internal/protocol`を参照してください。
