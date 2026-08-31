#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#ifndef NOMINMAX
#define NOMINMAX
#endif

#include <winsock2.h>
#include <ws2tcpip.h>
#include <windows.h>

#include <algorithm>
#include <atomic>
#include <cmath>
#include <cstdint>
#include <cwchar>
#include <mutex>
#include <sstream>
#include <stdexcept>
#include <string>
#include <thread>
#include <unordered_map>
#include <utility>
#include <vector>

#if __has_include(<nlohmann/json.hpp>)
#include <nlohmann/json.hpp>
#elif __has_include(<nlohmann_json/json.hpp>)
#include <nlohmann_json/json.hpp>
#else
#error "nlohmann/json.hpp was not found"
#endif

#include "logger2.h"
#include "plugin2.h"

namespace {

using json = nlohmann::json;

constexpr std::uint32_t kProtocolVersion = 1;
constexpr std::uint32_t kMaxMessageSize = 4U << 20;
constexpr std::uint16_t kDefaultPort = 28552;
constexpr int kMaxTimelineObjects = 1000;
constexpr int kMaxBatchOperations = 100;
constexpr std::uint32_t kRequiredVersion = 2003300;

COMMON_PLUGIN_TABLE plugin_table{
    L"AviUtl2 MCP Bridge",
    L"Native bridge for the independent Go-based AviUtl2 MCP server",
};

EDIT_HANDLE* edit_handle = nullptr;
LOG_HANDLE* logger = nullptr;
std::atomic<bool> running{false};
std::atomic<std::uint64_t> generation{1};
std::thread server_thread;
std::mutex socket_mutex;
SOCKET listen_socket = INVALID_SOCKET;
SOCKET client_socket = INVALID_SOCKET;
std::string session_id;
std::uint32_t host_version = 0;

std::mutex registry_mutex;
std::unordered_map<std::uint64_t, OBJECT_HANDLE> objects_by_id;
std::unordered_map<OBJECT_HANDLE, std::uint64_t> ids_by_object;
std::uint64_t next_object_id = 1;

class BridgeError final : public std::runtime_error {
public:
    BridgeError(std::string code, std::string message, bool retryable = false)
        : std::runtime_error(std::move(message)), code_(std::move(code)), retryable_(retryable) {}

    const std::string& code() const noexcept { return code_; }
    bool retryable() const noexcept { return retryable_; }

private:
    std::string code_;
    bool retryable_;
};

void log_error(const wchar_t* message) {
    if (logger != nullptr) {
        logger->error(logger, message);
    }
}

std::string make_session_id() {
    std::ostringstream stream;
    stream << std::hex << GetCurrentProcessId() << '-' << GetTickCount64();
    return stream.str();
}

std::string wide_to_utf8(const wchar_t* value) {
    if (value == nullptr || *value == L'\0') {
        return {};
    }
    const int input_length = static_cast<int>(std::wcslen(value));
    const int output_length = WideCharToMultiByte(
        CP_UTF8, WC_ERR_INVALID_CHARS, value, input_length, nullptr, 0, nullptr, nullptr);
    if (output_length <= 0) {
        throw BridgeError("ENCODING_ERROR", "could not encode an AviUtl2 string as UTF-8");
    }
    std::string output(static_cast<std::size_t>(output_length), '\0');
    if (WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, value, input_length,
                            output.data(), output_length, nullptr, nullptr) == 0) {
        throw BridgeError("ENCODING_ERROR", "could not encode an AviUtl2 string as UTF-8");
    }
    return output;
}

std::wstring utf8_to_wide(const std::string& value) {
    if (value.empty()) {
        return {};
    }
    const int output_length = MultiByteToWideChar(
        CP_UTF8, MB_ERR_INVALID_CHARS, value.data(), static_cast<int>(value.size()), nullptr, 0);
    if (output_length <= 0) {
        throw BridgeError("INVALID_ARGUMENT", "string is not valid UTF-8");
    }
    std::wstring output(static_cast<std::size_t>(output_length), L'\0');
    if (MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, value.data(),
                            static_cast<int>(value.size()), output.data(), output_length) == 0) {
        throw BridgeError("INVALID_ARGUMENT", "string is not valid UTF-8");
    }
    return output;
}

bool is_hex_color(const std::string& value) {
    return value.size() == 6 && std::all_of(value.begin(), value.end(), [](unsigned char character) {
        return (character >= '0' && character <= '9') ||
               (character >= 'a' && character <= 'f') ||
               (character >= 'A' && character <= 'F');
    });
}

void invalidate_objects() noexcept {
    std::lock_guard lock(registry_mutex);
    objects_by_id.clear();
    ids_by_object.clear();
    next_object_id = 1;
    generation.fetch_add(1, std::memory_order_acq_rel);
}

std::uint64_t register_object(OBJECT_HANDLE object) {
    if (object == nullptr) {
        throw BridgeError("HOST_ERROR", "AviUtl2 returned a null object handle");
    }
    std::lock_guard lock(registry_mutex);
    if (const auto found = ids_by_object.find(object); found != ids_by_object.end()) {
        return found->second;
    }
    const std::uint64_t id = next_object_id++;
    objects_by_id.emplace(id, object);
    ids_by_object.emplace(object, id);
    return id;
}

OBJECT_HANDLE resolve_object(std::uint64_t id) {
    std::lock_guard lock(registry_mutex);
    const auto found = objects_by_id.find(id);
    if (found == objects_by_id.end()) {
        throw BridgeError("STALE_OBJECT", "object_id is unknown or belongs to an expired generation");
    }
    return found->second;
}

void unregister_object(OBJECT_HANDLE object) noexcept {
    std::lock_guard lock(registry_mutex);
    const auto found = ids_by_object.find(object);
    if (found == ids_by_object.end()) {
        return;
    }
    objects_by_id.erase(found->second);
    ids_by_object.erase(found);
}

void close_socket(SOCKET& target) {
    if (target == INVALID_SOCKET) {
        return;
    }
    shutdown(target, SD_BOTH);
    closesocket(target);
    target = INVALID_SOCKET;
}

void close_server_sockets() {
    std::lock_guard lock(socket_mutex);
    close_socket(client_socket);
    close_socket(listen_socket);
}

bool receive_all(SOCKET socket, void* destination, std::size_t size) {
    auto* output = static_cast<char*>(destination);
    while (size > 0 && running.load(std::memory_order_acquire)) {
        const int chunk_size = static_cast<int>(std::min<std::size_t>(size, 64 * 1024));
        const int received = recv(socket, output, chunk_size, 0);
        if (received <= 0) {
            return false;
        }
        output += received;
        size -= static_cast<std::size_t>(received);
    }
    return size == 0;
}

bool send_all(SOCKET socket, const void* source, std::size_t size) {
    const auto* input = static_cast<const char*>(source);
    while (size > 0 && running.load(std::memory_order_acquire)) {
        const int chunk_size = static_cast<int>(std::min<std::size_t>(size, 64 * 1024));
        const int sent = send(socket, input, chunk_size, 0);
        if (sent <= 0) {
            return false;
        }
        input += sent;
        size -= static_cast<std::size_t>(sent);
    }
    return size == 0;
}

bool read_frame(SOCKET socket, std::string& payload) {
    std::uint8_t header[4]{};
    if (!receive_all(socket, header, sizeof(header))) {
        return false;
    }
    const std::uint32_t size = static_cast<std::uint32_t>(header[0]) |
                               (static_cast<std::uint32_t>(header[1]) << 8U) |
                               (static_cast<std::uint32_t>(header[2]) << 16U) |
                               (static_cast<std::uint32_t>(header[3]) << 24U);
    if (size > kMaxMessageSize) {
        return false;
    }
    payload.resize(size);
    return size == 0 || receive_all(socket, payload.data(), size);
}

