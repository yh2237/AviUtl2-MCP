# MCP tool reference

All mutating tools require `session_id`, `generation`, and `scene_id` from a
recent `get_context` result. The bridge rejects the operation if the project or
scene changed. Refresh context and inspect the affected objects before retrying.

Object IDs are opaque and valid only for the current bridge session and
generation.

## Connection and inspection

- `ping`: reports whether the AviUtl2 bridge is reachable.
- `get_context`: returns project/session identity, generation, scene, cursor,
  selection range, display range, and video dimensions.
- `inspect_timeline`: returns objects and layer state in a bounded frame/layer
  range.
- `inspect_object`: returns placement, sections, aliases, and effect state for
  one object.
- `get_selection`: returns the focused object and selected objects.
- `list_effects`: enumerates effects registered with AviUtl2.
- `list_effect_items`: enumerates configurable items for an effect.
- `preflight_media`: asks AviUtl2 whether a file is supported and returns media
  metadata without changing the project.
- `render_preview`: renders a scene or object at one frame. The MCP result is a
  PNG image and is limited to 800 pixels on its longest edge.

## Editing

- `add_text`: creates and configures a text object.
- `add_media`: creates an object from a local media file.
- `update_object`: changes placement, name, or property values in one Undo
  unit.
- `delete_object`: removes an object.
- `add_effect`, `delete_effect`, `set_effect_state`: manage object effects.
- `execute_batch`: runs up to 100 supported edits in one AviUtl2 edit section
  and therefore one Undo unit. A later operation may refer to an object created
  earlier in the same batch through `result_ref`.

`execute_batch` is not a transaction: if operation N fails, operations before N
may remain applied. Use AviUtl2 Undo or explicitly repair the project after a
partial failure.

## Recommended agent flow

1. Call `ping`, then `get_context`.
2. Inspect the target timeline range or object.
3. Preflight media and discover effect/item names when relevant.
4. Submit the smallest useful edit or batch using the exact context tokens.
5. Refresh context and inspect or render the result.

The bridge binds only to loopback and has no client authentication or filesystem
sandbox of its own. Any local process can connect to the port, and `add_media`
can open any path accessible to the AviUtl2 process.
