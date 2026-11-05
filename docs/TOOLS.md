# MCPツール

変更操作には、直前の`get_context`が返す`session_id`、`generation`、`scene_id`を渡します。object IDは同じコンテキスト内でのみ有効です。

作業を始めるときは`inspect_scene`を使ってください。コンテキスト、レイヤーごとのobject数とフレーム範囲、レイヤー色、マーカー、BPMを1回で取得できます。個々のobjectが必要になったら`inspect_timeline`や`inspect_object`を使います。

複数の操作をまとめるときは`execute_batch`を使ってください。1回の呼び出しで1つのUndo単位になります。

## シーン把握

- `ping`: ブリッジが応答するか確認する
- `get_context`: シーン、カーソル、選択範囲、表示範囲、解像度、同時実行トークンを取得する
- `inspect_scene`: シーン全体の概要を取得する。`include_empty`で空レイヤーを含め、`layer_start` / `layer_end` / `max_objects`で範囲を制限する
- `inspect_timeline`: 範囲内のobjectとレイヤー状態を取得する
- `inspect_object`: 1件または最大100件のobjectを取得する。`object_id`か`object_ids`を指定する
- `inspect_object_values`: objectの設定項目、raw値、トラック情報、指定フレームの値を取得する。`sample_sections`で各区間の開始フレームを一括取得する
- `get_selection`: フォーカス中のobjectと選択中のobjectを取得する

## 検索・分析

- `find_objects`: 選択、フォーカス、名前、エフェクト、素材パス、レイヤー、フレーム範囲でobjectを検索する
- `analyze_timeline`: `operation`で切り替える。`summary`はレイヤーごとのobject数と範囲、`gaps`は空き区間、`overlaps`は重なりを返す
- `find_layers`: `layer_filter`（`used` / `empty` / `all`）と名前でレイヤーを検索する
- `audit_scene`: `operation`で切り替える。`effects`は使用エフェクト数、`media`は使用素材、`missing_media`は見つからない素材、`outside_scene`はシーン範囲外のobjectを返す
- `preflight_media`: 素材を追加せず、対応可否とメタデータを返す

## オブジェクト編集

- `add_text`: テキストを配置する
- `add_media`: 素材を配置する。`files`を渡すと順に並べ、各ファイルを事前検査する
- `update_object`: 位置、名前、設定項目を1つのUndo単位で変更する
- `duplicate_objects`: エフェクトとアニメーションを保ったまま複製する。`repeat`はオフセット繰り返し、`rows`と`columns`はグリッド複製
- `delete_objects`: 指定・選択・フォーカスのobjectをまとめて削除する
- `set_object_flags`: グループ・カメラ制御・クリッピングのフラグを一括適用する。指定したフラグだけを変更する
- `replace_media`: `file`で素材を差し替える。`relink_from_dir`でファイル名を頼りに再リンクする
- `replace_text`: テキストを検索置換する

## エフェクトと設定値

- `list_effects`: エフェクト一覧を返す。`effect`を指定するとその設定項目を返す
- `edit_effect`: `operation`で切り替える。`add`は追加、`delete`は削除、`set_state`は有効・無効や並び順の変更
- `apply_properties`: object間で設定値をコピーする。`template`でフェード・スライド・ズームのトラックを複製する
- `set_track_values`: `inspect_object_values`で取得したraw値を適用する

## 配置と長さ

- `arrange_objects`: `operation`で切り替える。`align`、`distribute`、`stagger`、`sequence`、`pack`、`normalize_gaps`、`snap_to_bpm`。衝突は編集前に検出する
- `retime_objects`: `operation`で切り替える。`shift`、`insert_time`、`trim`、`fit`、`fit_media`
- `move_to_layers`: 順序、名前、最初のエフェクトでobjectをレイヤーへ振り分ける
- `edit_layers`: `operation`で切り替える。`name`は連番の名前、`color`は背景色（AviUtl2 2.1.13以降）。空の色は既定色へ戻す
- `edit_sections`: `operation`で切り替える。`split`は指定フレームで分割、`create`・`delete`・`move`は各区間を編集する

## マーカーとBPM

- `markers`: `operation`で切り替える。`list`は一覧、`edit`は一括編集、`split_objects`はマーカー位置で分割、`place_objects`はマーカーへ配置する
- `bpm_grid`: `operation`で切り替える。`get`は取得、`set`は先頭テンポの設定、`set_list`は可変BPM全体の置き換え
- `set_scene_settings`: シーン名、解像度、フレームレート、サンプルレートを変更する。シーン設定はUndoできない

## シーン・プロジェクト・出力

- `scenes`: `operation`で切り替える。`list`は一覧、`select`は切替、`create`は作成して切替。切替と作成の後はobject IDが失効する
- `project`: `operation`で切り替える。`create`、`open`、`save`。`show_confirm`で本体の確認ダイアログを出す
- `output`: `operation`で切り替える。`list_plugins`は出力プラグイン一覧、`start`は出力開始、`status`はジョブ状態。`start`の応答は受付であり、出力完了ではない

## プレビュー

- `render_preview`: 1フレームをPNGで返す
- `render_contact_sheet`: 最大16フレームを1枚のPNGにまとめる。`source`は`frames`、`range`、`collision`
- `compare_frames`: `operation`で切り替える。`compare`は2フレームの比較、`capture`は編集前スナップショットの取得、`compare_capture`はスナップショットとの比較

## 一括と診断

- `execute_batch`: 最大100操作を1つの編集区間で実行する。事前検査、`dry_run`、進捗通知、タイムアウト、実行後のobject取得に対応する
- `diagnose_connection`: サーバー、ブリッジ、本体のバージョンと`capabilities`、遅延、制約を返す。`reconnect`でソケットを開き直し、`include_log`で直近の通信を返す

## 本体バージョンによる違い

| 機能 | 必要バージョン | 未対応時 |
| --- | --- | --- |
| シーン・プロジェクト・出力 | 2.1.10 | プラグインが読み込まれない |
| レイヤー背景色 | 2.1.13 | 色を省略し、設定は`UNSUPPORTED_VERSION` |
| `context.high_dpi_ratio` | 2.1.14 | 省略 |

対応状況は`diagnose_connection`の`bridge.capabilities`で確認できます。AviUtl2を更新すれば有効になり、プラグインの再インストールは不要です。

## 注意点

- 一括操作はトランザクションではありません。途中で失敗すると、先に成功した変更は残ります。
- シーン設定の変更はUndoの対象外です。
- 通信のキャンセルやタイムアウトは、送信済みの操作や出力を止めません。
- ブリッジは`127.0.0.1`で待ち受け、認証はありません。同じPC上のプロセスから接続できます。
- プロジェクトを開くまでは、編集データを扱うツールが`NO_PROJECT`を返します。`ping`と`diagnose_connection`はその状態でも応答します。