bool write_frame(SOCKET socket, const std::string& payload) {
    if (payload.size() > kMaxMessageSize) {
        return false;
    }
    const auto size = static_cast<std::uint32_t>(payload.size());
    const std::uint8_t header[4]{
        static_cast<std::uint8_t>(size),
        static_cast<std::uint8_t>(size >> 8U),
        static_cast<std::uint8_t>(size >> 16U),
        static_cast<std::uint8_t>(size >> 24U),
    };
    return send_all(socket, header, sizeof(header)) &&
           (payload.empty() || send_all(socket, payload.data(), payload.size()));
}

json error_response(std::uint64_t id, std::string code, std::string message,
                    bool retryable = false, json details = nullptr) {
    json error{{"code", std::move(code)}, {"message", std::move(message)}, {"retryable", retryable}};
    if (!details.is_null()) {
        error["details"] = std::move(details);
    }
    return {{"id", id}, {"version", kProtocolVersion}, {"error", std::move(error)}};
}

json context_json(EDIT_SECTION* edit = nullptr) {
    EDIT_INFO info{};
    edit_handle->get_edit_info(&info, sizeof(info));
    json output{
        {"session_id", session_id},
        {"generation", generation.load(std::memory_order_acquire)},
        {"scene_id", info.scene_id},
        {"edit_state", edit_handle->get_edit_state()},
        {"width", info.width}, {"height", info.height},
        {"rate", info.rate}, {"scale", info.scale}, {"sample_rate", info.sample_rate},
        {"frame", info.frame}, {"layer", info.layer},
        {"frame_max", info.frame_max}, {"layer_max", info.layer_max},
        {"display_frame_start", info.display_frame_start},
        {"display_layer_start", info.display_layer_start},
        {"display_frame_num", info.display_frame_num},
        {"display_layer_num", info.display_layer_num},
        {"select_range_start", info.select_range_start},
        {"select_range_end", info.select_range_end},
        {"grid_bpm_tempo", info.grid_bpm_tempo},
        {"grid_bpm_beat", info.grid_bpm_beat},
        {"grid_bpm_offset", info.grid_bpm_offset},
    };
    if (edit != nullptr) {
        output["scene_name"] = wide_to_utf8(edit->get_scene_name());
    }
    return output;
}

json effect_json(EDIT_SECTION* edit, EFFECT_HANDLE effect, int index) {
    return {
        {"index", index},
        {"name", wide_to_utf8(edit->get_effect_name(effect))},
        {"enabled", edit->get_effect_enable(effect)},
        {"locked", edit->get_effect_lock(effect)},
    };
}

json object_json(EDIT_SECTION* edit, OBJECT_HANDLE object, bool include_alias,
                 bool include_effects) {
    const OBJECT_LAYER_FRAME placement = edit->get_object_layer_frame(object);
    json output{
        {"id", register_object(object)},
        {"name", wide_to_utf8(edit->get_object_name(object))},
        {"layer", placement.layer},
        {"start", placement.start},
        {"end", placement.end},
    };
    if (include_alias) {
        const char* alias = edit->get_object_alias(object);
        output["alias"] = alias == nullptr ? "" : std::string(alias);
    }
    const int section_count = std::max(0, edit->get_object_section_num(object));
    json sections = json::array();
    for (int index = 0; index < section_count; ++index) {
        const int frame = edit->get_object_section_frame(object, index);
        if (frame >= 0) {
            sections.push_back(frame);
        }
    }
    output["sections"] = std::move(sections);

    if (include_effects) {
        const int count = std::max(0, edit->get_effect_list(object, nullptr, 0));
        std::vector<EFFECT_HANDLE> effects(static_cast<std::size_t>(count));
        const int received = count == 0 ? 0 : edit->get_effect_list(object, effects.data(), count);
        json values = json::array();
        for (int index = 0; index < received; ++index) {
            values.push_back(effect_json(edit, effects[static_cast<std::size_t>(index)], index));
        }
        output["effects"] = std::move(values);
    }
    return output;
}

struct SectionCall {
    std::string method;
    json params;
    json expected;
    json result;
    std::string error_code;
    std::string error_message;
    bool retryable = false;
};

void store_exception(SectionCall* call) noexcept {
    try {
        throw;
    } catch (const BridgeError& error) {
        call->error_code = error.code();
        call->error_message = error.what();
        call->retryable = error.retryable();
    } catch (const std::exception& error) {
        call->error_code = "HOST_ERROR";
        call->error_message = error.what();
    } catch (...) {
        call->error_code = "HOST_ERROR";
        call->error_message = "unknown native bridge error";
    }
}

void check_expected_context(const json& expected, EDIT_SECTION* edit) {
    if (!expected.is_object()) {
        throw BridgeError("MISSING_CONTEXT", "mutation requires session_id, generation, and scene_id");
    }
    if (expected.value("session_id", "") != session_id) {
        throw BridgeError("STALE_CONTEXT", "AviUtl2 session changed; call get_context again");
    }
    if (!expected.contains("generation") ||
        expected.at("generation").get<std::uint64_t>() != generation.load(std::memory_order_acquire)) {
        throw BridgeError("STALE_CONTEXT", "object generation changed; inspect the timeline again");
    }
    if (!expected.contains("scene_id") || expected.at("scene_id").get<int>() != edit->info->scene_id) {
        throw BridgeError("STALE_CONTEXT", "active scene changed; call get_context again");
    }
}

void inspect_timeline(SectionCall* call, EDIT_SECTION* edit) {
    const int layer_start = call->params.at("layer_start").get<int>();
    const int layer_end = call->params.at("layer_end").get<int>();
    const int frame_start = call->params.at("frame_start").get<int>();
    const int frame_end = call->params.at("frame_end").get<int>();
    const int max_objects = std::clamp(call->params.value("max_objects", 200), 1, kMaxTimelineObjects);
    const bool include_alias = call->params.value("include_alias", false);
    const bool include_effects = call->params.value("include_effects", false);
    if (layer_start < 0 || layer_end < layer_start || layer_end - layer_start > 99 ||
        frame_start < 0 || frame_end < frame_start) {
        throw BridgeError("INVALID_ARGUMENT", "invalid timeline range");
    }

    json layers = json::array();
    json objects = json::array();
    bool truncated = false;
    for (int layer = layer_start; layer <= layer_end; ++layer) {
        layers.push_back({
            {"index", layer},
            {"name", wide_to_utf8(edit->get_layer_name(layer))},
            {"enabled", edit->get_layer_enable(layer)},
            {"locked", edit->get_layer_lock(layer)},
        });
        int cursor = frame_start;
        while (cursor <= frame_end) {
            OBJECT_HANDLE object = edit->find_object(layer, cursor);
            if (object == nullptr) {
                break;
            }
            const OBJECT_LAYER_FRAME placement = edit->get_object_layer_frame(object);
            if (placement.start > frame_end) {
                break;
            }
            if (static_cast<int>(objects.size()) >= max_objects) {
                truncated = true;
                break;
            }
            objects.push_back(object_json(edit, object, include_alias, include_effects));
            const int next = placement.end + 1;
            cursor = next > cursor ? next : cursor + 1;
        }
        if (truncated) {
            break;
        }
    }
    call->result = {
        {"context", context_json(edit)},
        {"layers", std::move(layers)},
        {"objects", std::move(objects)},
        {"truncated", truncated},
    };
}

void inspect_object(SectionCall* call, EDIT_SECTION* edit) {
    OBJECT_HANDLE object = resolve_object(call->params.at("object_id").get<std::uint64_t>());
    call->result = {
        {"context", context_json(edit)},
        {"object", object_json(edit, object, call->params.value("include_alias", false),
                                call->params.value("include_effects", true))},
    };
}

