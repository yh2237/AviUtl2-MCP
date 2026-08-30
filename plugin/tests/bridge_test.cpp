#include <cassert>
#include <iostream>
#include <memory>

#include "../src/bridge.cpp"

namespace {

struct MockEffect {
    std::wstring name = L"テキスト";
    bool enabled = true;
    bool locked = false;
};

struct MockObject {
    int layer = 0;
    int start = 0;
    int end = 29;
    bool deleted = false;
    std::wstring name;
    std::vector<MockEffect> effects{{}};
    std::vector<int> sections{0};
};

EDIT_INFO mock_info{1920, 1080, 30, 1, 48000, 0, 0, 29, 0, 0, 0, 120, 10,
                    -1, -1, 120.0F, 4, 0.0F, 2};
EDIT_SECTION mock_section{};
EDIT_HANDLE mock_handle{};
std::vector<std::unique_ptr<MockObject>> mock_objects;

MockObject* as_object(OBJECT_HANDLE handle) {
    return static_cast<MockObject*>(handle);
}

MockEffect* as_effect(EFFECT_HANDLE handle) {
    return static_cast<MockEffect*>(handle);
}

OBJECT_HANDLE mock_find_object(int layer, int frame) {
    for (const auto& object : mock_objects) {
        if (!object->deleted && object->layer == layer && object->end >= frame) {
            return object.get();
        }
    }
    return nullptr;
}

OBJECT_LAYER_FRAME mock_get_placement(OBJECT_HANDLE handle) {
    const auto* object = as_object(handle);
    return {object->layer, object->start, object->end};
}

LPCSTR mock_get_alias(OBJECT_HANDLE) {
    return "[Object]\n[Object.0]\neffect.name=テキスト\n";
}

LPCSTR mock_get_item(OBJECT_HANDLE, LPCWSTR, LPCWSTR) {
    return "mock";
}

bool mock_set_item(OBJECT_HANDLE, LPCWSTR, LPCWSTR, LPCSTR) {
    return true;
}

bool mock_move_object(OBJECT_HANDLE handle, int layer, int frame) {
    auto* object = as_object(handle);
    const int length = object->end - object->start;
    const int delta = frame - object->start;
    object->layer = layer;
    object->start = frame;
    object->end = frame + length;
    for (int& section : object->sections) section += delta;
    return true;
}

void mock_delete_object(OBJECT_HANDLE handle) {
    as_object(handle)->deleted = true;
}

OBJECT_HANDLE mock_create_object(LPCWSTR, int layer, int frame, int length) {
    auto object = std::make_unique<MockObject>();
    object->layer = layer;
    object->start = frame;
    object->end = frame + length - 1;
    object->sections = {frame};
    MockObject* result = object.get();
    mock_objects.push_back(std::move(object));
    mock_info.layer_max = std::max(mock_info.layer_max, layer);
    mock_info.frame_max = std::max(mock_info.frame_max, result->end);
    return result;
}

OBJECT_HANDLE mock_create_alias(LPCSTR, int layer, int frame, int length) {
    return mock_create_object(L"Alias", layer, frame, length);
}

OBJECT_HANDLE mock_create_media(LPCWSTR, int layer, int frame, int length) {
    return mock_create_object(L"メディア", layer, frame, length == 0 ? 30 : length);
}

LPCWSTR mock_get_object_name(OBJECT_HANDLE handle) {
    const auto& name = as_object(handle)->name;
    return name.empty() ? nullptr : name.c_str();
}

void mock_set_object_name(OBJECT_HANDLE handle, LPCWSTR name) {
    as_object(handle)->name = name == nullptr ? L"" : name;
}

LPCWSTR mock_get_layer_name(int) { return L"Layer"; }
bool mock_layer_enable(int) { return true; }
bool mock_layer_lock(int) { return false; }
LPCWSTR mock_scene_name() { return L"Scene"; }
int mock_section_count(OBJECT_HANDLE handle) {
    return static_cast<int>(as_object(handle)->sections.size());
}
int mock_section_frame(OBJECT_HANDLE handle, int index) {
    const auto& sections = as_object(handle)->sections;
    return index >= 0 && index < static_cast<int>(sections.size())
               ? sections[static_cast<std::size_t>(index)] : -1;
}

bool mock_create_section(OBJECT_HANDLE handle, int frame) {
    auto& sections = as_object(handle)->sections;
    if (std::find(sections.begin(), sections.end(), frame) != sections.end()) return false;
    sections.push_back(frame);
    std::sort(sections.begin(), sections.end());
    return true;
}

bool mock_delete_section(OBJECT_HANDLE handle, int section) {
    auto& sections = as_object(handle)->sections;
    if (section < 1 || section >= static_cast<int>(sections.size())) return false;
    sections.erase(sections.begin() + section);
    return true;
}

bool mock_move_section(OBJECT_HANDLE handle, int section, int frame) {
    auto* object = as_object(handle);
    if (section < 0 || section > static_cast<int>(object->sections.size())) return false;
    if (section == static_cast<int>(object->sections.size())) {
        object->end = frame;
    } else {
        object->sections[static_cast<std::size_t>(section)] = frame;
        if (section == 0) object->start = frame;
    }
    return true;
}

int mock_effect_list(OBJECT_HANDLE handle, EFFECT_HANDLE* output, int count) {
    auto* object = as_object(handle);
    if (output == nullptr) {
        return static_cast<int>(object->effects.size());
    }
    const int copied = std::min(count, static_cast<int>(object->effects.size()));
    for (int index = 0; index < copied; ++index) {
        output[index] = &object->effects[static_cast<std::size_t>(index)];
    }
    return copied;
}

LPCWSTR mock_effect_name(EFFECT_HANDLE handle) { return as_effect(handle)->name.c_str(); }
bool mock_effect_enable(EFFECT_HANDLE handle) { return as_effect(handle)->enabled; }
void mock_set_effect_enable(EFFECT_HANDLE handle, bool enabled) { as_effect(handle)->enabled = enabled; }
bool mock_effect_lock(EFFECT_HANDLE handle) { return as_effect(handle)->locked; }
void mock_set_effect_lock(EFFECT_HANDLE handle, bool locked) { as_effect(handle)->locked = locked; }

EFFECT_HANDLE mock_create_effect(OBJECT_HANDLE handle, LPCWSTR name) {
    auto* object = as_object(handle);
    object->effects.push_back(MockEffect{name});
    return &object->effects.back();
}

bool mock_delete_effect(OBJECT_HANDLE handle, EFFECT_HANDLE effect) {
    auto* object = as_object(handle);
    const auto found = std::find_if(object->effects.begin(), object->effects.end(),
                                    [effect](MockEffect& value) { return &value == effect; });
    if (found == object->effects.end()) return false;
    object->effects.erase(found);
    return true;
}

int mock_move_effect(OBJECT_HANDLE, EFFECT_HANDLE, int index) { return index; }
EFFECT_HANDLE mock_find_effect(OBJECT_HANDLE handle, LPCWSTR name) {
    auto* object = as_object(handle);
    for (auto& effect : object->effects) {
        if (effect.name == name) return &effect;
    }
    return nullptr;
}
LPCSTR mock_get_effect_item(EFFECT_HANDLE, LPCWSTR item) {
    return std::wcscmp(item, L"ファイル") == 0 ? "old.png" : "34";
}
bool mock_set_effect_item(EFFECT_HANDLE, LPCWSTR, LPCSTR) { return true; }
bool mock_get_track_value(EFFECT_HANDLE, LPCWSTR, double, double* value) {
    *value = 34.0;
    return true;
}
bool mock_get_check_value(EFFECT_HANDLE, LPCWSTR, int, bool* value) {
    *value = true;
    return true;
}
bool mock_get_track_info(EFFECT_HANDLE, LPCWSTR, TRACK_INFO* info, int) {
    static double parameters[]{34.0, 40.0};
    *info = {L"直線移動", parameters, 2, false, false, false, false, 1, 0, nullptr};
    return true;
}
OBJECT_HANDLE mock_focus_object() { return mock_objects.empty() ? nullptr : mock_objects.front().get(); }
int mock_selected_count() { return mock_objects.empty() ? 0 : 1; }
OBJECT_HANDLE mock_selected_object(int index) { return index == 0 ? mock_focus_object() : nullptr; }
int mock_focus_section() { return 0; }
bool mock_support_media(LPCWSTR, bool) { return true; }
bool mock_media_info(LPCWSTR, MEDIA_INFO* info, int) {
    *info = {1, 1, 2.5, 1280, 720};
    return true;
}

void mock_set_layer_name(int, LPCWSTR) {}
void mock_set_layer_enable(int, bool) {}
void mock_set_layer_lock(int, bool) {}
void mock_set_scene_name(LPCWSTR) {}
void mock_set_scene_size(int width, int height) { mock_info.width = width; mock_info.height = height; }
void mock_set_scene_rate(int rate, int scale) { mock_info.rate = rate; mock_info.scale = scale; }
void mock_set_sample_rate(int value) { mock_info.sample_rate = value; }
void mock_set_cursor(int layer, int frame) { mock_info.layer = layer; mock_info.frame = frame; }
void mock_set_display(int layer, int frame) { mock_info.display_layer_start = layer; mock_info.display_frame_start = frame; }
void mock_set_range(int start, int end) { mock_info.select_range_start = start; mock_info.select_range_end = end; }
void mock_set_marker(int, LPCWSTR) {}
void mock_clear_marker(int) {}
bool mock_move_marker(int, int) { return true; }

void mock_get_edit_info(EDIT_INFO* info, int) { *info = mock_info; }
int mock_edit_state() { return EDIT_HANDLE::EDIT_STATE_EDIT; }
bool mock_read(void* param, void (*callback)(void*, EDIT_SECTION*)) {
    callback(param, &mock_section);
    return true;
}
bool mock_edit(void* param, void (*callback)(void*, EDIT_SECTION*)) {
    mock_section.info = &mock_info;
    callback(param, &mock_section);
    mock_section.info = nullptr;
    return true;
}

void mock_enum_effect(void* param, void (*callback)(void*, LPCWSTR, int, int)) {
    callback(param, L"テキスト", EDIT_HANDLE::EFFECT_TYPE_FILTER,
             EDIT_HANDLE::EFFECT_FLAG_VIDEO);
}

bool mock_enum_items(LPCWSTR, void* param, void (*callback)(void*, LPCWSTR, int)) {
    callback(param, L"サイズ", EDIT_HANDLE::EFFECT_ITEM_TYPE_NUMBER);
    callback(param, L"ファイル", EDIT_HANDLE::EFFECT_ITEM_TYPE_FILE);
    return true;
}

bool mock_render(int frame, void* param,
                 void (*callback)(void*, int, const void*, int, int, int)) {
    const std::uint8_t pixels[]{255, 0, 0, 255, 0, 255, 0, 255};
    callback(param, frame, pixels, 2, 1, 8);
    return true;
}

bool mock_render_object(OBJECT_HANDLE, int frame, bool, void* param,
                        void (*callback)(void*, int, const void*, int, int, int)) {
    return mock_render(frame, param, callback);
}

void mock_wait_render() {}

void configure_mock() {
    mock_objects.clear();
    invalidate_objects();
    generation.store(10);
    session_id = "test-session";
    mock_create_object(L"テキスト", 0, 0, 30);

    mock_section.find_object = mock_find_object;
    mock_section.get_object_layer_frame = mock_get_placement;
    mock_section.get_object_alias = mock_get_alias;
    mock_section.get_object_item_value = mock_get_item;
    mock_section.set_object_item_value = mock_set_item;
    mock_section.move_object = mock_move_object;
    mock_section.delete_object = mock_delete_object;
    mock_section.get_focus_object = mock_focus_object;
    mock_section.get_selected_object = mock_selected_object;
    mock_section.get_selected_object_num = mock_selected_count;
    mock_section.create_object_from_media_file = mock_create_media;
    mock_section.create_object_from_alias = mock_create_alias;
    mock_section.create_object = mock_create_object;
    mock_section.get_object_name = mock_get_object_name;
    mock_section.set_object_name = mock_set_object_name;
    mock_section.get_layer_name = mock_get_layer_name;
    mock_section.get_scene_name = mock_scene_name;
    mock_section.get_layer_enable = mock_layer_enable;
    mock_section.get_layer_lock = mock_layer_lock;
    mock_section.get_object_section_num = mock_section_count;
    mock_section.get_focus_object_section = mock_focus_section;
    mock_section.get_object_section_frame = mock_section_frame;
    mock_section.create_object_section = mock_create_section;
    mock_section.delete_object_section = mock_delete_section;
    mock_section.move_object_section = mock_move_section;
    mock_section.get_effect_list = mock_effect_list;
    mock_section.get_effect_name = mock_effect_name;
    mock_section.get_effect_enable = mock_effect_enable;
    mock_section.set_effect_enable = mock_set_effect_enable;
    mock_section.get_effect_lock = mock_effect_lock;
    mock_section.set_effect_lock = mock_set_effect_lock;
    mock_section.create_effect = mock_create_effect;
    mock_section.delete_effect = mock_delete_effect;
    mock_section.move_effect = mock_move_effect;
    mock_section.find_effect = mock_find_effect;
    mock_section.get_effect_item_value = mock_get_effect_item;
    mock_section.set_effect_item_value = mock_set_effect_item;
    mock_section.get_effect_track_value = mock_get_track_value;
    mock_section.get_effect_check_value = mock_get_check_value;
    mock_section.get_effect_track_info = mock_get_track_info;
    mock_section.is_support_media_file = mock_support_media;
    mock_section.get_media_info = mock_media_info;
    mock_section.set_layer_name = mock_set_layer_name;
    mock_section.set_layer_enable = mock_set_layer_enable;
    mock_section.set_layer_lock = mock_set_layer_lock;
    mock_section.set_scene_name = mock_set_scene_name;
    mock_section.set_scene_size = mock_set_scene_size;
    mock_section.set_scene_frame_rate = mock_set_scene_rate;
    mock_section.set_scene_sample_rate = mock_set_sample_rate;
    mock_section.set_cursor_layer_frame = mock_set_cursor;
    mock_section.set_display_layer_frame = mock_set_display;
    mock_section.set_select_range = mock_set_range;
    mock_section.set_mark_frame = mock_set_marker;
    mock_section.clear_mark_frame = mock_clear_marker;
    mock_section.move_mark_frame = mock_move_marker;

    mock_handle.get_edit_info = mock_get_edit_info;
    mock_handle.get_edit_state = mock_edit_state;
    mock_handle.call_read_section_param = mock_read;
    mock_handle.call_edit_section_param = mock_edit;
    mock_handle.enum_effect_name = mock_enum_effect;
    mock_handle.enum_effect_item = mock_enum_items;
    mock_handle.rendering_scene_video = mock_render;
    mock_handle.rendering_object_video = mock_render_object;
    mock_handle.wait_rendering_task = mock_wait_render;
    edit_handle = &mock_handle;
}

json request(std::string method, json params = json::object(), bool expected = false) {
    json value{{"version", kProtocolVersion}, {"method", std::move(method)}, {"params", std::move(params)}};
    if (expected) {
        value["context"] = {{"session_id", session_id}, {"generation", generation.load()},
                            {"scene_id", mock_info.scene_id}};
    }
    return value;
}

}  // namespace

