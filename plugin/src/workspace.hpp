// Included inside the bridge's anonymous namespace after call_section().

json list_scenes() {
    SectionCall call{"list_scenes", json::object(), nullptr};
    const bool called = edit_handle->call_read_section_param(&call, [](void* parameter, EDIT_SECTION* edit) noexcept {
        auto* value = static_cast<SectionCall*>(parameter);
        try {
            EnumerationCall enumeration;
            edit_handle->enum_scene_name(&enumeration, [](void* parameter, LPCWSTR name, int id) noexcept {
                auto* scenes = static_cast<EnumerationCall*>(parameter);
                try { scenes->values.push_back({{"id", id}, {"name", wide_to_utf8(name)}}); }
                catch (...) { scenes->error = "could not enumerate scene names"; }
            });
            if (!enumeration.error.empty()) throw BridgeError("HOST_ERROR", enumeration.error);
            auto current = context_json(edit);
            for (auto& scene : enumeration.values) scene["current"] = scene.at("id") == current.at("scene_id");
            value->result = {{"context", std::move(current)}, {"scenes", std::move(enumeration.values)}};
        } catch (...) { store_exception(value); }
    });
    if (!called) throw BridgeError("EDIT_UNAVAILABLE", "AviUtl2 is not currently readable", true);
    if (!call.error_code.empty()) throw BridgeError(call.error_code, call.error_message, call.retryable);
    return std::move(call.result);
}

void check_workspace_context(const json& expected) {
    SectionCall call{"check_workspace_context", json::object(), expected};
    const bool called = edit_handle->call_read_section_param(&call, [](void* parameter, EDIT_SECTION* edit) noexcept {
        auto* value = static_cast<SectionCall*>(parameter);
        try { check_expected_context(value->expected, edit); }
        catch (...) { store_exception(value); }
    });
    if (!called) throw BridgeError("EDIT_UNAVAILABLE", "AviUtl2 is not currently readable", true);
    if (!call.error_code.empty()) throw BridgeError(call.error_code, call.error_message, call.retryable);
}

struct WorkspaceSettings {
    int width, height, rate, scale, sample_rate;
    EDIT_INFO::COLOR background;
};

WorkspaceSettings workspace_settings(const json& params) {
    EDIT_INFO info{};
    edit_handle->get_edit_info(&info, sizeof(info));
    WorkspaceSettings settings{params.value("width", info.width), params.value("height", info.height),
                               params.value("rate", info.rate), params.value("scale", info.scale),
                               params.value("sample_rate", info.sample_rate), info.background};
    if (settings.width < 1 || settings.width > 16384 || settings.height < 1 || settings.height > 16384 ||
        settings.rate < 1 || settings.scale < 1 || settings.sample_rate < 1) {
        throw BridgeError("INVALID_ARGUMENT", "invalid scene dimensions, frame rate, or sample rate");
    }
    const auto background = params.value("background", "");
    if (!background.empty()) {
        if (background.size() != 8 || !is_hex_color(background.substr(0, 6)) ||
            !is_hex_color("0000" + background.substr(6))) {
            throw BridgeError("INVALID_ARGUMENT", "background must be eight hexadecimal digits (RRGGBBAA)");
        }
        settings.background = {
            static_cast<unsigned char>(std::stoul(background.substr(0, 2), nullptr, 16)),
            static_cast<unsigned char>(std::stoul(background.substr(2, 2), nullptr, 16)),
            static_cast<unsigned char>(std::stoul(background.substr(4, 2), nullptr, 16)),
            static_cast<unsigned char>(std::stoul(background.substr(6, 2), nullptr, 16)),
        };
    }
    return settings;
}

std::wstring workspace_path(const std::string& file) {
    if (file.empty() || file.find('\0') != std::string::npos) {
        throw BridgeError("INVALID_ARGUMENT", "an absolute file path is required");
    }
    const auto path = utf8_to_wide(file);
    if (!std::filesystem::path(path).is_absolute()) {
        throw BridgeError("INVALID_ARGUMENT", "an absolute file path is required");
    }
    return path;
}