void inspect_objects(SectionCall* call, EDIT_SECTION* edit) {
    const json& object_ids = call->params.at("object_ids");
    if (!object_ids.is_array() || object_ids.empty() || object_ids.size() > 100) {
        throw BridgeError("INVALID_ARGUMENT", "object_ids must contain between 1 and 100 entries");
    }
    const bool include_alias = call->params.value("include_alias", false);
    const bool include_effects = call->params.value("include_effects", false);
    json objects = json::array();
    for (const json& object_id : object_ids) {
        objects.push_back(object_json(edit, resolve_object(object_id.get<std::uint64_t>()),
                                      include_alias, include_effects));
    }
    call->result = {
        {"context", context_json(edit)},
        {"objects", std::move(objects)},
    };
}

void get_selection(SectionCall* call, EDIT_SECTION* edit) {
    json objects = json::array();
    const int count = std::max(0, edit->get_selected_object_num());
    for (int index = 0; index < count; ++index) {
        if (OBJECT_HANDLE object = edit->get_selected_object(index); object != nullptr) {
            objects.push_back(object_json(edit, object, false, true));
        }
    }
    json result{
        {"context", context_json(edit)},
        {"focus_object_section", edit->get_focus_object_section()},
        {"objects", std::move(objects)},
    };
    if (OBJECT_HANDLE focus = edit->get_focus_object(); focus != nullptr) {
        result["focus_object_id"] = register_object(focus);
    }
    call->result = std::move(result);
}

void preflight_media(SectionCall* call, EDIT_SECTION* edit) {
    const std::string file_utf8 = call->params.at("file").get<std::string>();
    const std::wstring file = utf8_to_wide(file_utf8);
    const bool supported = edit->is_support_media_file(file.c_str(), call->params.value("strict", false));
    MEDIA_INFO info{};
    const bool has_info = supported && edit->get_media_info(file.c_str(), &info, sizeof(info));
    call->result = {
        {"file", file_utf8},
        {"supported", supported},
        {"video_track_count", has_info ? info.video_track_num : 0},
        {"audio_track_count", has_info ? info.audio_track_num : 0},
        {"total_time", has_info ? info.total_time : 0.0},
        {"width", has_info ? info.width : 0},
        {"height", has_info ? info.height : 0},
    };
}

struct ObjectItemEnumeration {
    EDIT_SECTION* edit = nullptr;
    EFFECT_HANDLE effect = nullptr;
    double frame = 0.0;
    json items = json::array();
    std::string error;
    std::vector<std::wstring> requested_items;
    bool include_raw_values = true;
    bool include_track_info = true;
    bool include_sampled_values = true;
};

void collect_object_item(void* parameter, LPCWSTR name, int type) noexcept {
    auto* call = static_cast<ObjectItemEnumeration*>(parameter);
    try {
        if (!call->requested_items.empty() &&
            std::find(call->requested_items.begin(), call->requested_items.end(), name) ==
                call->requested_items.end()) {
            return;
        }
        json item{{"name", wide_to_utf8(name)}, {"type", type}};
        if (call->include_raw_values) {
            if (const char* raw = call->edit->get_effect_item_value(call->effect, name); raw != nullptr) {
                item["raw_value"] = std::string(raw);
            }
        }
        if (type == EDIT_HANDLE::EFFECT_ITEM_TYPE_NUMBER) {
            TRACK_INFO info{};
            if (call->include_track_info &&
                call->edit->get_effect_track_info(call->effect, name, &info, sizeof(info))) {
                json track{
                    {"accelerate", info.accelerate}, {"decelerate", info.decelerate},
                    {"two_point", info.twopoint}, {"time_control", info.timecontrol},
                    {"group_count", info.group_num}, {"group_index", info.group_index},
                };
                if (info.mode != nullptr) {
                    track["mode"] = wide_to_utf8(info.mode);
                }
                if (info.group_name != nullptr) {
                    track["group_name"] = wide_to_utf8(info.group_name);
                }
                json parameters = json::array();
                for (int index = 0; info.param != nullptr && index < info.param_num; ++index) {
                    parameters.push_back(info.param[index]);
                }
                track["parameters"] = std::move(parameters);
                item["track"] = std::move(track);
            }
            double sampled = 0.0;
            if (call->include_sampled_values &&
                call->edit->get_effect_track_value(call->effect, name, call->frame, &sampled)) {
                item["sampled_value"] = sampled;
            }
        } else if (type == EDIT_HANDLE::EFFECT_ITEM_TYPE_CHECK && call->include_sampled_values) {
            bool checked = false;
            if (call->edit->get_effect_check_value(
                    call->effect, name, static_cast<int>(std::floor(call->frame)), &checked)) {
                item["checked"] = checked;
            }
        }
        call->items.push_back(std::move(item));
    } catch (const std::exception& error) {
        call->error = error.what();
    } catch (...) {
        call->error = "unknown object item enumeration error";
    }
}

void inspect_object_values(SectionCall* call, EDIT_SECTION* edit) {
    OBJECT_HANDLE object = resolve_object(call->params.at("object_id").get<std::uint64_t>());
    const OBJECT_LAYER_FRAME placement = edit->get_object_layer_frame(object);
    const double frame = call->params.contains("frame") && !call->params.at("frame").is_null()
                             ? call->params.at("frame").get<double>()
                             : static_cast<double>(placement.start);
    if (!std::isfinite(frame) || frame < 0.0) {
        throw BridgeError("INVALID_ARGUMENT", "frame must be a finite non-negative number");
    }

    const int effect_count = std::max(0, edit->get_effect_list(object, nullptr, 0));
    std::vector<EFFECT_HANDLE> effects(static_cast<std::size_t>(effect_count));
    const int received = effect_count == 0 ? 0 : edit->get_effect_list(object, effects.data(), effect_count);
    const int requested_index = call->params.contains("effect_index") && !call->params.at("effect_index").is_null()
                                    ? call->params.at("effect_index").get<int>() : -1;
    const std::wstring requested_effect = utf8_to_wide(call->params.value("effect", ""));
    if (requested_index >= received) {
        throw BridgeError("INVALID_ARGUMENT", "effect_index is out of range");
    }
    std::vector<std::wstring> requested_items;
    if (call->params.contains("items")) {
        for (const json& item : call->params.at("items")) {
            requested_items.push_back(utf8_to_wide(item.get<std::string>()));
        }
    }
    const bool include_raw = call->params.value("include_raw_values", true);
    const bool include_track = call->params.value("include_track_info", true);
    const bool include_sampled = call->params.value("include_sampled_values", true);
    json values = json::array();
    for (int index = 0; index < received; ++index) {
        EFFECT_HANDLE effect = effects[static_cast<std::size_t>(index)];
        const wchar_t* raw_effect_name = edit->get_effect_name(effect);
        const std::wstring effect_name = raw_effect_name == nullptr
                                             ? std::wstring{}
                                             : std::wstring(raw_effect_name);
        if ((requested_index >= 0 && index != requested_index) ||
            (!requested_effect.empty() && effect_name != requested_effect)) {
            continue;
        }
        ObjectItemEnumeration enumeration{edit, effect, frame};
        enumeration.requested_items = requested_items;
        enumeration.include_raw_values = include_raw;
        enumeration.include_track_info = include_track;
        enumeration.include_sampled_values = include_sampled;
        if (!effect_name.empty()) {
            edit_handle->enum_effect_item(effect_name.c_str(), &enumeration, collect_object_item);
        }
        if (!enumeration.error.empty()) {
            throw BridgeError("HOST_ERROR", enumeration.error);
        }
        values.push_back({
            {"index", index}, {"name", wide_to_utf8(effect_name.c_str())},
            {"enabled", edit->get_effect_enable(effect)},
            {"locked", edit->get_effect_lock(effect)},
            {"items", std::move(enumeration.items)},
        });
    }
    call->result = {
        {"context", context_json(edit)},
        {"object", object_json(edit, object, false, false)},
        {"frame", frame},
        {"effects", std::move(values)},
    };
}

