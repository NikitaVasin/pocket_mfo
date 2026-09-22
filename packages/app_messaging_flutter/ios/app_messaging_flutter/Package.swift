// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "app_messaging_flutter",
    platforms: [.iOS("15.0")],
    products: [.library(name: "app-messaging-flutter", targets: ["app_messaging_flutter"])],
    dependencies: [.package(url: "https://github.com/appmetrica/push-sdk-ios", from: "3.4.0")],
    targets: [.target(
        name: "app_messaging_flutter",
        dependencies: [.product(name: "AppMetricaPush", package: "push-sdk-ios")],
        publicHeadersPath: "include"
    )]
)
