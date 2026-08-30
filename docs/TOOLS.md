# MCPツール

変更操作には、直前の`get_context`で得た`session_id`、`generation`、`scene_id`が必要です。object IDも同じセッション・generation内でのみ有効です。

## 取得

- `ping`: 接続確認
- `get_context`: シーン、カーソル、選択範囲などを取得
- `inspect_timeline`: 指定範囲のレイヤーとobjectを取得
- `inspect_object`: objectの詳細を取得
- `inspect_objects`: 最大100個のobjectを一括取得
- `inspect_object_values`: 設定値、トラック情報、指定フレームの補間値を取得
- `get_selection`: 選択中のobjectを取得
- `list_effects`: エフェクト一覧を取得
- `list_effect_items`: エフェクトの設定項目を取得
- `preflight_media`: メディア対応可否と情報を確認
- `render_preview`: シーンまたはobjectをPNGで取得
- `render_contact_sheet`: 最大16フレームを1枚のPNGで取得

## 編集

- `add_text`: テキストを追加
- `add_media`: メディアを追加
- `update_object`: 位置・名前・設定値を変更
- `delete_object`: objectを削除
- `duplicate_objects`: エフェクトやアニメーションを維持して一括複製
- `add_effect` / `delete_effect` / `set_effect_state`: エフェクトを編集
- `execute_batch`: 最大100操作を1つのUndo単位で実行

`execute_batch`では中間点、レイヤー状態、シーン設定、マーカー、カーソル、表示位置、選択範囲も変更できます。

## 高水準編集

- `shift_objects`: 複数objectを相対移動
- `align_objects`: 開始・中央・終了位置を整列
- `distribute_objects`: 指定範囲へ等間隔配置
- `insert_time`: 指定位置以降へ空き時間を挿入
- `stagger_objects`: 一定間隔で段階配置
- `replace_media`: 配置やエフェクトを維持して素材を交換

高水準編集は実行前に配置衝突を検査します。`dry_run: true`では編集せず、予定操作と衝突内容だけを返します。

`execute_batch`はトランザクションではありません。途中で失敗した場合、それ以前の変更は残ることがあります。

## 注意

ブリッジはloopback専用ですが認証機能はありません。同じPC上のプロセスは接続でき、`add_media`はAviUtl2から読めるローカルファイルへアクセスできます。
