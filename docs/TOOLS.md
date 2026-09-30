# MCPツール

変更操作には、直前の`get_context`が返す`session_id`、`generation`、`scene_id`が必要です。object IDも同じコンテキスト内でのみ有効です。

## 取得・検索

- `ping` / `get_context` / `get_selection`: 接続状態、現在のシーン、選択を取得
- `inspect_timeline` / `inspect_object` / `inspect_objects`: タイムラインとobjectを取得
  - objectとエフェクトの`native_id`はSDK側の64bit IDを10進文字列で返します。編集には引き続きブリッジ側の`id`（`object_id`）を使います。
  - objectにはグループ・カメラ制御対象とクリッピングの`flags`を含みます。
- `inspect_object_values` / `inspect_section_values`: エフェクト値、トラック、区間ごとの値を取得
- `list_effects` / `list_effect_items`: 利用可能なエフェクトと項目を取得
- `find_objects`: 名前、エフェクト、素材種別・解像度・長さ、配置、選択で検索
- `summarize_timeline` / `find_gaps` / `find_overlaps`: タイムラインを分析
- `list_used_effects` / `list_used_media` / `find_missing_media`: 使用要素を集計・検査
- `find_out_of_scene_objects` / `find_layers` / `find_empty_layers`: 範囲外objectやレイヤーを検査
- `get_markers` / `get_bpm_grid`: マーカーと可変BPMグリッドを取得

## 基本編集

- `add_text` / `add_media` / `add_media_sequence`: テキストや素材を配置
- `update_object` / `delete_object` / `delete_objects`: objectを変更・削除
- `duplicate_objects` / `duplicate_pattern`: エフェクトを保ったまま複製
- `add_effect` / `delete_effect` / `set_effect_state`: エフェクトを編集
- `replace_media` / `replace_media_bulk` / `relink_media`: 素材を置換・再リンク
- `replace_text`: 複数テキストを検索置換
- `set_object_flags`: `object_ids` / `selected` / `focus`の対象へフラグを一括適用。`dry_run`と変更後objectの取得に対応

## 配置・長さ

- `shift_objects` / `align_objects` / `distribute_objects` / `stagger_objects`: 移動・整列
- `sequence_objects` / `pack_objects` / `normalize_gaps`: 連続配置と間隔調整
- `insert_time`: 指定位置へ空き時間を挿入
- `trim_objects` / `fit_objects` / `fit_objects_to_media`: 長さを調整
- `move_to_layers` / `name_layers`: レイヤーへ分類し命名
- `snap_objects_to_bpm`: BPMグリッドへ吸着

## 区間・マーカー・設定

- `split_objects` / `edit_sections`: 中間点を一括編集
- `edit_markers` / `split_at_markers` / `place_objects_at_markers`: マーカーを編集・利用
- `set_bpm_grid` / `set_bpm_grid_list`: 固定または可変BPMを設定
- `set_scene_settings`: 現在のシーン設定を変更
- `apply_properties`: object間で設定値やアニメーションを複製
- `set_track_values`: AviUtl2から取得済みのrawトラック値を適用
- `apply_animation_template`: テンプレートobjectのfade・slide・zoomを複製

## プレビュー・診断

- `render_preview` / `render_contact_sheet` / `render_range_contact_sheet`: PNGを取得
- `render_collision_sheet`: 重なり位置を一覧表示
- `capture_preview_snapshot` / `render_snapshot_comparison`: 編集前後を比較
- `render_change_comparison`: 2フレームを比較
- `preflight_media`: 素材を追加せず対応可否と情報を検査
- `diagnose_connection` / `get_server_log` / `reconnect_bridge`: 接続・SDK・直近通信を診断

## シーン・プロジェクト・出力

AviUtl2 **2.1.10以上** が必要です。

- `list_scenes`: ID・名前・現在のシーンとコンテキストを取得
- `select_scene`: `target_scene_id`または一意に一致する`name`で切替。`scene_id`は変更前コンテキストの照合用
- `create_scene`: `name`、任意の`label`で作成して切替
- `create_project`: 新規プロジェクトを作成し、現在のプロジェクトを置換
- `open_project`: 絶対パスの`file`を読み込み、現在のプロジェクトを置換
- `save_project`: 絶対パスの`file`へSDKのバックアップと同じ形式で保存
- `list_output_plugins`: 出力に指定できるプラグイン名を取得
- `output_file`: `file`と`output_plugin`を指定し、現在のシーンの出力を開始。任意の`save_project_file`で出力前にプロジェクトを保存
- `get_output_status`: `job_id`で状態を取得。省略すると最新ジョブ。ブリッジ内で直近20件を保持

シーン・プロジェクト作成では`width` / `height` / `rate` / `scale` / `sample_rate` / `background`を省略すると、現在のシーン設定を引き継ぎます。`background`は`RRGGBBAA`の8桁で、SDKでは末尾が`ff`以外の場合は透明色として扱います。シーン一覧・切替・作成は一覧と新しいコンテキストをまとめて返すため、続けて編集できます。切替・作成・読み込み後は古いobject IDが失効します。同じシーンを選び直すだけなら失効しません。

`create_project` / `open_project`の`show_confirm`は既定で`false`です。`true`にすると本体の保存・キャンセル確認ダイアログを表示します。これらの操作とシーン切替・作成、保存、出力開始には`timeout_ms`を指定でき、既定は30000、最大300000です。

出力は非同期です。`output_file`の応答は出力開始の受付であり、完了ではありません。`job.state`は`preparing` / `starting` / `running` / `ended` / `rejected`で、`ended`は出力状態を抜けたことだけを表します。SDKは成功・失敗・キャンセルの判別結果を公開していないため、開始後の`outcome`は`unknown`です。開始拒否時は`not_started`になります。

出力プラグインの設定は本体側の設定を使用し、任意のエンコーダ設定をMCPから組み立てる機能はありません。通信キャンセル・タイムアウトは、本体へ送信済みの操作や出力を中止しません。出力開始後に応答を失った場合も、再接続して`get_output_status`を引数なしで呼ぶことで最新ジョブを確認できます。本体再起動後はジョブ履歴が失効します。

フラグ更新例（ほかのフラグは保持）:

```json
{
  "session_id": "get_contextの値",
  "generation": 4,
  "scene_id": 0,
  "selected": true,
  "flags": { "enable_camera": false, "clipping_object": true },
  "dry_run": true
}
```

`flags`には`enable_group` / `enable_camera` / `clipping_object` / `clipping_upper_object`を指定できます。`execute_batch`でも`op: "set_object_flags"`、`object_id`（または作成結果の`result_ref`）、`flags`で利用できます。

## 一括操作と安全性

`execute_batch`は最大100操作をまとめ、事前検査、`dry_run`、進捗通知、タイムアウト、変更後objectの再取得に対応します。高水準編集も配置衝突を事前検査します。

一括操作はトランザクションではなく、途中で失敗すると先に成功した変更が残る場合があります。AviUtl2 SDKにはUndo実行APIがなく、シーン設定変更もUndo対象外です。シーン・プロジェクト操作と出力は`execute_batch`には含められません。これらのSDK関数は読み取り／編集ロックの外で実行する必要があります。

トラック値の内部表現はSDKで公開されていません。`set_track_values`には`inspect_object_values`で取得した値を渡してください。loopback通信に認証はないため、同じPC上のプロセスから接続できます。