void get_markers(SectionCall* call, EDIT_SECTION* edit) {
    const int count = std::max(0, edit->get_mark_frame_list(nullptr, 0));
    std::vector<int> frames(static_cast<std::size_t>(count));
    const int received = count == 0 ? 0 : edit->get_mark_frame_list(frames.data(), count);
    json markers = json::array();
    for (int index = 0; index < received; ++index) {
        const int frame = frames[static_cast<std::size_t>(index)];
        markers.push_back({{"frame", frame}, {"memo", wide_to_utf8(edit->get_mark_frame_memo(frame))}});
    }
    call->result = {{"context", context_json(edit)}, {"markers", std::move(markers)}};
}

void get_bpm_grid(SectionCall* call, EDIT_SECTION* edit) {
    const int count = std::max(0, edit->get_grid_bpm_list(nullptr, 0, sizeof(BPM_INFO)));
    std::vector<BPM_INFO> points(static_cast<std::size_t>(count));
    const int received = count == 0 ? 0 : edit->get_grid_bpm_list(points.data(), count, sizeof(BPM_INFO));
    json result = json::array();
    for (int index = 0; index < received; ++index) {
        const BPM_INFO& point = points[static_cast<std::size_t>(index)];
        result.push_back({{"tempo", point.tempo}, {"beat", point.beat},
                          {"start", point.start}, {"offset", point.offset}});
    }
    call->result = {{"context", context_json(edit)}, {"points", std::move(result)}};
}

void read_section_callback(void* parameter, EDIT_SECTION* edit) noexcept {
    auto* call = static_cast<SectionCall*>(parameter);
    try {
        if (call->method == "get_context") {
            call->result = context_json(edit);
        } else if (call->method == "inspect_timeline") {
            inspect_timeline(call, edit);
        } else if (call->method == "inspect_object") {
            inspect_object(call, edit);
        } else if (call->method == "inspect_objects") {
            inspect_objects(call, edit);
        } else if (call->method == "get_selection") {
            get_selection(call, edit);
        } else if (call->method == "preflight_media") {
            preflight_media(call, edit);
        } else if (call->method == "inspect_object_values") {
            inspect_object_values(call, edit);
        } else if (call->method == "get_markers") {
            get_markers(call, edit);
        } else if (call->method == "get_bpm_grid") {
            get_bpm_grid(call, edit);
        } else {
            throw BridgeError("METHOD_NOT_FOUND", "unknown read method: " + call->method);
        }
    } catch (...) {
        store_exception(call);
    }
}

EFFECT_HANDLE effect_at(EDIT_SECTION* edit, OBJECT_HANDLE object, int index) {
    const int count = edit->get_effect_list(object, nullptr, 0);
    if (index < 0 || index >= count) {
        throw BridgeError("INVALID_ARGUMENT", "effect_index is out of range");
    }
    std::vector<EFFECT_HANDLE> effects(static_cast<std::size_t>(count));
    const int received = edit->get_effect_list(object, effects.data(), count);
    if (index >= received || effects[static_cast<std::size_t>(index)] == nullptr) {
        throw BridgeError("HOST_ERROR", "could not resolve effect_index");
    }
    return effects[static_cast<std::size_t>(index)];
}

OBJECT_HANDLE operation_object(const json& operation, int operation_index,
                               const std::vector<OBJECT_HANDLE>& created) {
    if (operation.contains("result_ref") && !operation.at("result_ref").is_null()) {
        const int reference = operation.at("result_ref").get<int>();
        if (reference < 0 || reference >= operation_index ||
            reference >= static_cast<int>(created.size()) || created[static_cast<std::size_t>(reference)] == nullptr) {
            throw BridgeError("INVALID_ARGUMENT", "result_ref must refer to an earlier object-creating operation");
        }
        return created[static_cast<std::size_t>(reference)];
    }
    return resolve_object(operation.at("object_id").get<std::uint64_t>());
}

void apply_properties(EDIT_SECTION* edit, OBJECT_HANDLE object, const json& properties) {
    if (!properties.is_array()) {
        return;
    }
    for (const json& property : properties) {
        if (!property.is_object()) {
            throw BridgeError("INVALID_ARGUMENT", "each property update must be an object");
        }
        const std::wstring effect = utf8_to_wide(property.at("effect").get<std::string>());
        const std::wstring item = utf8_to_wide(property.at("item").get<std::string>());
        const std::string value = property.at("value").get<std::string>();
        if (effect.empty() || item.empty()) {
            throw BridgeError("INVALID_ARGUMENT", "property effect and item are required");
        }
        if (!edit->set_object_item_value(object, effect.c_str(), item.c_str(), value.c_str())) {
            throw BridgeError("HOST_REJECTED", "AviUtl2 rejected property " + property.at("item").get<std::string>());
        }
    }
}

struct FileItemCollector {
    std::vector<std::wstring> names;
};

void collect_file_item(void* parameter, LPCWSTR name, int type) noexcept {
    if (type != EDIT_HANDLE::EFFECT_ITEM_TYPE_FILE || name == nullptr) {
        return;
    }
    try {
        static_cast<FileItemCollector*>(parameter)->names.emplace_back(name);
    } catch (...) {
    }
}

std::pair<EFFECT_HANDLE, std::wstring> resolve_media_item(
    EDIT_SECTION* edit, OBJECT_HANDLE object, const json& operation) {
    const std::string requested_effect = operation.value("effect", "");
    const std::string requested_item = operation.value("item", "");
    if (!requested_effect.empty()) {
        const std::wstring effect_name = utf8_to_wide(requested_effect);
        EFFECT_HANDLE effect = edit->find_effect(object, effect_name.c_str());
        if (effect == nullptr) {
            throw BridgeError("NOT_FOUND", "media effect was not found");
        }
        if (!requested_item.empty()) {
            return {effect, utf8_to_wide(requested_item)};
        }
        FileItemCollector items;
        edit_handle->enum_effect_item(effect_name.c_str(), &items, collect_file_item);
        if (items.names.size() != 1) {
            throw BridgeError("INVALID_ARGUMENT", "effect must have exactly one file item or item must be specified");
        }
        return {effect, items.names.front()};
    }

    const int count = std::max(0, edit->get_effect_list(object, nullptr, 0));
    std::vector<EFFECT_HANDLE> effects(static_cast<std::size_t>(count));
    const int received = count == 0 ? 0 : edit->get_effect_list(object, effects.data(), count);
    std::vector<std::pair<EFFECT_HANDLE, std::wstring>> candidates;
    for (int index = 0; index < received; ++index) {
        EFFECT_HANDLE effect = effects[static_cast<std::size_t>(index)];
        const wchar_t* name = edit->get_effect_name(effect);
        if (name == nullptr) {
            continue;
        }
        const std::wstring effect_name(name);
        FileItemCollector items;
        edit_handle->enum_effect_item(effect_name.c_str(), &items, collect_file_item);
        for (const auto& item : items.names) {
            candidates.emplace_back(effect, item);
        }
    }
    if (candidates.size() != 1) {
        throw BridgeError("INVALID_ARGUMENT", "object must have exactly one file item or effect/item must be specified");
    }
    return candidates.front();
}

