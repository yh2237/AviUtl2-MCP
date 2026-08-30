#include <winsock2.h>
#include <ws2tcpip.h>
#include <windows.h>

#include <algorithm>
#include <atomic>
#include <cstdint>
#include <cwchar>
#include <cstring>
#include <iomanip>
#include <mutex>
#include <sstream>
#include <stdexcept>
#include <string>
#include <thread>
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
        return {};
    }
    std::string output(static_cast<std::size_t>(output_length), '\0');
    if (WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, value, input_length,
                            output.data(), output_length, nullptr, nullptr) == 0) {
        return {};
    }
    return output;
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
                    bool retryable = false) {
    return {
        {"id", id},
        {"version", kProtocolVersion},
        {"error", {
            {"code", std::move(code)},
            {"message", std::move(message)},
            {"retryable", retryable},
        }},
    };
}

struct ContextCollection {
    std::string scene_name;
    std::string error;
};

void collect_context(void* parameter, EDIT_SECTION* edit) noexcept {
    auto* context = static_cast<ContextCollection*>(parameter);
    try {
        context->scene_name = wide_to_utf8(edit->get_scene_name());
    } catch (const std::exception& exception) {
        context->error = exception.what();
    } catch (...) {
        context->error = "unknown exception while reading edit context";
    }
}

json get_context() {
    EDIT_INFO info{};
    edit_handle->get_edit_info(&info, sizeof(info));

    ContextCollection collection;
    if (!edit_handle->call_read_section_param(&collection, collect_context)) {
        throw std::runtime_error("AviUtl2 is not currently available for reading");
    }
    if (!collection.error.empty()) {
        throw std::runtime_error(collection.error);
    }

    return {
        {"session_id", session_id},
        {"generation", generation.load(std::memory_order_acquire)},
        {"scene_id", info.scene_id},
        {"scene_name", collection.scene_name},
        {"width", info.width},
        {"height", info.height},
        {"rate", info.rate},
        {"scale", info.scale},
        {"sample_rate", info.sample_rate},
        {"frame", info.frame},
        {"layer", info.layer},
        {"frame_max", info.frame_max},
        {"layer_max", info.layer_max},
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
}

json dispatch(const json& request) {
    if (!request.is_object()) {
        return error_response(0, "INVALID_REQUEST", "request must be a JSON object");
    }
    const std::uint64_t id = request.value("id", std::uint64_t{0});
    if (request.value("version", 0U) != kProtocolVersion) {
        return error_response(id, "UNSUPPORTED_VERSION", "unsupported bridge protocol version");
    }
    if (edit_handle == nullptr) {
        return error_response(id, "HOST_UNAVAILABLE", "AviUtl2 edit handle is unavailable", true);
    }

    const std::string method = request.value("method", "");
    try {
        json result;
        if (method == "ping") {
            result = {
                {"pong", true},
                {"session_id", session_id},
                {"generation", generation.load(std::memory_order_acquire)},
            };
        } else if (method == "get_context") {
            result = get_context();
        } else {
            return error_response(id, "METHOD_NOT_FOUND", "unknown bridge method: " + method);
        }
        return {{"id", id}, {"version", kProtocolVersion}, {"result", std::move(result)}};
    } catch (const std::exception& exception) {
        return error_response(id, "HOST_ERROR", exception.what(), true);
    } catch (...) {
        return error_response(id, "HOST_ERROR", "unknown native bridge error", true);
    }
}

void serve_client(SOCKET socket) {
    while (running.load(std::memory_order_acquire)) {
        std::string payload;
        if (!read_frame(socket, payload)) {
            return;
        }

        json response;
        try {
            response = dispatch(json::parse(payload));
        } catch (const json::exception& exception) {
            response = error_response(0, "INVALID_JSON", exception.what());
        } catch (const std::exception& exception) {
            response = error_response(0, "INTERNAL_ERROR", exception.what());
        } catch (...) {
            response = error_response(0, "INTERNAL_ERROR", "unknown request error");
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

}  // namespace

extern "C" __declspec(dllexport) void InitializeLogger(LOG_HANDLE* handle) {
    logger = handle;
}

extern "C" __declspec(dllexport) bool InitializePlugin(DWORD) {
    session_id = make_session_id();
    return true;
}

extern "C" __declspec(dllexport) void UninitializePlugin() {
    running.store(false, std::memory_order_release);
    close_server_sockets();
    if (server_thread.joinable()) {
        server_thread.join();
    }
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
    running.store(true, std::memory_order_release);
    server_thread = std::thread(server_main);
}
