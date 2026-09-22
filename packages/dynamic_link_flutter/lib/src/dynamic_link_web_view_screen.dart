import 'package:dynamic_link/dynamic_link.dart';
import 'package:dynamic_link_flutter/src/dynamic_link_web_view.dart';
import 'package:flutter/material.dart';

/// Ready-to-use screen for [DynamicLinkMode.appView].
final class const DynamicLinkWebViewScreen({
  required final DynamicLink link,
  super.key,
}) extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text(link.title ?? '')),
      body: DynamicLinkWebView(
        uri: link.url,
        saveCooke: link.saveCooke,
        changeClient: link.changeClient,
        showLoader: link.showLoader,
        openUrlsInBrowser: link.openUrlsInBrowser,
      ),
    );
  }
}
