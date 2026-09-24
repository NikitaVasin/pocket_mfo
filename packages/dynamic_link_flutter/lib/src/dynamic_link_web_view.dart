import 'package:dynamic_link_flutter/src/gesture_aware_android_navigation_delegate.dart';
import 'package:webview_flutter_android/webview_flutter_android.dart';

import 'dart:async';

import 'package:dynamic_link_flutter/src/web_view_back_navigation.dart';
import 'package:dynamic_link_flutter/src/dynamic_link_web_data.dart';

import 'package:dynamic_link_flutter/src/android_file_picker_stub.dart'
    if (dart.library.io) 'package:dynamic_link_flutter/src/android_file_picker_io.dart';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher_string.dart';
import 'package:webview_flutter/webview_flutter.dart';

/// Embedded WebView used by dynamic links.
final class const DynamicLinkWebView({
  required final Uri uri,
  required final bool saveCooke,
  required final bool changeClient,
  final bool showLoader = true,
  final bool openUrlsInBrowser = false,
  final bool protectFromHistoryTrap = false,
  super.key,
}) extends StatefulWidget {
  @override
  State<DynamicLinkWebView> createState() => _DynamicLinkWebViewState();
}

final class _DynamicLinkWebViewState() extends State<DynamicLinkWebView> {
  final ValueNotifier<double> _progress = .new(0);
  WebViewController? _controller;
  var _initialized = false;
  bool _cleared = false;
  bool _canGoBack = false;
  bool _allowClose = false;
  bool _historyBoundaryLocked = false;
  String? _historyBoundaryUrl;
  String? _lastFinishedUrl;
  final _webGeneration = DynamicLinkWebData.generation;
  bool get _active =>
      mounted && !_cleared && _webGeneration == DynamicLinkWebData.generation;

  @override
  void initState() {
    super.initState();
    DynamicLinkWebData.closeViews.add(_clearView);
  }

  Future<void> _clearView() async {
    if (mounted) setState(() => _cleared = true);
    final controller = _controller;
    if (controller != null) {
      await controller.loadRequest(Uri.parse("about:blank"));
    }
  }

