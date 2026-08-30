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

安定版前の内部仕様です。正確な定義は`internal/protocol`を参照してください。
