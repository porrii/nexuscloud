cmake_minimum_required(VERSION 3.15)
set(PROJECT_NAME "nexus_startup_task")
project(${PROJECT_NAME} LANGUAGES CXX)

set(PLUGIN_NAME "nexus_startup_task_plugin")

add_library(${PLUGIN_NAME} SHARED
  "nexus_startup_task_plugin.cpp"
)
apply_standard_settings(${PLUGIN_NAME})
# C++/WinRT (usado para IAsyncOperation::get()) necesita el <coroutine> de
# C++20; con C++17 cae en <experimental/coroutine>, que el MSVC actual
# rechaza con un error duro. Se fuerza C++20 solo en este plugin.
set_target_properties(${PLUGIN_NAME} PROPERTIES
  CXX_VISIBILITY_PRESET hidden
  CXX_STANDARD 20
  CXX_STANDARD_REQUIRED ON)
target_compile_options(${PLUGIN_NAME} PRIVATE "/std:c++20")
target_compile_definitions(${PLUGIN_NAME} PRIVATE FLUTTER_PLUGIN_IMPL)
target_compile_definitions(${PLUGIN_NAME} PRIVATE _SILENCE_EXPERIMENTAL_COROUTINE_DEPRECATION_WARNINGS)
target_include_directories(${PLUGIN_NAME} INTERFACE
  "${CMAKE_CURRENT_SOURCE_DIR}/include")
# `windowsapp` aporta el runtime de C++/WinRT (Windows.ApplicationModel.*).
target_link_libraries(${PLUGIN_NAME} PRIVATE flutter flutter_wrapper_plugin windowsapp)

set(nexus_startup_task_bundled_libraries
  ""
  PARENT_SCOPE
)
