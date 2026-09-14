#include "include/nexus_startup_task/nexus_startup_task_plugin.h"

#include <windows.h>

#include <flutter/method_channel.h>
#include <flutter/plugin_registrar_windows.h>
#include <flutter/standard_method_codec.h>

#include <winrt/Windows.Foundation.h>
#include <winrt/Windows.ApplicationModel.h>

#include <memory>
#include <string>
#include <thread>

// El TaskId debe coincidir con msix_config: startup_task: task_id del
// pubspec.yaml de la app (que a su vez va al AppxManifest.xml).
namespace {

constexpr wchar_t kTaskId[] = L"NexusCloudAutostart";

using flutter::EncodableValue;
using flutter::MethodCall;
using flutter::MethodResult;

const char* StateToString(winrt::Windows::ApplicationModel::StartupTaskState s) {
  using S = winrt::Windows::ApplicationModel::StartupTaskState;
  switch (s) {
    case S::Disabled:
      return "disabled";
    case S::DisabledByUser:
      return "disabledByUser";
    case S::DisabledByPolicy:
      return "disabledByPolicy";
    case S::Enabled:
      return "enabled";
    case S::EnabledByPolicy:
      return "enabledByPolicy";
    default:
      return "unavailable";
  }
}

// RunOnBackground ejecuta la operación WinRT en un hilo propio con
// apartamento multihilo, para que un `.get()` bloqueante (o el diálogo de
// RequestEnableAsync) nunca detenga el bucle de mensajes del hilo de UI.
// La respuesta del MethodChannel desde un hilo de fondo es segura en el
// embedder de Windows moderno (Flutter 3.x+): el marshalling al hilo de
// plataforma lo hace el propio embedder.
void RunOnBackground(std::function<std::string()> work,
                     std::shared_ptr<MethodResult<EncodableValue>> result) {
  std::thread([work = std::move(work), result]() mutable {
    std::string value;
    try {
      winrt::init_apartment(winrt::apartment_type::multi_threaded);
      value = work();
    } catch (const winrt::hresult_error&) {
      value = "unavailable";  // p.ej. APPMODEL_ERROR_NO_PACKAGE (no empaquetado)
    } catch (...) {
      value = "unavailable";
    }
    result->Success(EncodableValue(value));
  }).detach();
}

class NexusStartupTaskPlugin : public flutter::Plugin {
 public:
  static void RegisterWithRegistrar(
      flutter::PluginRegistrarWindows* registrar) {
    auto channel =
        std::make_unique<flutter::MethodChannel<EncodableValue>>(
            registrar->messenger(), "nexuscloud/startup_task",
            &flutter::StandardMethodCodec::GetInstance());

    auto plugin = std::make_unique<NexusStartupTaskPlugin>();
    channel->SetMethodCallHandler(
        [plugin_pointer = plugin.get()](const auto& call, auto result) {
          plugin_pointer->HandleMethodCall(
              call, std::shared_ptr<MethodResult<EncodableValue>>(
                        std::move(result)));
        });
    registrar->AddPlugin(std::move(plugin));
  }

  NexusStartupTaskPlugin() = default;
  virtual ~NexusStartupTaskPlugin() = default;

 private:
  void HandleMethodCall(
      const MethodCall<EncodableValue>& call,
      std::shared_ptr<MethodResult<EncodableValue>> result) {
    const std::string& method = call.method_name();

    if (method == "getState") {
      RunOnBackground(
          []() -> std::string {
            auto task = winrt::Windows::ApplicationModel::StartupTask::GetAsync(
                            kTaskId)
                            .get();
            return StateToString(task.State());
          },
          result);
      return;
    }

    if (method == "requestEnable") {
      RunOnBackground(
          []() -> std::string {
            auto task = winrt::Windows::ApplicationModel::StartupTask::GetAsync(
                            kTaskId)
                            .get();
            auto state = task.RequestEnableAsync().get();
            return StateToString(state);
          },
          result);
      return;
    }

    if (method == "disable") {
      RunOnBackground(
          []() -> std::string {
            auto task = winrt::Windows::ApplicationModel::StartupTask::GetAsync(
                            kTaskId)
                            .get();
            task.Disable();
            return StateToString(task.State());
          },
          result);
      return;
    }

    result->NotImplemented();
  }
};

}  // namespace

void NexusStartupTaskPluginRegisterWithRegistrar(
    FlutterDesktopPluginRegistrarRef registrar) {
  NexusStartupTaskPlugin::RegisterWithRegistrar(
      flutter::PluginRegistrarManager::GetInstance()
          ->GetRegistrar<flutter::PluginRegistrarWindows>(registrar));
}