  var _firstPageFinished = false;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_initialized) return;
    _initialized = true;
    _initializeWebView();
  }

  @override
  void dispose() {
    DynamicLinkWebData.closeViews.remove(_clearView);
    _progress.dispose();
    super.dispose();
  }

  Future<void> _initializeWebView() async {
    // Native stores preserve Secure, HttpOnly, SameSite, path and expiry.
    // Never reconstruct cookies from document.cookie.
    await DynamicLinkWebData.removeLegacyCookies();
    if (!_active) {
      return;
    }

    late final WebViewController controller;
    controller = WebViewController()
      ..setJavaScriptMode(.unrestricted)
      ..setNavigationDelegate(
        NavigationDelegate.fromPlatform(
          widget.protectFromHistoryTrap &&
                  WebViewPlatform.instance is AndroidWebViewPlatform
              ? GestureAwareAndroidNavigationDelegate(
                  onUserNavigation: (_) {
                    unawaited(_lockCurrentPageAsBoundary());
                  },
                )
              : NavigationDelegate().platform,
          onPageStarted: (_) {
            unawaited(_installHistoryBoundaryGuard());
          },
          onProgress: (progress) {
            if (_active) {
              _progress.value = (progress / 100).clamp(0, 1);
            }
          },
          onPageFinished: (url) async {
            if (!_active) {
              return;
            }
            _firstPageFinished = true;
            _lastFinishedUrl = url;
            await _installHistoryBoundaryGuard();
            if (!_active) return;
            _progress.value = 1;
            await _updateBackNavigation(controller);
          },
          onNavigationRequest: (request) async {
            await _syncHistoryBoundaryFromPage();
            if (!_active) {
              return request.url == 'about:blank'
                  ? NavigationDecision.navigate
                  : NavigationDecision.prevent;
            }
            if (['tel', 'mailto'].contains(Uri.tryParse(request.url)?.scheme)) {
              launchUrlString(request.url);
              return .prevent;
            }
            if (widget.openUrlsInBrowser && _firstPageFinished) {
              launchUrlString(request.url, mode: .externalApplication);
              return .prevent;
            }
            return .navigate;
          },
        ),
      );
    await configureAndroidFilePicker(controller);
    if (!_active) return;
    if (widget.changeClient) {
      await controller.setUserAgent('Chrome/99.9.9999.9 Mobile Safari/999.9');
      if (!_active) return;
    }
    _controller = controller;
    if (widget.protectFromHistoryTrap) {
      await controller.addJavaScriptChannel(
        webViewHistoryBoundaryChannelName,
        onMessageReceived: (message) {
          if (message.message.startsWith('locked|')) {
            _historyBoundaryLocked = true;
            _historyBoundaryUrl = message.message.substring(7);
          }
        },
      );
    }
    if (!_active) return;
    await controller.loadRequest(widget.uri);
    if (!_active) {
      return;
    }
    setState(() => _controller = controller);
  }

  Future<void> _updateBackNavigation(WebViewController controller) async {
    final canGoBack = await controller.canGoBack();
    if (mounted && !_cleared && canGoBack != _canGoBack) {
      setState(() => _canGoBack = canGoBack);
    }
  }

  Future<void> _installHistoryBoundaryGuard() async {
    if (!widget.protectFromHistoryTrap) {
      return;
    }

    try {
      await _controller?.runJavaScript(
        buildHistoryBoundaryScript(boundaryLocked: _historyBoundaryLocked),
      );
    } catch (_) {
      // A redirect can replace the document while the script is being added.
    }
  }

  Future<bool?> _isCurrentEntryBoundary() async {
    final controller = _controller;
    if (controller == null) {
      return null;
    }

    try {
      final result = await controller.runJavaScriptReturningResult(
        "Boolean(history.state && history.state['$webViewHistoryBoundaryMarker'])",
      );
      return switch (result) {
        bool value => value,
        String value => value == 'true',
        num value => value != 0,
        _ => null,
      };
    } catch (_) {
      return null;
    }
  }

  Future<void> _syncHistoryBoundaryFromPage() async {
    if (!widget.protectFromHistoryTrap || _historyBoundaryUrl != null) {
      return;
    }

    if (await _isCurrentEntryBoundary() == true) {
      await _lockCurrentPageAsBoundary(markHistoryEntry: false);
    }
  }

  Future<void> _lockCurrentPageAsBoundary({
    bool markHistoryEntry = true,
  }) async {
    if (!widget.protectFromHistoryTrap || _historyBoundaryUrl != null) {
      return;
    }

    final controller = _controller;
    final currentUrl = _lastFinishedUrl ?? await controller?.currentUrl();
    if (controller == null || currentUrl == null) {
      return;
    }

    _historyBoundaryLocked = true;
    _historyBoundaryUrl = currentUrl;

    if (!markHistoryEntry) {
      return;
    }

    try {
      await controller.runJavaScript('''
        (() => {
          const marker = '$webViewHistoryBoundaryMarker';
          const state = history.state && typeof history.state === 'object'
            ? {...history.state}
            : {};
          state[marker] = true;
          history.replaceState(state, document.title, location.href);
        })();
      ''');
    } catch (_) {
      // The native URL remains the source of truth if the page is unloading.
    }
  }

  Future<void> _handleBack() async {
    final controller = _controller;
    if (controller == null) {
      setState(() => _allowClose = true);
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) Navigator.of(context).maybePop();
      });
      return;
    }
    await _syncHistoryBoundaryFromPage();
    final action = resolveWebViewBackAction(
      protectFromHistoryTrap: widget.protectFromHistoryTrap,
      boundaryLocked: _historyBoundaryLocked,
      isCurrentEntryBoundary: await _isCurrentEntryBoundary(),
      isCurrentUrlBoundary:
          _historyBoundaryUrl != null &&
          await controller.currentUrl() == _historyBoundaryUrl,
      canGoBack: await controller.canGoBack(),
    );
    if (!mounted) return;
    if (action == WebViewBackAction.navigateWebViewBack) {
      await controller.goBack();
      await _updateBackNavigation(controller);
      return;
    }
    setState(() => _allowClose = true);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) Navigator.of(context).maybePop();
    });
  }

  @override
  Widget build(BuildContext context) {
    if (_cleared) {
      return const Center(child: Text('Сессия веб-страницы очищена'));
    }
    return PopScope<void>(
      canPop: _allowClose || (!widget.protectFromHistoryTrap && !_canGoBack),
      onPopInvokedWithResult: (didPop, result) async {
        if (didPop) return;
        await _handleBack();
      },
      child: Column(
        children: [
          if (widget.showLoader)
            ValueListenableBuilder(
              valueListenable: _progress,
              builder: (context, progress, child) => progress < 1
                  ? LinearProgressIndicator(value: progress)
                  : const SizedBox.shrink(),
            ),
          if (_controller case final controller?)
            Expanded(child: WebViewWidget(controller: controller)),
        ],
      ),
    );
  }
}
