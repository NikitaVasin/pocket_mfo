// swift-tools-version: 5.9
import PackageDescription
let package = Package(name: "dynamic_link_flutter", platforms: [.iOS("15.0")],
    products: [.library(name: "dynamic-link-flutter", targets: ["dynamic_link_flutter"])],
    targets: [.target(name: "dynamic_link_flutter", dependencies: [])])
