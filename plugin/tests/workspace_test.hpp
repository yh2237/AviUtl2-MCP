int workspace_edit_state = EDIT_HANDLE::EDIT_STATE_EDIT;
std::vector<std::pair<int, std::wstring>> mock_scenes{{2, L"Scene"}, {7, L"Sub"}};
std::wstring saved_project, opened_project;
bool workspace_reject = false;
bool workspace_confirm = false;
int workspace_calls = 0;

std::int64_t mock_native_object_id(OBJECT_HANDLE handle) {
    const auto* object = as_object(handle);
    return object->deleted ? 0 : object->native_id;
}
std::int64_t mock_native_effect_id(EFFECT_HANDLE handle) { return as_effect(handle)->native_id; }
bool mock_get_flag(OBJECT_HANDLE handle, OBJECT_FLAG_TYPE type) {
    return as_object(handle)->flags[static_cast<int>(type) - 1];
}
void mock_set_flag(OBJECT_HANDLE handle, OBJECT_FLAG_TYPE type, bool value) {
    assert(mock_section_locked);
    as_object(handle)->flags[static_cast<int>(type) - 1] = value;
}
void mock_scenes_enum(void* param, void (*callback)(void*, LPCWSTR, int)) {
    for (const auto& [id, name] : mock_scenes) callback(param, name.c_str(), id);
}
bool mock_select_scene(int id) {
    assert(GetCurrentThreadId() == workspace_thread_id);
    assert(!mock_section_locked);
    ++workspace_calls;
    if (workspace_reject || std::none_of(mock_scenes.begin(), mock_scenes.end(), [id](const auto& s) { return s.first == id; })) return false;
    mock_info.scene_id = id;
    return true;
}
bool mock_new_scene(LPCWSTR name, LPCWSTR, int width, int height, int rate, int scale,
                    int sample_rate, EDIT_INFO::COLOR background) {
    assert(GetCurrentThreadId() == workspace_thread_id);
    assert(!mock_section_locked);
    ++workspace_calls;
    if (workspace_reject) return false;
    const int id = mock_scenes.back().first + 1;
    mock_scenes.emplace_back(id, name);
    mock_info.scene_id = id;
    mock_info.width = width; mock_info.height = height;
    mock_info.rate = rate; mock_info.scale = scale;
    mock_info.sample_rate = sample_rate; mock_info.background = background;
    on_scene_change(nullptr);
    return true;
}
bool mock_new_project(int width, int height, int rate, int scale, int sample_rate,
                      EDIT_INFO::COLOR background, bool show_confirm) {
    assert(GetCurrentThreadId() == workspace_thread_id);
    assert(!mock_section_locked);
    ++workspace_calls;
    workspace_confirm = show_confirm;
    if (workspace_reject) return false;
    mock_scenes = {{0, L"New"}};
    mock_info.scene_id = 0;
    mock_info.width = width; mock_info.height = height;
    mock_info.rate = rate; mock_info.scale = scale;
    mock_info.sample_rate = sample_rate; mock_info.background = background;
    on_project_load(nullptr);
    return true;
}
bool mock_open_project(LPCWSTR file, bool show_confirm) {
    assert(GetCurrentThreadId() == workspace_thread_id);
    assert(!mock_section_locked);
    ++workspace_calls;
    if (workspace_reject) return false;
    opened_project = file;
    workspace_confirm = show_confirm;
    on_project_load(nullptr);
    return true;
}
bool mock_save_project(LPCWSTR file) {
    assert(GetCurrentThreadId() == workspace_thread_id);
    assert(!mock_section_locked);
    ++workspace_calls;
    if (workspace_reject) return false;
    saved_project = file;
    return true;
}
void mock_output_modules(void* param, void (*callback)(void*, MODULE_INFO*)) {
    mock_enum_modules(param, callback);
    MODULE_INFO output{MODULE_INFO::TYPE_PLUGIN_OUTPUT, L"Mock encoder", L"Output test"};
    callback(param, &output);
}
bool mock_output_file(LPCWSTR, LPCWSTR name, void*, void (*config)(void*, PROJECT_FILE*)) {
    assert(GetCurrentThreadId() == workspace_thread_id);
    assert(!mock_section_locked);
    assert(std::wstring(name) == L"Mock encoder");
    assert(config == nullptr);
    ++workspace_calls;
    if (workspace_reject) return false;
    workspace_edit_state = EDIT_HANDLE::EDIT_STATE_SAVE;
    on_edit_state_change(nullptr);
    return true;
}