json execute_operations(const json& operations, EDIT_SECTION* edit) {
    if (!operations.is_array() || operations.empty() || operations.size() > kMaxBatchOperations) {
        throw BridgeError("INVALID_ARGUMENT", "operations must contain between 1 and 100 entries");
    }
    std::vector<OBJECT_HANDLE> created(operations.size(), nullptr);
    json results = json::array();

    for (std::size_t raw_index = 0; raw_index < operations.size(); ++raw_index) {
        const int index = static_cast<int>(raw_index);
        const json& operation = operations[raw_index];
        if (!operation.is_object()) {
            throw BridgeError("INVALID_ARGUMENT", "each batch operation must be an object");
        }
        const std::string op = operation.at("op").get<std::string>();
        json result{{"index", index}, {"op", op}, {"changed", false}};

        if (op == "add_text") {
            const int layer = operation.at("layer").get<int>();
            const int frame = operation.at("frame").get<int>();
            const int length = operation.at("length").get<int>();
            const std::string text = operation.at("text").get<std::string>();
            const double size_value = operation.value("size", 34.0);
            const std::string color = operation.value("color", "ffffff");
            if (text.empty() || layer < 0 || frame < 0 || length < 1 ||
                size_value <= 0.0 || !is_hex_color(color)) {
                throw BridgeError("INVALID_ARGUMENT", "add_text requires valid layer, frame, and length");
            }
            OBJECT_HANDLE object = edit->create_object(L"テキスト", layer, frame, length);
            if (object == nullptr) {
                throw BridgeError("HOST_REJECTED", "AviUtl2 could not create a text object");
            }
            created[raw_index] = object;
            const std::string size = std::to_string(size_value);
            if (!edit->set_object_item_value(object, L"テキスト", L"テキスト", text.c_str()) ||
                !edit->set_object_item_value(object, L"テキスト", L"サイズ", size.c_str()) ||
                !edit->set_object_item_value(object, L"テキスト", L"文字色", color.c_str())) {
                throw BridgeError("HOST_REJECTED", "AviUtl2 rejected a text property");
            }
            const std::uint64_t id = register_object(object);
            result["object_id"] = id;
            result["changed"] = true;
        } else if (op == "add_media") {
            const int layer = operation.at("layer").get<int>();
            const int frame = operation.at("frame").get<int>();
            const int length = operation.value("length", 0);
            const std::string file_utf8 = operation.at("file").get<std::string>();
            if (file_utf8.empty() || layer < 0 || frame < 0 || length < 0) {
                throw BridgeError("INVALID_ARGUMENT", "add_media requires valid file, layer, frame, and length");
            }
            const std::wstring file = utf8_to_wide(file_utf8);
            OBJECT_HANDLE object = edit->create_object_from_media_file(
                file.c_str(), layer, frame, length);
            if (object == nullptr) {
                throw BridgeError("HOST_REJECTED", "AviUtl2 could not create a media object");
            }
            created[raw_index] = object;
            const std::uint64_t id = register_object(object);
            result["object_id"] = id;
            result["changed"] = true;
        } else if (op == "duplicate_object") {
            OBJECT_HANDLE source = operation_object(operation, index, created);
            const char* alias_value = edit->get_object_alias(source);
            if (alias_value == nullptr) {
                throw BridgeError("HOST_REJECTED", "AviUtl2 could not export the source object alias");
            }
            const std::string alias(alias_value);
            const OBJECT_LAYER_FRAME source_placement = edit->get_object_layer_frame(source);
            const int layer = operation.at("layer").get<int>();
            const int frame = operation.at("frame").get<int>();
            const int length = operation.value(
                "length", source_placement.end - source_placement.start + 1);
            if (layer < 0 || frame < 0 || length < 1) {
                throw BridgeError("INVALID_ARGUMENT", "duplicate_object requires valid layer, frame, and length");
            }
            OBJECT_HANDLE object = edit->create_object_from_alias(alias.c_str(), layer, frame, length);
            if (object == nullptr) {
                throw BridgeError("HOST_REJECTED", "AviUtl2 could not duplicate the object");
            }
            created[raw_index] = object;
            const std::uint64_t id = register_object(object);
            result["object_id"] = id;
            result["changed"] = true;
        } else if (op == "replace_media") {
            OBJECT_HANDLE object = operation_object(operation, index, created);
            const std::string file_utf8 = operation.at("file").get<std::string>();
            if (file_utf8.empty()) {
                throw BridgeError("INVALID_ARGUMENT", "replacement media file is required");
            }
            const std::wstring file = utf8_to_wide(file_utf8);
            if (!edit->is_support_media_file(file.c_str(), true)) {
                throw BridgeError("HOST_REJECTED", "AviUtl2 does not support the replacement media file");
            }
            const auto [effect, item] = resolve_media_item(edit, object, operation);
            if (!edit->set_effect_item_value(effect, item.c_str(), file_utf8.c_str())) {
                throw BridgeError("HOST_REJECTED", "AviUtl2 rejected the replacement media path");
            }
            result["object_id"] = register_object(object);
            result["changed"] = true;
        } else if (op == "update_object") {
            OBJECT_HANDLE object = operation_object(operation, index, created);
            const bool has_update =
                (operation.contains("layer") && !operation.at("layer").is_null()) ||
                (operation.contains("frame") && !operation.at("frame").is_null()) ||
                (operation.contains("name") && !operation.at("name").is_null()) ||
                (operation.contains("properties") && !operation.at("properties").empty());
            if (!has_update) {
                throw BridgeError("INVALID_ARGUMENT", "update_object requires at least one update");
            }
            const OBJECT_LAYER_FRAME placement = edit->get_object_layer_frame(object);
            const int layer = operation.contains("layer") && !operation.at("layer").is_null()
                                  ? operation.at("layer").get<int>() : placement.layer;
            const int frame = operation.contains("frame") && !operation.at("frame").is_null()
                                  ? operation.at("frame").get<int>() : placement.start;
            if (layer < 0 || frame < 0) {
                throw BridgeError("INVALID_ARGUMENT", "object layer and frame must be non-negative");
            }
            if ((layer != placement.layer || frame != placement.start) && !edit->move_object(object, layer, frame)) {
                throw BridgeError("HOST_REJECTED", "AviUtl2 could not move the object");
            }
            if (operation.contains("name") && !operation.at("name").is_null()) {
                const std::wstring name = utf8_to_wide(operation.at("name").get<std::string>());
                edit->set_object_name(object, name.c_str());
            }
            if (operation.contains("properties")) {
                apply_properties(edit, object, operation.at("properties"));
            }
            result["object_id"] = register_object(object);
            result["changed"] = true;
        } else if (op == "delete_object") {
            if (operation.contains("result_ref") && !operation.at("result_ref").is_null()) {
                throw BridgeError("INVALID_ARGUMENT", "an object created in this batch cannot be deleted in the same edit section");
            }
            OBJECT_HANDLE object = operation_object(operation, index, created);
            edit->delete_object(object);
            unregister_object(object);
            result["changed"] = true;
        } else if (op == "create_section") {
            OBJECT_HANDLE object = operation_object(operation, index, created);
            const int frame = operation.at("frame").get<int>();
            const OBJECT_LAYER_FRAME placement = edit->get_object_layer_frame(object);
            if (frame <= placement.start || frame > placement.end) {
                throw BridgeError("INVALID_ARGUMENT", "section frame must be inside the object");
            }
            if (!edit->create_object_section(object, frame)) {
                throw BridgeError("HOST_REJECTED", "AviUtl2 could not create the section");
            }
            result["object_id"] = register_object(object);
            result["changed"] = true;
        } else if (op == "delete_section") {
            OBJECT_HANDLE object = operation_object(operation, index, created);
            const int section = operation.at("section").get<int>();
            const int count = edit->get_object_section_num(object);
            if (section < 1 || section >= count) {
                throw BridgeError("INVALID_ARGUMENT", "section is not a deletable intermediate section");
            }
            if (!edit->delete_object_section(object, section)) {
                throw BridgeError("HOST_REJECTED", "AviUtl2 could not delete the section");
            }
            result["object_id"] = register_object(object);
            result["changed"] = true;
        } else if (op == "move_section") {
            OBJECT_HANDLE object = operation_object(operation, index, created);
            const int section = operation.at("section").get<int>();
            const int frame = operation.at("frame").get<int>();
            const int count = edit->get_object_section_num(object);
            if (section < 0 || section > count || frame < 0) {
                throw BridgeError("INVALID_ARGUMENT", "invalid section or frame");
            }
            if (!edit->move_object_section(object, section, frame)) {
                throw BridgeError("HOST_REJECTED", "AviUtl2 could not move the section");
            }
            result["object_id"] = register_object(object);
            result["changed"] = true;
        } else if (op == "set_layer_state") {
            const int layer = operation.at("layer").get<int>();
            if (layer < 0) {
                throw BridgeError("INVALID_ARGUMENT", "layer must be non-negative");
            }
            bool changed = false;
            if (operation.contains("name") && !operation.at("name").is_null()) {
                const std::wstring name = utf8_to_wide(operation.at("name").get<std::string>());
                edit->set_layer_name(layer, name.c_str());
                changed = true;
            }
            if (operation.contains("enabled") && !operation.at("enabled").is_null()) {
                edit->set_layer_enable(layer, operation.at("enabled").get<bool>());
                changed = true;
            }
            if (operation.contains("locked") && !operation.at("locked").is_null()) {
                edit->set_layer_lock(layer, operation.at("locked").get<bool>());
                changed = true;
            }
            if (!changed) {
                throw BridgeError("INVALID_ARGUMENT", "set_layer_state requires a state update");
            }
            result["changed"] = true;
        } else if (op == "set_scene_settings") {
            bool changed = false;
            if (operation.contains("name") && !operation.at("name").is_null()) {
                const std::wstring name = utf8_to_wide(operation.at("name").get<std::string>());
                edit->set_scene_name(name.c_str());
                changed = true;
            }
            const bool has_width = operation.contains("width") && !operation.at("width").is_null();
            const bool has_height = operation.contains("height") && !operation.at("height").is_null();
            if (has_width != has_height) {
                throw BridgeError("INVALID_ARGUMENT", "width and height must be specified together");
            }
            if (has_width) {
                const int width = operation.at("width").get<int>();
                const int height = operation.at("height").get<int>();
                if (width < 1 || height < 1) {
                    throw BridgeError("INVALID_ARGUMENT", "scene dimensions must be positive");
                }
                edit->set_scene_size(width, height);
                changed = true;
            }
            const bool has_rate = operation.contains("rate") && !operation.at("rate").is_null();
            const bool has_scale = operation.contains("scale") && !operation.at("scale").is_null();
            if (has_rate != has_scale) {
                throw BridgeError("INVALID_ARGUMENT", "rate and scale must be specified together");
            }
            if (has_rate) {
                const int rate = operation.at("rate").get<int>();
                const int scale = operation.at("scale").get<int>();
                if (rate < 1 || scale < 1) {
                    throw BridgeError("INVALID_ARGUMENT", "scene frame rate must be positive");
                }
                edit->set_scene_frame_rate(rate, scale);
                changed = true;
            }
            if (operation.contains("sample_rate") && !operation.at("sample_rate").is_null()) {
                const int sample_rate = operation.at("sample_rate").get<int>();
                if (sample_rate < 1) {
                    throw BridgeError("INVALID_ARGUMENT", "sample_rate must be positive");
                }
                edit->set_scene_sample_rate(sample_rate);
                changed = true;
            }
            if (!changed) {
                throw BridgeError("INVALID_ARGUMENT", "set_scene_settings requires an update");
            }
            result["changed"] = true;
        } else if (op == "set_marker") {
            const int frame = operation.at("frame").get<int>();
            if (frame < 0) {
                throw BridgeError("INVALID_ARGUMENT", "marker frame must be non-negative");
            }
            const std::wstring memo = utf8_to_wide(operation.value("memo", ""));
            edit->set_mark_frame(frame, memo.c_str());
            result["changed"] = true;
        } else if (op == "set_grid_bpm") {
            const float tempo = operation.at("tempo").get<float>();
            const int beat = operation.at("beat").get<int>();
            const float offset = operation.value("offset", 0.0F);
            if (!std::isfinite(tempo) || tempo <= 0.0F || beat < 1 ||
                !std::isfinite(offset)) {
                throw BridgeError("INVALID_ARGUMENT", "invalid BPM grid values");
            }
            edit->set_grid_bpm(tempo, beat, offset);
            result["changed"] = true;
        } else if (op == "set_grid_bpm_list") {
            const json& values = operation.at("bpm_points");
            if (!values.is_array() || values.empty() || values.size() > 100) {
                throw BridgeError("INVALID_ARGUMENT", "bpm_points must contain between 1 and 100 entries");
            }
            std::vector<BPM_INFO> points;
            points.reserve(values.size());
            for (const json& value : values) {
                BPM_INFO point{value.at("tempo").get<float>(), value.at("beat").get<int>(),
                               value.at("start").get<double>(), value.value("offset", 0.0F)};
                if (!std::isfinite(point.tempo) || point.tempo <= 0.0F || point.beat < 1 ||
                    !std::isfinite(point.start) || point.start < 0.0 || !std::isfinite(point.offset)) {
                    throw BridgeError("INVALID_ARGUMENT", "invalid BPM point");
                }
                points.push_back(point);
            }
            edit->set_grid_bpm_list(points.data(), static_cast<int>(points.size()), sizeof(BPM_INFO));
            result["changed"] = true;
        } else if (op == "clear_marker") {
            const int frame = operation.at("frame").get<int>();
            if (frame < 0) {
                throw BridgeError("INVALID_ARGUMENT", "marker frame must be non-negative");
            }
            edit->clear_mark_frame(frame);
            result["changed"] = true;
        } else if (op == "move_marker") {
            const int frame = operation.at("frame").get<int>();
            const int frame_to = operation.at("frame_to").get<int>();
            if (frame < 0 || frame_to < 0) {
                throw BridgeError("INVALID_ARGUMENT", "marker frames must be non-negative");
            }
            if (!edit->move_mark_frame(frame, frame_to)) {
                throw BridgeError("HOST_REJECTED", "AviUtl2 could not move the marker");
            }
            result["changed"] = true;
        } else if (op == "set_cursor") {
            const int layer = operation.at("layer").get<int>();
            const int frame = operation.at("frame").get<int>();
            if (layer < 0 || frame < 0) {
                throw BridgeError("INVALID_ARGUMENT", "cursor layer and frame must be non-negative");
            }
            edit->set_cursor_layer_frame(layer, frame);
            result["changed"] = true;
        } else if (op == "set_display") {
            const int layer = operation.at("layer").get<int>();
            const int frame = operation.at("frame").get<int>();
            if (layer < 0 || frame < 0) {
                throw BridgeError("INVALID_ARGUMENT", "display layer and frame must be non-negative");
            }
            edit->set_display_layer_frame(layer, frame);
            result["changed"] = true;
        } else if (op == "set_selection_range") {
            const int start = operation.at("start").get<int>();
            const int end = operation.at("end").get<int>();
            if (!((start == -1 && end == -1) || (start >= 0 && end >= start))) {
                throw BridgeError("INVALID_ARGUMENT", "invalid selection range");
            }
            edit->set_select_range(start, end);
            result["changed"] = true;
        } else if (op == "add_effect") {
            OBJECT_HANDLE object = operation_object(operation, index, created);
            const std::string effect_name = operation.at("effect").get<std::string>();
            if (effect_name.empty()) {
                throw BridgeError("INVALID_ARGUMENT", "effect is required");
            }
            const std::wstring name = utf8_to_wide(effect_name);
            if (edit->create_effect(object, name.c_str()) == nullptr) {
                throw BridgeError("HOST_REJECTED", "AviUtl2 could not add the effect");
            }
            result["object_id"] = register_object(object);
            result["changed"] = true;
        } else if (op == "delete_effect") {
            OBJECT_HANDLE object = operation_object(operation, index, created);
            EFFECT_HANDLE effect = effect_at(edit, object, operation.at("effect_index").get<int>());
            if (!edit->delete_effect(object, effect)) {
                throw BridgeError("HOST_REJECTED", "AviUtl2 could not delete the effect");
            }
            result["object_id"] = register_object(object);
            result["changed"] = true;
        } else if (op == "set_effect_state") {
            OBJECT_HANDLE object = operation_object(operation, index, created);
            EFFECT_HANDLE effect = effect_at(edit, object, operation.at("effect_index").get<int>());
            if ((!operation.contains("enabled") || operation.at("enabled").is_null()) &&
                (!operation.contains("locked") || operation.at("locked").is_null()) &&
                (!operation.contains("index") || operation.at("index").is_null())) {
                throw BridgeError("INVALID_ARGUMENT", "set_effect_state requires a state update");
            }
            if (operation.contains("enabled") && !operation.at("enabled").is_null()) {
                edit->set_effect_enable(effect, operation.at("enabled").get<bool>());
            }
            if (operation.contains("locked") && !operation.at("locked").is_null()) {
                edit->set_effect_lock(effect, operation.at("locked").get<bool>());
            }
            if (operation.contains("index") && !operation.at("index").is_null() &&
                operation.at("index").get<int>() < 0) {
                throw BridgeError("INVALID_ARGUMENT", "effect index must be non-negative");
            }
            if (operation.contains("index") && !operation.at("index").is_null()) {
                if (edit->move_effect(object, effect, operation.at("index").get<int>()) < 0) {
                    throw BridgeError("HOST_REJECTED", "AviUtl2 could not reorder the effect");
                }
            }
            result["object_id"] = register_object(object);
            result["changed"] = true;
        } else {
            throw BridgeError("INVALID_ARGUMENT", "unknown batch operation: " + op);
        }
        results.push_back(std::move(result));
    }
    return results;
}

