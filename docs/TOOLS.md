# MCPツール

変更操作には、直前の`get_context`で得た`session_id`、`generation`、`scene_id`が必要です。object IDも同じセッション・generation内でのみ有効です。

## 取得

- `ping`: 接続確認
- `get_context`: シーン、カーソル、選択範囲などを取得
- `inspect_timeline`: 指定範囲のレイヤーとobjectを取得
- `inspect_object`: objectの詳細を取得
- `get_selection`: 選択中のobjectを取得
- `list_effects`: エフェクト一覧を取得
- `list_effect_items`: エフェクトの設定項目を取得
- `preflight_media`: メディア対応可否と情報を確認
- `render_preview`: シーンまたはobjectをPNGで取得

## 編集

- `add_text`: テキストを追加
- `add_media`: メディアを追加
- `update_object`: 位置・名前・設定値を変更
- `delete_object`: objectを削除
- `add_effect` / `delete_effect` / `set_effect_state`: エフェクトを編集
- `execute_batch`: 最大100操作を1つのUndo単位で実行

`execute_batch`はトランザクションではありません。途中で失敗した場合、それ以前の変更は残ることがあります。

## 注意

ブリッジはloopback専用ですが認証機能はありません。同じPC上のプロセスは接続でき、`add_media`はAviUtl2から読めるローカルファイルへアクセスできます。
