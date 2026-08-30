cmake_minimum_required(VERSION 3.24)
project(aviutl2_mcp_bridge LANGUAGES CXX)

set(AVIUTL2_SDK_DIR "" CACHE PATH "Directory containing plugin2.h and logger2.h")
if(NOT EXISTS "${AVIUTL2_SDK_DIR}/plugin2.h")
  message(FATAL_ERROR "Set AVIUTL2_SDK_DIR to the official AviUtl2 SDK header directory")
endif()

include(FetchContent)
FetchContent_Declare(
  nlohmann_json
  GIT_REPOSITORY https://github.com/nlohmann/json.git
  GIT_TAG v3.12.0
  GIT_SHALLOW TRUE
)
FetchContent_MakeAvailable(nlohmann_json)

add_library(aviutl2_mcp_bridge SHARED src/bridge.cpp)
target_compile_features(aviutl2_mcp_bridge PRIVATE cxx_std_20)
target_include_directories(aviutl2_mcp_bridge PRIVATE "${AVIUTL2_SDK_DIR}")
target_link_libraries(aviutl2_mcp_bridge PRIVATE nlohmann_json::nlohmann_json ws2_32)
target_compile_definitions(aviutl2_mcp_bridge PRIVATE WIN32_LEAN_AND_MEAN NOMINMAX)

if(MSVC)
  target_compile_options(aviutl2_mcp_bridge PRIVATE /W4 /EHsc /utf-8)
else()
  target_compile_options(aviutl2_mcp_bridge PRIVATE -Wall -Wextra -Wpedantic)
endif()

set_target_properties(aviutl2_mcp_bridge PROPERTIES
  PREFIX ""
  SUFFIX ".aux2"
  OUTPUT_NAME "aviutl2-mcp-bridge"
)
