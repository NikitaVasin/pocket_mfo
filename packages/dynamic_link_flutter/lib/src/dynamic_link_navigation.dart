import 'package:dynamic_link/dynamic_link.dart';
import 'package:dynamic_link_flutter/src/dynamic_link_actions.dart';
import 'package:dynamic_link_flutter/src/dynamic_link_web_view_screen.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

/// Builds a custom embedded view for [DynamicLinkMode.appView].
typedef DynamicLinkEmbeddedViewBuilder = Widget Function(
  BuildContext context,
  DynamicLink link,
);

typedef DynamicLinkWarningDialogBuilder = Widget Function(
  BuildContext context,
  DynamicLinkWarningDialog warning,
);

/// Adds Flutter opening behavior to the platform-independent [DynamicLink].
extension DynamicLinkNavigation on DynamicLink {
  /// Opens this link according to its [DynamicLink.mode].
  Future<bool> open(
    BuildContext context, {
    DynamicLinkActions actions = const PlatformDynamicLinkActions(),
    DynamicLinkEmbeddedViewBuilder? embeddedViewBuilder,
    DynamicLinkWarningDialogBuilder? warningDialogBuilder,
  }) {
    return openDynamicLink(
      context,
      this,
      actions: actions,
      embeddedViewBuilder: embeddedViewBuilder,
      warningDialogBuilder: warningDialogBuilder,
    );
  }
}

/// Opens [link] according to its mode.
///
/// Warning button labels come from Flutter's [MaterialLocalizations]. Pass
/// [actions] or [embeddedViewBuilder] to replace platform behavior in tests or
/// in an application-specific integration.
Future<bool> openDynamicLink(
  BuildContext context,
  DynamicLink link, {
  DynamicLinkActions actions = const PlatformDynamicLinkActions(),
  DynamicLinkEmbeddedViewBuilder? embeddedViewBuilder,
  DynamicLinkWarningDialogBuilder? warningDialogBuilder,
}) async {
  final warning = link.warningDialog;
  if (warning != null && !link.skipWarningDialog) {
    final confirmed = await _showWarningDialog(
      context,
      warning,
      warningDialogBuilder,
    );
    if (!confirmed || !context.mounted) return true;
  }

  final scheme = link.url.scheme.toLowerCase();
  final isHttp = scheme == 'http' || scheme == 'https';
  if (!isHttp) return actions.openExternal(link.url);

  return switch (link.mode) {
    .view => actions.openInAppBrowser(link.url),
    .browser => actions.openExternal(link.url),
    .appView when _supportsEmbeddedView => () async {
      final child =
          embeddedViewBuilder?.call(context, link) ??
          DynamicLinkWebViewScreen(link: link);
      await Navigator.of(
        context,
        rootNavigator: true,
      ).push<void>(MaterialPageRoute(builder: (context) => child));
      return true;
    }(),
    .appView => actions.openInAppBrowser(link.url),
  };
}

Future<bool> _showWarningDialog(
  BuildContext context,
  DynamicLinkWarningDialog warning,
  DynamicLinkWarningDialogBuilder? customBuilder,
) async {
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (dialogContext) {
      if (customBuilder != null) return customBuilder(dialogContext, warning);
      final localizations = MaterialLocalizations.of(dialogContext);
      return AlertDialog(
        title: Text(warning.title),
        content: Text(warning.content),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialogContext, false),
            child: Text(localizations.cancelButtonLabel),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(dialogContext, true),
            child: Text(localizations.continueButtonLabel),
          ),
        ],
      );
    },
  );
  return confirmed == true;
}

bool get _supportsEmbeddedView {
  if (kIsWeb) return false;
  return defaultTargetPlatform == .android || defaultTargetPlatform == .iOS;
}
