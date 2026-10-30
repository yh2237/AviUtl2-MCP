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

`diagnostics`は`host_version`（数値と`host_version_text`）と`capabilities`を返します。`capabilities`の各機能は`supported`と`requires_aviutl2_version`を持ち、ブリッジは`InitializePlugin`で受け取った本体バージョンが要件以上の場合のみ該当するSDK関数を呼びます。古い本体では関数ポインタ自体が存在しないため、この判定は省略できません。

```json
{
  "host_version": 2011300,
  "host_version_text": "2.1.13",
  "capabilities": {
    "layer_color": {
      "supported": true,
      "requires_aviutl2_version": 2011300,
      "requires_aviutl2_version_text": "2.1.13"
    }
  }
}
```

レイヤーの背景色は`inspect_timeline`の`layers[].color`で`RRGGBBAA`を返します。既定色の場合はキーを省略します。設定はbatch操作`set_layer_state`の`layer_color`で行います。空文字は既定色へ戻す指定で、キー自体が無い場合は色を変更しません。8桁のうちアルファ値が`ff`以外の場合は既定色として扱われます（SDK仕様）。AviUtl2 2.1.13未満では`UNSUPPORTED_VERSION`を返します。

`context.high_dpi_ratio`はAviUtl2 2.1.14以降でのみ返します。

`inspect_scene`は1回の読み取りで、`context`、レイヤー毎の`object_count`と`first_frame` / `last_frame`、`color`、`markers`、`bpm`を返します。`inspect_timeline`と違いobject本体を返さないため、シーン全体の把握とGo側のポーリングを軽くできます。

`select_scene` / `create_scene` / `create_project` / `open_project` / `save_project` / `output_file`は通常の編集batchとは別に、読み取り区間でコンテキストを照合した後、SDKの読み取り／編集ロックの外で実行します。`select_scene`の対象は`params.target_scene_id`または`params.name`です。`context.scene_id`は変更前の照合用で、対象IDとは区別します。

`set_object_flags`はbatch操作で、`flags`の指定項目だけを変更します。参照時はSDKのnative IDも照合し、UI削除やハンドルの再利用を検出した場合に`STALE_OBJECT`を返します。

`output_file`は任意の`save_project_file`へ保存後、出力を開始し`job`を返します。`get_output_status`は`params.job_id`（省略時は最新）に対応します。`CHANGE_EDIT_STATE`イベントと状態取得で`starting`→`running`→`ended`を記録します。出力開始の拒否は`rejected`です。SDKは出力の成否を返さないため、開始後の`outcome`は`unknown`です。

Undo実行、トラック値の組み立てはプロトコルで代替しません。

安定版前の内部仕様です。正確な定義は`internal/protocol`を参照してください。
