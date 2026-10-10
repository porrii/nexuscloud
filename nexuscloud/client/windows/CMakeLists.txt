# Project-level configuration.
cmake_minimum_required(VERSION 3.14)
project(nexuscloud_client LANGUAGES CXX)

# The name of the executable created for the application. Change this to change
# the on-disk name of your application.
set(BINARY_NAME "nexuscloud_client")

# Explicitly opt in to modern CMake behaviors to avoid warnings with recent
# versions of CMake.
cmake_policy(VERSION 3.14...3.25)

# Define build configuration option.
get_property(IS_MULTICONFIG GLOBAL PROPERTY GENERATOR_IS_MULTI_CONFIG)
if(IS_MULTICONFIG)
  set(CMAKE_CONFIGURATION_TYPES "Debug;Profile;Release"
    CACHE STRING "" FORCE)
else()
  if(NOT CMAKE_BUILD_TYPE AND NOT CMAKE_CONFIGURATION_TYPES)
    set(CMAKE_BUILD_TYPE "Debug" CACHE
      STRING "Flutter build mode" FORCE)
    set_property(CACHE CMAKE_BUILD_TYPE PROPERTY STRINGS
      "Debug" "Profile" "Release")
  endif()
endif()
# Define settings for the Profile build mode.
set(CMAKE_EXE_LINKER_FLAGS_PROFILE "${CMAKE_EXE_LINKER_FLAGS_RELEASE}")
set(CMAKE_SHARED_LINKER_FLAGS_PROFILE "${CMAKE_SHARED_LINKER_FLAGS_RELEASE}")
set(CMAKE_C_FLAGS_PROFILE "${CMAKE_C_FLAGS_RELEASE}")
set(CMAKE_CXX_FLAGS_PROFILE "${CMAKE_CXX_FLAGS_RELEASE}")

# Use Unicode for all projects.
add_definitions(-DUNICODE -D_UNICODE)

# Compilation settings that should be applied to most targets.
#
# Be cautious about adding new options here, as plugins use this function by
# default. In most cases, you should add new options to specific targets instead
# of modifying this function.
function(APPLY_STANDARD_SETTINGS TARGET)
  target_compile_features(${TARGET} PUBLIC cxx_std_17)
  target_compile_options(${TARGET} PRIVATE /W4 /WX /wd"4100")
  target_compile_options(${TARGET} PRIVATE /EHsc)
  target_compile_definitions(${TARGET} PRIVATE "_HAS_EXCEPTIONS=0")
  target_compile_definitions(${TARGET} PRIVATE "$<$<CONFIG:Debug>:_DEBUG>")
endfunction()

# Flutter library and tool build rules.
set(FLUTTER_MANAGED_DIR "${CMAKE_CURRENT_SOURCE_DIR}/flutter")
add_subdirectory(${FLUTTER_MANAGED_DIR})

# Application build; see runner/CMakeLists.txt.
add_subdirectory("runner")


# Generated plugin build rules, which manage building the plugins and adding
# them to the application.
include(flutter/generated_plugins.cmake)


# === Installation ===
# Support files are copied into place next to the executable, so that it can
# run in place. This is done instead of making a separate bundle (as on Linux)
# so that building and running from within Visual Studio will work.
set(BUILD_BUNDLE_DIR "$<TARGET_FILE_DIR:${BINARY_NAME}>")
# Make the "install" step default, as it's required to run.
set(CMAKE_VS_INCLUDE_INSTALL_TO_DEFAULT_BUILD 1)
if(CMAKE_INSTALL_PREFIX_INITIALIZED_TO_DEFAULT)
  set(CMAKE_INSTALL_PREFIX "${BUILD_BUNDLE_DIR}" CACHE PATH "..." FORCE)
endif()

set(INSTALL_BUNDLE_DATA_DIR "${CMAKE_INSTALL_PREFIX}/data")
set(INSTALL_BUNDLE_LIB_DIR "${CMAKE_INSTALL_PREFIX}")

install(TARGETS ${BINARY_NAME} RUNTIME DESTINATION "${CMAKE_INSTALL_PREFIX}"
  COMPONENT Runtime)

install(FILES "${FLUTTER_ICU_DATA_FILE}" DESTINATION "${INSTALL_BUNDLE_DATA_DIR}"
  COMPONENT Runtime)

install(FILES "${FLUTTER_LIBRARY}" DESTINATION "${INSTALL_BUNDLE_LIB_DIR}"
  COMPONENT Runtime)