json list_output_plugins() {
    EnumerationCall call;
    edit_handle->enum_module_info(&call, [](void* parameter, MODULE_INFO* info) noexcept {
        if (info->type != MODULE_INFO::TYPE_PLUGIN_OUTPUT) return;
        auto* enumeration = static_cast<EnumerationCall*>(parameter);
        try {
            enumeration->values.push_back({{"type", info->type}, {"name", wide_to_utf8(info->name)},
                                           {"information", wide_to_utf8(info->information)}});
        } catch (...) { enumeration->error = "could not enumerate output plugins"; }
    });
    if (!call.error.empty()) throw BridgeError("HOST_ERROR", call.error);
    return call.values;
}

std::mutex output_mutex;
std::vector<json> output_jobs;
std::uint64_t next_output_id = 1;

void update_output_state(int state) {
    std::lock_guard lock(output_mutex);
    if (output_jobs.empty()) return;
    auto& job = output_jobs.back();
    const auto previous = job.at("state").get<std::string>();
    if ((previous == "starting" || previous == "running") && state == EDIT_HANDLE::EDIT_STATE_SAVE) {
        job["state"] = "running";
    } else if (previous == "running" && state != EDIT_HANDLE::EDIT_STATE_SAVE) {
        // The SDK reports the end of output, but does not report success vs cancellation/failure.
        job["state"] = "ended";
    }
}

void on_edit_state_change(void*) noexcept {
    try { update_output_state(edit_handle->get_edit_state()); }
    catch (...) { log_error(L"AviUtl2 MCP bridge: could not record output state"); }
}

json output_status(std::uint64_t id) {
    const int state = edit_handle->get_edit_state();
    update_output_state(state);
    std::lock_guard lock(output_mutex);
    json result{{"edit_state", state}};
    if (output_jobs.empty()) {
        if (id != 0) throw BridgeError("NOT_FOUND", "output job was not found");
        return result;
    }
    if (id == 0) result["job"] = output_jobs.back();
    else {
        const auto found = std::find_if(output_jobs.begin(), output_jobs.end(), [id](const json& job) {
            return job.at("id").get<std::uint64_t>() == id;
        });
        if (found == output_jobs.end()) throw BridgeError("NOT_FOUND", "output job was not found or expired");
        result["job"] = *found;
    }
    return result;
}

json start_output(const json& params, const json& expected) {
    const auto file_utf8 = params.at("file").get<std::string>();
    const auto file = workspace_path(file_utf8);
    const auto plugin_utf8 = params.at("output_plugin").get<std::string>();
    const auto plugin = utf8_to_wide(plugin_utf8);
    const auto plugins = list_output_plugins();
    if (plugin_utf8.empty() || plugin_utf8.find('\0') != std::string::npos ||
        std::none_of(plugins.begin(), plugins.end(), [&plugin_utf8](const json& item) {
            return item.at("name") == plugin_utf8;
        })) throw BridgeError("NOT_FOUND", "output_plugin must match a registered output plugin name");
    const auto save_utf8 = params.value("save_project_file", "");
    const auto save = save_utf8.empty() ? std::wstring{} : workspace_path(save_utf8);
    if (!save.empty() && _wcsicmp(std::filesystem::path(save).lexically_normal().c_str(),
                                std::filesystem::path(file).lexically_normal().c_str()) == 0) {
        throw BridgeError("INVALID_ARGUMENT", "project and output paths must be different");
    }
    check_workspace_context(expected);
    if (edit_handle->get_edit_state() != EDIT_HANDLE::EDIT_STATE_EDIT) {
        throw BridgeError("EDIT_UNAVAILABLE", "output requires an idle editing state", true);
    }
    std::uint64_t id;
    {
        std::lock_guard lock(output_mutex);
        if (!output_jobs.empty()) {
            const auto state = output_jobs.back().at("state").get<std::string>();
            if (state == "starting" || state == "running") {
                throw BridgeError("OUTPUT_PENDING", "previous output has not reported its end", true);
            }
        }
        id = next_output_id++;
        if (output_jobs.size() == 20) output_jobs.erase(output_jobs.begin());
        output_jobs.push_back({{"id", id}, {"session_id", session_id}, {"scene_id", expected.at("scene_id")},
                               {"file", file_utf8}, {"output_plugin", plugin_utf8},
                               {"state", "preparing"}, {"outcome", "unknown"}});
    }
    // No read/edit lock is held while invoking project or output APIs.
    if (!save.empty()) {
        if (!edit_handle->save_project_file(save.c_str())) {
            std::lock_guard lock(output_mutex);
            output_jobs.back()["state"] = "rejected";
            output_jobs.back()["outcome"] = "not_started";
            throw BridgeError("HOST_REJECTED", "AviUtl2 could not save the project before output");
        }
        std::lock_guard lock(output_mutex);
        output_jobs.back()["save_project_file"] = save_utf8;
    }
    {
        std::lock_guard lock(output_mutex);
        output_jobs.back()["state"] = "starting";
    }
    if (!edit_handle->output_file(file.c_str(), plugin.c_str(), nullptr, nullptr)) {
        std::lock_guard lock(output_mutex);
        output_jobs.back()["state"] = "rejected";
        output_jobs.back()["outcome"] = "not_started";
        throw BridgeError("HOST_REJECTED", "AviUtl2 rejected the output request; inspect get_output_status");
    }
    return output_status(id);
}

