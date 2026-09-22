import 'package:flutter_test/flutter_test.dart';
import 'package:dynamic_link_flutter/src/web_view_back_navigation.dart';

void main() {
  group('isUserInitiatedMainFrameNavigation', () {
    test('accepts a main-frame navigation with a native gesture', () {
      expect(
        isUserInitiatedMainFrameNavigation(isMainFrame: true, hasGesture: true),
        isTrue,
      );
    });

    test('ignores automatic redirects and sub-frame gestures', () {
      expect(
        isUserInitiatedMainFrameNavigation(
          isMainFrame: true,
          hasGesture: false,
        ),
        isFalse,
      );
      expect(
        isUserInitiatedMainFrameNavigation(
          isMainFrame: false,
          hasGesture: true,
        ),
        isFalse,
      );
    });
  });

  group('buildHistoryBoundaryScript', () {
    test('locks after slider, component and scroll interactions', () {
      final script = buildHistoryBoundaryScript(boundaryLocked: false);

      expect(script, contains('[role="slider"]'));
      expect(
        script,
        contains("'pointerdown',\n    registerComponentInteraction"),
      );
      expect(
        script,
        contains("addEventListener('pointerup', registerComponentInteraction"),
      );
      expect(script, contains("addEventListener('touchstart'"));
      expect(script, contains('accumulatedScroll >= 48'));
      expect(script, contains('markBoundary();'));
    });
  });

  group('resolveWebViewBackAction', () {
    test('uses the WebView history when protection is disabled', () {
      final action = resolveWebViewBackAction(
        protectFromHistoryTrap: false,
        boundaryLocked: false,
        isCurrentEntryBoundary: null,
        isCurrentUrlBoundary: false,
        canGoBack: true,
      );

      expect(action, WebViewBackAction.navigateWebViewBack);
    });

    test('bubbles Back when protection is disabled and history is empty', () {
      final action = resolveWebViewBackAction(
        protectFromHistoryTrap: false,
        boundaryLocked: false,
        isCurrentEntryBoundary: null,
        isCurrentUrlBoundary: false,
        canGoBack: false,
      );

      expect(action, WebViewBackAction.bubble);
    });

    test('closes WebView before the first confirmed interaction', () {
      final action = resolveWebViewBackAction(
        protectFromHistoryTrap: true,
        boundaryLocked: false,
        isCurrentEntryBoundary: null,
        isCurrentUrlBoundary: false,
        canGoBack: true,
      );

      expect(action, WebViewBackAction.closeWebView);
    });

    test('navigates back inside WebView after the boundary', () {
      final action = resolveWebViewBackAction(
        protectFromHistoryTrap: true,
        boundaryLocked: true,
        isCurrentEntryBoundary: false,
        isCurrentUrlBoundary: false,
        canGoBack: true,
      );

      expect(action, WebViewBackAction.navigateWebViewBack);
    });

    test('closes WebView on the boundary', () {
      final action = resolveWebViewBackAction(
        protectFromHistoryTrap: true,
        boundaryLocked: true,
        isCurrentEntryBoundary: true,
        isCurrentUrlBoundary: false,
        canGoBack: true,
      );

      expect(action, WebViewBackAction.closeWebView);
    });

    test('navigates back when JavaScript check fails outside boundary URL', () {
      final action = resolveWebViewBackAction(
        protectFromHistoryTrap: true,
        boundaryLocked: true,
        isCurrentEntryBoundary: null,
        isCurrentUrlBoundary: false,
        canGoBack: true,
      );

      expect(action, WebViewBackAction.navigateWebViewBack);
    });

    test('closes WebView on the native boundary URL', () {
      final action = resolveWebViewBackAction(
        protectFromHistoryTrap: true,
        boundaryLocked: true,
        isCurrentEntryBoundary: null,
        isCurrentUrlBoundary: true,
        canGoBack: true,
      );

      expect(action, WebViewBackAction.closeWebView);
    });

    test('closes WebView when protected history is empty', () {
      final action = resolveWebViewBackAction(
        protectFromHistoryTrap: true,
        boundaryLocked: true,
        isCurrentEntryBoundary: false,
        isCurrentUrlBoundary: false,
        canGoBack: false,
      );

      expect(action, WebViewBackAction.closeWebView);
    });
  });
}
