# 旧ツールからの移行

0.3.0でツールを用途別に統合しました。86ツールから40ツールになり、多くのツールは`operation`で動作を選びます。応答には`operation`が入ります。

## 名前が変わらないもの

`ping`、`get_context`、`inspect_scene`、`inspect_timeline`、`find_objects`、`set_object_flags`、`set_scene_settings`、`update_object`、`add_text`、`replace_text`、`set_track_values`、`preflight_media`、`execute_batch`、`render_preview`、`move_to_layers`は名前も引数もそのままです。`inspect_object`と`list_effects`は名前を保ち、`object_ids`と`effect`を受け取れるようにしました。

`compare_frames`は今回追加したツールで、`capture_preview_snapshot`、`render_snapshot_comparison`、`render_change_comparison`をまとめています。

## 統合したツール

| 旧 | 新 |
| --- | --- |
| `list_scenes` / `select_scene` / `create_scene` | `scenes`（`operation`: `list` / `select` / `create`） |
| `create_project` / `open_project` / `save_project` | `project`（`operation`: `create` / `open` / `save`） |
| `list_output_plugins` / `output_file` / `get_output_status` | `output`（`operation`: `list_plugins` / `start` / `status`） |
| `get_server_log` / `reconnect_bridge` | `diagnose_connection`（`include_log` / `reconnect`） |
| `inspect_objects` | `inspect_object`（`object_ids`） |
| `inspect_section_values` | `inspect_object_values`（`sample_sections: true`） |
| `summarize_timeline` / `find_gaps` / `find_overlaps` | `analyze_timeline`（`operation`: `summary` / `gaps` / `overlaps`） |
| `find_empty_layers` | `find_layers`（`layer_filter: "empty"`） |
| `list_used_effects` / `list_used_media` / `find_missing_media` | `audit_scene`（`operation`: `effects` / `media` / `missing_media`） |
| `find_out_of_scene_objects` | `audit_scene`（`operation: "outside_scene"`） |
| `list_effect_items` | `list_effects`（`effect`） |
| `add_media_sequence` | `add_media`（`files`、`gap`、`layer_step`、`default_length`） |
| `duplicate_pattern` | `duplicate_objects`（`rows`、`columns`、`frame_step`、`layer_step`） |
| `delete_object` | `delete_objects`（`object_id`） |
| `replace_media`（単体）/ `replace_media_bulk` / `relink_media` | `replace_media`（`file` / `relink_from_dir`） |
| `add_effect` / `delete_effect` / `set_effect_state` | `edit_effect`（`operation`: `add` / `delete` / `set_state`） |
| `apply_animation_template` | `apply_properties`（`template`: `fade` / `slide` / `zoom`） |
| `align_objects` / `distribute_objects` / `stagger_objects` / `sequence_objects` / `pack_objects` / `normalize_gaps` / `snap_objects_to_bpm` | `arrange_objects`（`operation`） |
| `shift_objects` / `insert_time` / `trim_objects` / `fit_objects` / `fit_objects_to_media` | `retime_objects`（`operation`: `shift` / `insert_time` / `trim` / `fit` / `fit_media`） |
| `split_objects` / `edit_sections` | `edit_sections`（`operation`: `split` / `create` / `delete` / `move`） |
| `name_layers` / `set_layer_colors` | `edit_layers`（`operation`: `name` / `color`） |
| `get_markers` / `edit_markers` / `split_at_markers` / `place_objects_at_markers` | `markers`（`operation`: `list` / `edit` / `split_objects` / `place_objects`） |
| `get_bpm_grid` / `set_bpm_grid` / `set_bpm_grid_list` | `bpm_grid`（`operation`: `get` / `set` / `set_list`） |
| `render_contact_sheet` / `render_range_contact_sheet` / `render_collision_sheet` | `render_contact_sheet`（`source`: `frames` / `range` / `collision`） |
| `capture_preview_snapshot` / `render_snapshot_comparison` / `render_change_comparison` | `compare_frames`（`operation`: `capture` / `compare_capture` / `compare`） |

## 引数の変更

統合に伴い、次の名前を変えました。操作ごとに意味が重ならないよう分けています。

| 旧 | 新 |
| --- | --- |
| `align_objects.target_frame` | `arrange_objects.target_frame`（同じ） |
| `insert_time.frame` + `length` | `retime_objects.frame` + `length` |
| `trim_objects.start` + `end` | `retime_objects.start` + `end` |
| `fit_objects.start` + `end` | `retime_objects.fit_start` + `fit_end` |
| `find_layers.include_empty` | `find_layers.layer_filter`（`used` / `empty` / `all`） |
| `set_layer_colors.color` / `colors` | `edit_layers.color` / `colors`（`operation: "color"`） |
| `name_layers.prefix` | `edit_layers.prefix`（`operation: "name"`） |
| `output_file.file` など | `output`の`file` / `output_plugin` / `save_project_file` |

`session_id`、`generation`、`scene_id`は、変更を伴う操作でのみ必要です。読み取り専用の操作では省略できます。

## 応答の形

統合したツールは応答の先頭に`operation`を返します。`scenes`は`context`と`scenes`、`project`は`context`と`action`、`output`は`plugins`または`status`を返します。`delete_object`の応答は`objects`ではなく`operations`と`mutation`です。