int main() {
    configure_mock();

    const json ping = dispatch(request("ping"));
    assert(ping.at("pong").get<bool>());

    const json timeline = dispatch(request("inspect_timeline", {
        {"layer_start", 0}, {"layer_end", 0}, {"frame_start", 0}, {"frame_end", 60},
        {"max_objects", 10}, {"include_effects", true},
    }));
    assert(timeline.at("objects").size() == 1);
    const std::uint64_t object_id = timeline.at("objects").at(0).at("id").get<std::uint64_t>();

    const json objects = dispatch(request("inspect_objects", {
        {"object_ids", json::array({object_id})}, {"include_effects", true},
    }));
    assert(objects.at("objects").size() == 1);
    assert(objects.at("objects").at(0).at("effects").size() == 1);

    const json values = dispatch(request("inspect_object_values", {
        {"object_id", object_id}, {"frame", 5.0},
    }));
    assert(values.at("effects").at(0).at("items").size() == 2);
    assert(values.at("effects").at(0).at("items").at(0).at("sampled_value").get<double>() == 34.0);

    const json added = dispatch(request("add_text", {
        {"text", "hello"}, {"layer", 1}, {"frame", 10}, {"length", 20},
        {"size", 34.0}, {"color", "ffffff"},
    }, true));
    assert(added.at("results").at(0).at("changed").get<bool>());

    const json batch = dispatch(request("execute_batch", {{"operations", json::array({
        {{"op", "add_text"}, {"text", "batch"}, {"layer", 2}, {"frame", 0},
         {"length", 10}, {"size", 30.0}, {"color", "ffffff"}},
        {{"op", "update_object"}, {"result_ref", 0}, {"layer", 3}, {"frame", 5}},
    })}}, true));
    assert(batch.at("results").size() == 2);

    const json advanced = dispatch(request("execute_batch", {{"operations", json::array({
        {{"op", "duplicate_object"}, {"object_id", object_id}, {"layer", 4},
         {"frame", 40}, {"length", 30}},
        {{"op", "create_section"}, {"object_id", object_id}, {"frame", 10}},
        {{"op", "set_layer_state"}, {"layer", 0}, {"name", "Main"}, {"locked", true}},
        {{"op", "set_marker"}, {"frame", 15}, {"memo", "beat"}},
        {{"op", "set_selection_range"}, {"start", 5}, {"end", 25}},
        {{"op", "replace_media"}, {"object_id", object_id}, {"file", "new.png"},
         {"effect", "テキスト"}, {"item", "ファイル"}},
    })}}, true));
    assert(advanced.at("results").size() == 6);
    assert(mock_objects.size() >= 4);
    assert(as_object(resolve_object(object_id))->sections.size() == 2);

    bool invalid_batch_rejected = false;
    try {
        (void)dispatch(request("execute_batch", {{"operations", json::array({
            {{"op", "add_media"}, {"file", ""}, {"layer", -1}, {"frame", 0}},
        })}}, true));
    } catch (const BridgeError& error) {
        invalid_batch_rejected = error.code() == "INVALID_ARGUMENT";
    }
    assert(invalid_batch_rejected);

    const json preview = dispatch(request("render_preview", {
        {"frame", 0}, {"max_width", 320}, {"max_height", 320},
    }));
    assert(preview.at("width").get<int>() == 2);
    assert(!preview.at("rgba_base64").get<std::string>().empty());

    bool stale_rejected = false;
    try {
        json stale = request("delete_object", {{"object_id", object_id}}, true);
        stale["context"]["generation"] = 9;
        (void)dispatch(stale);
    } catch (const BridgeError& error) {
        stale_rejected = error.code() == "STALE_CONTEXT";
    }
    assert(stale_rejected);

    std::cout << "bridge dispatch tests passed\n";
    return 0;
}