json workspace_operation(const std::string& method, const json& params, const json& expected) {
    if (method == "output_file") return start_output(params, expected);
    check_workspace_context(expected);
    const auto before = generation.load(std::memory_order_acquire);
    bool changed_context = false;
    bool success = false;
    std::string file_utf8;
    if (method == "select_scene") {
        const bool has_id = params.contains("target_scene_id") && !params.at("target_scene_id").is_null();
        const auto name = params.value("name", "");
        if (has_id == !name.empty()) throw BridgeError("INVALID_ARGUMENT", "specify either scene_id or exact scene name");
        int id = has_id ? params.at("target_scene_id").get<int>() : -1;
        if (!has_id) {
            const auto scenes = list_scenes();
            int matches = 0;
            for (const auto& scene : scenes.at("scenes")) {
                if (scene.at("name") == name) { id = scene.at("id").get<int>(); ++matches; }
            }
            if (matches != 1) throw BridgeError("INVALID_ARGUMENT", "scene name must match exactly one scene; use scene_id for duplicate names");
        }
        if (id < 0) throw BridgeError("INVALID_ARGUMENT", "scene_id must be non-negative");
        if (id == expected.at("scene_id").get<int>()) return list_scenes();
        success = edit_handle->select_scene(id);
        changed_context = true;
    } else if (method == "create_scene" || method == "create_project") {
        const auto settings = workspace_settings(params);
        if (method == "create_scene") {
            const auto name_utf8 = params.at("name").get<std::string>();
            const auto label_utf8 = params.value("label", "");
            if (name_utf8.empty() || name_utf8.find('\0') != std::string::npos || label_utf8.find('\0') != std::string::npos) {
                throw BridgeError("INVALID_ARGUMENT", "a nonempty scene name without NUL is required");
            }
            const auto name = utf8_to_wide(name_utf8), label = utf8_to_wide(label_utf8);
            success = edit_handle->create_scene(name.c_str(), label.c_str(), settings.width, settings.height,
                                                settings.rate, settings.scale, settings.sample_rate, settings.background);
        } else {
            success = edit_handle->create_project(settings.width, settings.height, settings.rate, settings.scale,
                                                  settings.sample_rate, settings.background, params.value("show_confirm", false));
        }
        changed_context = true;
    } else {
        file_utf8 = params.at("file").get<std::string>();
        const auto file = workspace_path(file_utf8);
        if (method == "open_project") {
            success = edit_handle->open_project_file(file.c_str(), params.value("show_confirm", false));
            changed_context = true;
        } else success = edit_handle->save_project_file(file.c_str());
    }
    if (!success) throw BridgeError("HOST_REJECTED", "AviUtl2 rejected " + method + " (busy, invalid input, or cancelled)");
    // SDK events normally invalidate IDs; also cover hosts that have not delivered the event yet.
    if (changed_context && generation.load(std::memory_order_acquire) == before) invalidate_objects();
    if (method == "select_scene" || method == "create_scene") return list_scenes();
    return {{"context", call_section("get_context", json::object(), nullptr, false)},
            {"action", method}, {"file", file_utf8}};
}

