import 'package:webview_flutter/webview_flutter.dart';
import 'package:webview_flutter_android/webview_flutter_android.dart';
// ignore: implementation_imports
import 'package:webview_flutter_android/src/android_webkit.g.dart'
    as android_webview;

import 'package:dynamic_link_flutter/src/web_view_back_navigation.dart';

/// Adds Android WebResourceRequest.hasGesture without changing the public
/// navigation behavior of webview_flutter.
class GestureAwareAndroidNavigationDelegate extends AndroidNavigationDelegate {
  GestureAwareAndroidNavigationDelegate({required this.onUserNavigation})
    : super(const PlatformNavigationDelegateCreationParams());

  final void Function(String url) onUserNavigation;

  android_webview.WebViewClient get _baseWebViewClient =>
      super.androidWebViewClient;

  late final android_webview.WebViewClient _gestureAwareWebViewClient =
      android_webview.WebViewClient(
        onPageStarted: (_, webView, url) {
          final client = _baseWebViewClient;
          client.onPageStarted?.call(client, webView, url);
        },
        onPageFinished: (_, webView, url) {
          final client = _baseWebViewClient;
          client.onPageFinished?.call(client, webView, url);
        },
        onReceivedHttpError: (_, webView, request, response) {
          final client = _baseWebViewClient;
          client.onReceivedHttpError?.call(client, webView, request, response);
        },
        onReceivedRequestError: (_, webView, request, error) {
          final client = _baseWebViewClient;
          client.onReceivedRequestError?.call(client, webView, request, error);
        },
        onReceivedRequestErrorCompat: (_, webView, request, error) {
          final client = _baseWebViewClient;
          client.onReceivedRequestErrorCompat?.call(
            client,
            webView,
            request,
            error,
          );
        },
        requestLoading: (_, webView, request) {
          if (isUserInitiatedMainFrameNavigation(
            isMainFrame: request.isForMainFrame,
            hasGesture: request.hasGesture,
          )) {
            onUserNavigation(request.url);
          }

          final client = _baseWebViewClient;
          client.requestLoading?.call(client, webView, request);
        },
        urlLoading: (_, webView, url) {
          final client = _baseWebViewClient;
          client.urlLoading?.call(client, webView, url);
        },
        doUpdateVisitedHistory: (_, webView, url, isReload) {
          final client = _baseWebViewClient;
          client.doUpdateVisitedHistory?.call(client, webView, url, isReload);
        },
        onReceivedHttpAuthRequest: (_, webView, handler, host, realm) {
          final client = _baseWebViewClient;
          client.onReceivedHttpAuthRequest?.call(
            client,
            webView,
            handler,
            host,
            realm,
          );
        },
        onFormResubmission: (_, webView, dontResend, resend) {
          final client = _baseWebViewClient;
          client.onFormResubmission?.call(client, webView, dontResend, resend);
        },
        onReceivedClientCertRequest: (_, webView, request) {
          final client = _baseWebViewClient;
          client.onReceivedClientCertRequest?.call(client, webView, request);
        },
        onReceivedSslError: (_, webView, handler, error) {
          final client = _baseWebViewClient;
          client.onReceivedSslError?.call(client, webView, handler, error);
        },
      );

  @override
  android_webview.WebViewClient get androidWebViewClient =>
      _gestureAwareWebViewClient;

  @override
  Future<void> setOnNavigationRequest(
    NavigationRequestCallback onNavigationRequest,
  ) async {
    await super.setOnNavigationRequest(onNavigationRequest);
    await _gestureAwareWebViewClient
        .setSynchronousReturnValueForShouldOverrideUrlLoading(true);
  }
}