json single_operation(const std::string& method, const json& params) {
    json operation = params;
    operation["op"] = method;
    if (method == "delete_object") {
        operation["op"] = "delete_object";
    }
    return json::array({std::move(operation)});
}

void edit_section_callback(void* parameter, EDIT_SECTION* edit) noexcept {
    auto* call = static_cast<SectionCall*>(parameter);
    try {
        check_expected_context(call->expected, edit);
        json operations;
        if (call->method == "execute_batch") {
            operations = call->params.at("operations");
        } else {
            operations = single_operation(call->method, call->params);
        }
        call->result = {
            {"context", context_json(edit)},
            {"results", execute_operations(operations, edit)},
        };
        call->result["context"] = context_json(edit);
    } catch (...) {
        store_exception(call);
    }
}

struct EnumerationCall {
    json values = json::array();
    std::string error;
};

void collect_effect(void* parameter, LPCWSTR name, int type, int flags) noexcept {
    auto* call = static_cast<EnumerationCall*>(parameter);
    try {
        call->values.push_back({{"name", wide_to_utf8(name)}, {"type", type}, {"flags", flags}});
    } catch (const std::exception& error) {
        call->error = error.what();
    } catch (...) {
        call->error = "unknown effect enumeration error";
    }
}

void collect_effect_item(void* parameter, LPCWSTR name, int type) noexcept {
    auto* call = static_cast<EnumerationCall*>(parameter);
    try {
        call->values.push_back({{"name", wide_to_utf8(name)}, {"type", type}});
    } catch (const std::exception& error) {
        call->error = error.what();
    } catch (...) {
        call->error = "unknown effect item enumeration error";
    }
}