// Scene/project APIs update UI state. Run them on the host's main thread,
// outside read/edit callbacks (which hold SDK locks). A posted queue also lets
// the server abandon its wait during shutdown without blocking plugin teardown.
constexpr UINT kWorkspaceMessage = WM_APP + 52;
HWND workspace_window = nullptr;
DWORD workspace_thread_id = 0;
std::mutex workspace_queue_mutex;

struct WorkspaceCall {
    SectionCall call;
    HANDLE done = CreateEventW(nullptr, TRUE, FALSE, nullptr);
    ~WorkspaceCall() { if (done != nullptr) CloseHandle(done); }
};

std::vector<std::shared_ptr<WorkspaceCall>> workspace_queue;

LRESULT CALLBACK workspace_window_proc(HWND window, UINT message, WPARAM wparam, LPARAM lparam) {
    if (message != kWorkspaceMessage) return DefWindowProcW(window, message, wparam, lparam);
    std::shared_ptr<WorkspaceCall> value;
    {
        std::lock_guard lock(workspace_queue_mutex);
        if (workspace_queue.empty()) return 0;
        value = workspace_queue.front();
        workspace_queue.erase(workspace_queue.begin());
    }
    try {
        value->call.result = workspace_operation(value->call.method, value->call.params, value->call.expected);
    } catch (...) { store_exception(&value->call); }
    SetEvent(value->done);
    return 0;
}

bool start_workspace_window() {
    HINSTANCE instance = nullptr;
    if (!GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS | GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT,
                           reinterpret_cast<LPCWSTR>(&workspace_window_proc), &instance)) return false;
    WNDCLASSW klass{};
    klass.lpfnWndProc = workspace_window_proc;
    klass.hInstance = instance;
    klass.lpszClassName = L"AviUtl2MCPWorkspaceDispatcher";
    if (!RegisterClassW(&klass) && GetLastError() != ERROR_CLASS_ALREADY_EXISTS) return false;
    workspace_window = CreateWindowExW(0, klass.lpszClassName, L"", 0, 0, 0, 0, 0,
                                      HWND_MESSAGE, nullptr, instance, nullptr);
    workspace_thread_id = GetCurrentThreadId();
    return workspace_window != nullptr;
}

void stop_workspace_window() {
    if (workspace_window != nullptr) {
        HINSTANCE instance = reinterpret_cast<HINSTANCE>(GetWindowLongPtrW(workspace_window, GWLP_HINSTANCE));
        DestroyWindow(workspace_window);
        workspace_window = nullptr;
        UnregisterClassW(L"AviUtl2MCPWorkspaceDispatcher", instance);
    }
    std::lock_guard lock(workspace_queue_mutex);
    workspace_queue.clear();
}

json call_workspace(const std::string& method, const json& params, const json& expected) {
    if (workspace_window == nullptr) throw BridgeError("HOST_UNAVAILABLE", "main-thread dispatcher is unavailable");
    if (GetCurrentThreadId() == workspace_thread_id) return workspace_operation(method, params, expected);
    auto value = std::make_shared<WorkspaceCall>();
    value->call = SectionCall{method, params, expected};
    if (value->done == nullptr) throw BridgeError("HOST_ERROR", "could not create SDK call completion event");
    {
        std::lock_guard lock(workspace_queue_mutex);
        workspace_queue.push_back(value);
        if (!PostMessageW(workspace_window, kWorkspaceMessage, 0, 0)) {
            workspace_queue.pop_back();
            throw BridgeError("HOST_ERROR", "could not queue SDK call on main thread");
        }
    }
    for (;;) {
        const DWORD wait = WaitForSingleObject(value->done, 100);
        if (wait == WAIT_OBJECT_0) break;
        if (wait == WAIT_FAILED || !running.load(std::memory_order_acquire)) {
            throw BridgeError("HOST_UNAVAILABLE", "SDK call interrupted by plugin shutdown");
        }
    }
    if (!value->call.error_code.empty()) throw BridgeError(value->call.error_code, value->call.error_message, value->call.retryable);
    return std::move(value->call.result);
}
