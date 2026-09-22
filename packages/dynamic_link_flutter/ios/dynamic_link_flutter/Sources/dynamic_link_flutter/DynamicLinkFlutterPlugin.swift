import Flutter
import WebKit
public class DynamicLinkFlutterPlugin: NSObject, FlutterPlugin {
    public static func register(with registrar: FlutterPluginRegistrar) {
        let channel = FlutterMethodChannel(name: "dynamic_link_flutter/web_data", binaryMessenger: registrar.messenger())
        registrar.addMethodCallDelegate(DynamicLinkFlutterPlugin(), channel: channel)
    }
    public func handle(_ call: FlutterMethodCall, result: @escaping FlutterResult) {
        guard call.method == "clear" else { result(FlutterMethodNotImplemented); return }
        WKWebsiteDataStore.default().removeData(ofTypes: WKWebsiteDataStore.allWebsiteDataTypes(), modifiedSince: Date.distantPast) {
            result(nil)
        }
    }
}