const char kBase64Alphabet[] =
    "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

std::string base64_encode(const std::vector<std::uint8_t>& input) {
    std::string output;
    output.reserve(((input.size() + 2) / 3) * 4);
    for (std::size_t index = 0; index < input.size(); index += 3) {
        const std::uint32_t a = input[index];
        const std::uint32_t b = index + 1 < input.size() ? input[index + 1] : 0;
        const std::uint32_t c = index + 2 < input.size() ? input[index + 2] : 0;
        const std::uint32_t value = (a << 16U) | (b << 8U) | c;
        output.push_back(kBase64Alphabet[(value >> 18U) & 63U]);
        output.push_back(kBase64Alphabet[(value >> 12U) & 63U]);
        output.push_back(index + 1 < input.size() ? kBase64Alphabet[(value >> 6U) & 63U] : '=');
        output.push_back(index + 2 < input.size() ? kBase64Alphabet[value & 63U] : '=');
    }
    return output;
}

struct RenderCall {
    int max_width = 640;
    int max_height = 640;
    int frame = 0;
    int width = 0;
    int height = 0;
    std::vector<std::uint8_t> rgba;
    std::string error;
};

void collect_render(void* parameter, int frame, const void* buffer, int width, int height,
                    int pitch) noexcept {
    auto* call = static_cast<RenderCall*>(parameter);
    try {
        if (buffer == nullptr || width <= 0 || height <= 0 || pitch == 0) {
            call->error = "AviUtl2 returned an empty preview";
            return;
        }
        const double scale = std::min({1.0, static_cast<double>(call->max_width) / width,
                                      static_cast<double>(call->max_height) / height});
        call->width = std::max(1, static_cast<int>(std::floor(width * scale)));
        call->height = std::max(1, static_cast<int>(std::floor(height * scale)));
        call->frame = frame;
        call->rgba.resize(static_cast<std::size_t>(call->width) * call->height * 4);
        const auto* pixels = static_cast<const std::uint8_t*>(buffer);
        for (int y = 0; y < call->height; ++y) {
            const int source_y = std::min(height - 1, y * height / call->height);
            const auto* row = pitch > 0
                                  ? pixels + static_cast<std::ptrdiff_t>(source_y) * pitch
                                  : pixels + static_cast<std::ptrdiff_t>(height - 1 - source_y) * (-pitch);
            for (int x = 0; x < call->width; ++x) {
                const int source_x = std::min(width - 1, x * width / call->width);
                const auto* source = row + static_cast<std::ptrdiff_t>(source_x) * 4;
                auto* target = call->rgba.data() +
                               (static_cast<std::size_t>(y) * call->width + x) * 4;
                std::copy_n(source, 4, target);
            }
        }
    } catch (const std::exception& error) {
        call->error = error.what();
    } catch (...) {
        call->error = "unknown preview callback error";
    }
}

json render_preview(const json& params) {
    RenderCall render;
    render.frame = params.at("frame").get<int>();
    if (render.frame < 0) {
        throw BridgeError("INVALID_ARGUMENT", "preview frame must be non-negative");
    }
    render.max_width = std::clamp(params.value("max_width", 640), 1, 800);
    render.max_height = std::clamp(params.value("max_height", 640), 1, 800);

    bool scheduled = false;
    const std::uint64_t object_id = params.value("object_id", std::uint64_t{0});
    if (object_id == 0) {
        scheduled = edit_handle->rendering_scene_video(render.frame, &render, collect_render);
    } else {
        scheduled = edit_handle->rendering_object_video(
            resolve_object(object_id), render.frame, params.value("apply_effects", false),
            &render, collect_render);
    }
    if (!scheduled) {
        throw BridgeError("HOST_REJECTED", "AviUtl2 could not schedule preview rendering", true);
    }
    edit_handle->wait_rendering_task();
    if (!render.error.empty()) {
        throw BridgeError("HOST_ERROR", render.error);
    }
    if (render.rgba.empty()) {
        throw BridgeError("HOST_ERROR", "preview callback did not return pixels");
    }
    return {
        {"frame", render.frame}, {"width", render.width}, {"height", render.height},
        {"rgba_base64", base64_encode(render.rgba)},
        {"session_id", session_id},
        {"generation", generation.load(std::memory_order_acquire)},
    };
}