void configure_workspace_mock() {
    assert(start_workspace_window());
    mock_section.get_object_id = mock_native_object_id;
    mock_section.get_effect_id = mock_native_effect_id;
    mock_section.get_object_flag = mock_get_flag;
    mock_section.set_object_flag = mock_set_flag;
    mock_handle.enum_scene_name = mock_scenes_enum;
    mock_handle.select_scene = mock_select_scene;
    mock_handle.create_scene = mock_new_scene;
    mock_handle.create_project = mock_new_project;
    mock_handle.open_project_file = mock_open_project;
    mock_handle.save_project_file = mock_save_project;
    mock_handle.output_file = mock_output_file;
    mock_handle.get_edit_state = [] { return workspace_edit_state; };
    mock_info.background = {10, 20, 30, 255};
}

template <class F> void expect_bridge_error(const std::string& code, F action) {
    bool rejected = false;
    try { action(); } catch (const BridgeError& error) { rejected = error.code() == code; }
    assert(rejected);
}

void test_workspace_operations() {
    const auto initial = generation.load();
    const auto id = register_object(mock_objects.front().get(), &mock_section);
    auto inspected = dispatch(request("inspect_object", {{"object_id", id}, {"include_effects", true}}));
    assert(inspected.at("object").at("native_id") == std::to_string(mock_objects.front()->native_id));
    assert(inspected.at("object").at("effects").at(0).at("native_id") == "9007199254740993");
    assert(inspected.at("context").at("background") == "0a141eff");
    auto flag_edit = request("execute_batch", {{"operations", json::array({
        {{"op", "set_object_flags"}, {"object_id", id}, {"flags", {{"enable_camera", false}, {"clipping_object", true}}}}
    })}}, true);
    assert(dispatch(flag_edit).at("results").at(0).at("changed") == true);
    assert(dispatch(flag_edit).at("results").at(0).at("changed") == false);
    assert(mock_objects.front()->flags[0]);
    inspected = dispatch(request("inspect_object", {{"object_id", id}}));
    assert(inspected.at("object").at("flags").at("clipping_object") == true);

    mock_objects.front()->deleted = true;
    expect_bridge_error("STALE_OBJECT", [id] { dispatch(request("inspect_object", {{"object_id", id}})); });
    mock_objects.front()->deleted = false;
    ++mock_objects.front()->native_id;
    expect_bridge_error("STALE_OBJECT", [id] { dispatch(request("delete_object", {{"object_id", id}}, true)); });
    assert(register_object(mock_objects.front().get(), &mock_section) != id);

    const auto scenes = dispatch(request("list_scenes"));
    assert(scenes.at("scenes").size() == 2);
    assert(scenes.at("scenes").at(0).at("current") == true);
    const auto same = dispatch(request("select_scene", {{"target_scene_id", 2}}, true));
    assert(same.at("context").at("generation") == initial);
    const auto selected = dispatch(request("select_scene", {{"name", "Sub"}}, true));
    assert(selected.at("context").at("scene_id") == 7);
    assert(generation.load() == initial + 1);
    const auto created = dispatch(request("create_scene", {{"name", "Title"}, {"width", 1280}, {"background", "12345600"}}, true));
    assert(created.at("scenes").size() == 3);
    assert(mock_info.width == 1280 && mock_info.height == 1080 && mock_info.rate == 30);
    assert(created.at("context").at("background") == "12345600");
    assert(generation.load() == initial + 2);
    mock_scenes.emplace_back(20, L"Title");
    expect_bridge_error("INVALID_ARGUMENT", [] { dispatch(request("select_scene", {{"name", "Title"}}, true)); });
    expect_bridge_error("MISSING_CONTEXT", [] { dispatch(request("create_scene", {{"name", "Bad"}})); });
    auto stale = request("create_project", json::object(), true);
    stale["context"]["generation"] = initial;
    const int calls = workspace_calls;
    expect_bridge_error("STALE_CONTEXT", [&stale] { dispatch(stale); });
    assert(workspace_calls == calls);

    workspace_reject = true;
    expect_bridge_error("HOST_REJECTED", [] { dispatch(request("select_scene", {{"target_scene_id", 2}}, true)); });
    assert(generation.load() == initial + 2);
    workspace_reject = false;
    dispatch(request("create_project", {{"show_confirm", true}}, true));
    assert(workspace_confirm && mock_scenes.size() == 1);
    assert(generation.load() == initial + 3);
    dispatch(request("open_project", {{"file", "C:\\Projects\\編集.aup2"}}, true));
    assert(opened_project == L"C:\\Projects\\編集.aup2");
    assert(!workspace_confirm);
    assert(generation.load() == initial + 4);
    dispatch(request("save_project", {{"file", "C:\\Projects\\snapshot.aup2"}}, true));
    assert(saved_project == L"C:\\Projects\\snapshot.aup2");
    assert(generation.load() == initial + 4);
    expect_bridge_error("INVALID_ARGUMENT", [] { dispatch(request("save_project", {{"file", "relative.aup2"}}, true)); });

    mock_handle.enum_module_info = mock_output_modules;
    assert(dispatch(request("list_output_plugins")).size() == 1);
    assert(!dispatch(request("get_output_status")).contains("job"));
    const auto output = dispatch(request("output_file", {{"file", "C:\\Output\\movie.mp4"},
        {"output_plugin", "Mock encoder"}, {"save_project_file", "C:\\Projects\\before-output.aup2"}}, true));
    const auto job_id = output.at("job").at("id");
    assert(output.at("job").at("state") == "running");
    assert(saved_project == L"C:\\Projects\\before-output.aup2");
    expect_bridge_error("EDIT_UNAVAILABLE", [] { dispatch(request("output_file", {{"file", "C:\\Output\\another.mp4"}, {"output_plugin", "Mock encoder"}}, true)); });
    workspace_edit_state = EDIT_HANDLE::EDIT_STATE_EDIT;
    on_edit_state_change(nullptr);
    const auto ended = dispatch(request("get_output_status", {{"job_id", job_id}}));
    assert(ended.at("job").at("state") == "ended" && ended.at("job").at("outcome") == "unknown");
    workspace_reject = true;
    expect_bridge_error("HOST_REJECTED", [] { dispatch(request("output_file", {{"file", "C:\\Output\\fail.mp4"}, {"output_plugin", "Mock encoder"}}, true)); });
    assert(dispatch(request("get_output_status")).at("job").at("state") == "rejected");
    workspace_reject = false;
    expect_bridge_error("NOT_FOUND", [] { dispatch(request("get_output_status", {{"job_id", 999}})); });

    const int before_unknown = workspace_calls;
    expect_bridge_error("NOT_FOUND", [] { dispatch(request("output_file", {{"file", "C:\\Output\\fail.mp4"}, {"output_plugin", "Unknown"}}, true)); });
    assert(workspace_calls == before_unknown);
    workspace_reject = true;
    expect_bridge_error("HOST_REJECTED", [] { dispatch(request("output_file", {{"file", "C:\\Output\\fail.mp4"}, {"output_plugin", "Mock encoder"},
        {"save_project_file", "C:\\Projects\\failed-save.aup2"}}, true)); });
    assert(workspace_calls == before_unknown + 1);
    workspace_reject = false;

    mock_handle.output_file = [](LPCWSTR file, LPCWSTR plugin, void* param, void (*config)(void*, PROJECT_FILE*)) {
        const auto started = mock_output_file(file, plugin, param, config);
        workspace_edit_state = EDIT_HANDLE::EDIT_STATE_EDIT;
        on_edit_state_change(nullptr);
        return started;
    };
    for (int i = 0; i < 21; ++i) {
        const auto quick = dispatch(request("output_file", {{"file", "C:\\Output\\quick.mp4"}, {"output_plugin", "Mock encoder"}}, true));
        assert(quick.at("job").at("state") == "ended");
        assert(quick.at("job").at("outcome") == "unknown");
    }
    assert(output_jobs.size() == 20);
    expect_bridge_error("NOT_FOUND", [&job_id] { dispatch(request("get_output_status", {{"job_id", job_id}})); });

    running.store(true);
    const auto queued_request = request("save_project", {{"file", "C:\\Projects\\main-thread.aup2"}}, true);
    std::atomic<bool> completed{false};
    std::thread caller([&] {
        dispatch(queued_request);
        completed.store(true);
    });
    const auto limit = GetTickCount64() + 3000;
    while (!completed.load() && GetTickCount64() < limit) {
        MSG message{};
        while (PeekMessageW(&message, workspace_window, 0, 0, PM_REMOVE)) DispatchMessageW(&message);
        Sleep(1);
    }
    assert(completed.load());
    caller.join();
    assert(saved_project == L"C:\\Projects\\main-thread.aup2");
    std::thread shutting_down([&] {
        expect_bridge_error("HOST_UNAVAILABLE", [&] { dispatch(queued_request); });
    });
    Sleep(20);
    running.store(false);
    shutting_down.join();
    stop_workspace_window();
}