if(PLUGIN_BUNDLED_LIBRARIES)
  install(FILES "${PLUGIN_BUNDLED_LIBRARIES}"
    DESTINATION "${INSTALL_BUNDLE_LIB_DIR}"
    COMPONENT Runtime)
endif()

# Copy the native assets provided by the build.dart from all packages.
set(NATIVE_ASSETS_DIR "${PROJECT_BUILD_DIR}native_assets/windows/")
install(DIRECTORY "${NATIVE_ASSETS_DIR}"
   DESTINATION "${INSTALL_BUNDLE_LIB_DIR}"
   COMPONENT Runtime)

# Fully re-copy the assets directory on each build to avoid having stale files
# from a previous install.
set(FLUTTER_ASSET_DIR_NAME "flutter_assets")
install(CODE "
  file(REMOVE_RECURSE \"${INSTALL_BUNDLE_DATA_DIR}/${FLUTTER_ASSET_DIR_NAME}\")
  " COMPONENT Runtime)
install(DIRECTORY "${PROJECT_BUILD_DIR}/${FLUTTER_ASSET_DIR_NAME}"
  DESTINATION "${INSTALL_BUNDLE_DATA_DIR}" COMPONENT Runtime)

# Install the AOT library on non-Debug builds only.
install(FILES "${AOT_LIBRARY}" DESTINATION "${INSTALL_BUNDLE_DATA_DIR}"
  CONFIGURATIONS Profile;Release
  COMPONENT Runtime)

# === libmpv reciente (ADR-046) ===
# media_kit_libs_windows_video trae fija una libmpv de 2023-09 (FFmpeg sin
# parches posteriores), y la vista previa la usa para decodificar vídeos que
# pueden haber subido terceros. Se sustituye por una build reciente de
# shinchiro/mpv-winbuild-cmake, verificada con el SHA-256 que GitHub publica
# para ese asset. Para actualizarla: cambia nombre, URL y hash aquí, y el
# hash de la DLL en deploy/scripts/package-client-windows.ps1, y repite la
# batería de formatos descrita en el ADR.
set(NEXUS_LIBMPV_ARCHIVE_NAME "mpv-dev-x86_64-20261010-git-b2c255c13e.7z")
set(NEXUS_LIBMPV_URL "https://github.com/shinchiro/mpv-winbuild-cmake/releases/download/20261010/${NEXUS_LIBMPV_ARCHIVE_NAME}")
set(NEXUS_LIBMPV_SHA256 "394490ed6354c2294b87c63dfa3663b23524b5551aace9cbe35c4085682b14fa")
set(NEXUS_LIBMPV_ARCHIVE "${CMAKE_BINARY_DIR}/${NEXUS_LIBMPV_ARCHIVE_NAME}")
set(NEXUS_LIBMPV_DIR "${CMAKE_BINARY_DIR}/nexus-libmpv-20261010")
if(NOT EXISTS "${NEXUS_LIBMPV_DIR}/libmpv-2.dll")
  file(DOWNLOAD "${NEXUS_LIBMPV_URL}" "${NEXUS_LIBMPV_ARCHIVE}"
    EXPECTED_HASH SHA256=${NEXUS_LIBMPV_SHA256})
  file(MAKE_DIRECTORY "${NEXUS_LIBMPV_DIR}")
  execute_process(
    COMMAND "${CMAKE_COMMAND}" -E tar xf "${NEXUS_LIBMPV_ARCHIVE}"
    WORKING_DIRECTORY "${NEXUS_LIBMPV_DIR}"
    RESULT_VARIABLE NEXUS_LIBMPV_EXTRACT_RESULT)
  if(NOT NEXUS_LIBMPV_EXTRACT_RESULT EQUAL 0 OR NOT EXISTS "${NEXUS_LIBMPV_DIR}/libmpv-2.dll")
    message(FATAL_ERROR "No se pudo extraer libmpv-2.dll de ${NEXUS_LIBMPV_ARCHIVE_NAME}")
  endif()
endif()
# Va DESPUÉS de PLUGIN_BUNDLED_LIBRARIES: sobrescribe la DLL del paquete.
install(FILES "${NEXUS_LIBMPV_DIR}/libmpv-2.dll"
  DESTINATION "${INSTALL_BUNDLE_LIB_DIR}"
  COMPONENT Runtime)