json call_section(const std::string& method, const json& params, const json& expected,
                  bool editing) {
    SectionCall call{method, params, expected};
    const bool called = editing
                            ? edit_handle->call_edit_section_param(&call, edit_section_callback)
                            : edit_handle->call_read_section_param(&call, read_section_callback);
    if (!called) {
        throw BridgeError("EDIT_UNAVAILABLE", editing ? "AviUtl2 is not currently editable"
                                                       : "AviUtl2 is not currently readable",
                          true);
    }
    if (!call.error_code.empty()) {
        throw BridgeError(call.error_code, call.error_message, call.retryable);
    }
    return std::move(call.result);
}

json dispatch(const json& request) {
    if (!request.is_object()) {
        throw BridgeError("INVALID_REQUEST", "request must be a JSON object");
    }
    if (request.value("version", 0U) != kProtocolVersion) {
        throw BridgeError("UNSUPPORTED_VERSION", "unsupported bridge protocol version");
    }
    if (edit_handle == nullptr) {
        throw BridgeError("HOST_UNAVAILABLE", "AviUtl2 edit handle is unavailable", true);
    }
    const std::string method = request.value("method", "");
    const json params = request.value("params", json::object());
    const json expected = request.value("context", json(nullptr));

    if (method == "ping") {
        return {{"pong", true}, {"session_id", session_id},
                {"generation", generation.load(std::memory_order_acquire)}};
    }
    if (method == "diagnostics") {
        EnumerationCall call;
        edit_handle->enum_module_info(&call, [](void* parameter, MODULE_INFO* info) {
            auto* enumeration = static_cast<EnumerationCall*>(parameter);
            try {
                enumeration->values.push_back({{"type", info->type}, {"name", wide_to_utf8(info->name)},
                                               {"information", wide_to_utf8(info->information)}});
            } catch (const std::exception& error) {
                enumeration->error = error.what();
            }
        });
        if (!call.error.empty()) throw BridgeError("HOST_ERROR", call.error);
        return {{"session_id", session_id}, {"generation", generation.load()},
                {"protocol_version", kProtocolVersion}, {"required_aviutl2_version", kRequiredVersion},
                {"host_version", host_version}, {"modules", std::move(call.values)}};
    }
    if (method == "list_effects") {
        EnumerationCall call;
        edit_handle->enum_effect_name(&call, collect_effect);
        if (!call.error.empty()) {
            throw BridgeError("HOST_ERROR", call.error);
        }
        return std::move(call.values);
    }
    if (method == "list_effect_items") {
        EnumerationCall call;
        const std::string effect_utf8 = params.at("effect").get<std::string>();
        const std::wstring effect = utf8_to_wide(effect_utf8);
        if (!edit_handle->enum_effect_item(effect.c_str(), &call, collect_effect_item)) {
            throw BridgeError("NOT_FOUND", "effect was not found: " + effect_utf8);
        }
        if (!call.error.empty()) {
            throw BridgeError("HOST_ERROR", call.error);
        }
        return {{"effect", effect_utf8}, {"items", std::move(call.values)}};
    }
    if (method == "render_preview") {
        return render_preview(params);
    }

    const bool editing = method == "add_text" || method == "add_media" ||
                         method == "update_object" || method == "delete_object" ||
                         method == "add_effect" || method == "delete_effect" ||
                         method == "set_effect_state" || method == "execute_batch";
    return call_section(method, params, expected, editing);
}

void serve_client(SOCKET socket) {
    while (running.load(std::memory_order_acquire)) {
        std::string payload;
        if (!read_frame(socket, payload)) {
            return;
        }

        std::uint64_t id = 0;
        json response;
        try {
            const json request = json::parse(payload);
            if (request.is_object() && request.contains("id") && request.at("id").is_number_unsigned()) {
                id = request.at("id").get<std::uint64_t>();
            }
            response = {{"id", id}, {"version", kProtocolVersion}, {"result", dispatch(request)}};
        } catch (const BridgeError& error) {
            response = error_response(id, error.code(), error.what(), error.retryable());
        } catch (const json::exception& error) {
            response = error_response(id, "INVALID_JSON", error.what());
        } catch (const std::exception& error) {
            response = error_response(id, "INTERNAL_ERROR", error.what());
        } catch (...) {
            response = error_response(id, "INTERNAL_ERROR", "unknown request error");
        }

        const std::string encoded = response.dump(-1, ' ', false, json::error_handler_t::replace);
        if (!write_frame(socket, encoded)) {
            return;
        }
    }
}

void server_main() noexcept {
    WSADATA winsock_data{};
    if (WSAStartup(MAKEWORD(2, 2), &winsock_data) != 0) {
        log_error(L"AviUtl2 MCP bridge: WSAStartup failed");
        return;
    }

    SOCKET local_listener = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
    if (local_listener == INVALID_SOCKET) {
        log_error(L"AviUtl2 MCP bridge: socket creation failed");
        WSACleanup();
        return;
    }
    BOOL exclusive = TRUE;
    setsockopt(local_listener, SOL_SOCKET, SO_EXCLUSIVEADDRUSE,
               reinterpret_cast<const char*>(&exclusive), sizeof(exclusive));

    sockaddr_in address{};
    address.sin_family = AF_INET;
    address.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
    address.sin_port = htons(kDefaultPort);
    if (bind(local_listener, reinterpret_cast<sockaddr*>(&address), sizeof(address)) == SOCKET_ERROR ||
        listen(local_listener, 1) == SOCKET_ERROR) {
        log_error(L"AviUtl2 MCP bridge: could not listen on 127.0.0.1:28552");
        closesocket(local_listener);
        WSACleanup();
        return;
    }

    {
        std::lock_guard lock(socket_mutex);
        if (!running.load(std::memory_order_acquire)) {
            closesocket(local_listener);
            WSACleanup();
            return;
        }
        listen_socket = local_listener;
    }

    while (running.load(std::memory_order_acquire)) {
        SOCKET accepted = accept(local_listener, nullptr, nullptr);
        if (accepted == INVALID_SOCKET) {
            break;
        }
        {
            std::lock_guard lock(socket_mutex);
            client_socket = accepted;
        }
        serve_client(accepted);
        {
            std::lock_guard lock(socket_mutex);
            if (client_socket == accepted) {
                close_socket(client_socket);
            }
        }
    }

    {
        std::lock_guard lock(socket_mutex);
        if (listen_socket == local_listener) {
            close_socket(listen_socket);
        }
    }
    WSACleanup();
}

void on_project_load(PROJECT_FILE*) noexcept {
    invalidate_objects();
}

void on_scene_change(void*) noexcept {
    invalidate_objects();
}

}  // namespace

extern "C" __declspec(dllexport) DWORD RequiredVersion() {
    return kRequiredVersion;
}

extern "C" __declspec(dllexport) void InitializeLogger(LOG_HANDLE* handle) {
    logger = handle;
}

extern "C" __declspec(dllexport) bool InitializePlugin(DWORD version) {
    host_version = version;
    session_id = make_session_id();
    return true;
}

extern "C" __declspec(dllexport) void UninitializePlugin() {
    running.store(false, std::memory_order_release);
    close_server_sockets();
    if (server_thread.joinable()) {
        server_thread.join();
    }
    invalidate_objects();
    edit_handle = nullptr;
}

extern "C" __declspec(dllexport) COMMON_PLUGIN_TABLE* GetCommonPluginTable() {
    return &plugin_table;
}

extern "C" __declspec(dllexport) void RegisterPlugin(HOST_APP_TABLE* host) {
    if (host == nullptr || running.load(std::memory_order_acquire)) {
        return;
    }
    edit_handle = host->create_edit_handle();
    if (edit_handle == nullptr) {
        log_error(L"AviUtl2 MCP bridge: create_edit_handle failed");
        return;
    }
    host->register_project_load_handler(on_project_load);
    host->register_event_listener(EVENT_TYPE::CHANGE_EDIT_SCENE, nullptr, on_scene_change);
    running.store(true, std::memory_order_release);
    server_thread = std::thread(server_main);
}
