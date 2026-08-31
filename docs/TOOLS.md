# MCPツール

変更操作には、直前の`get_context`が返す`session_id`、`generation`、`scene_id`が必要です。object IDも同じコンテキスト内でのみ有効です。

## 取得・検索

- `ping` / `get_context` / `get_selection`: 接続状態、現在のシーン、選択を取得
- `inspect_timeline` / `inspect_object` / `inspect_objects`: タイムラインとobjectを取得
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

## 一括操作と安全性

`execute_batch`は最大100操作をまとめ、事前検査、`dry_run`、進捗通知、タイムアウト、変更後objectの再取得に対応します。高水準編集も配置衝突を事前検査します。

一括操作はトランザクションではなく、途中で失敗すると先に成功した変更が残る場合があります。AviUtl2 SDKにはUndo実行APIがなく、シーン設定変更もUndo対象外です。また、公開SDKにはシーン一覧・切替APIがないため、操作対象は現在のシーンだけです。

トラック値の内部表現はSDKで公開されていません。`set_track_values`には`inspect_object_values`で取得した値を渡してください。loopback通信に認証はないため、同じPC上のプロセスから接続できます。
