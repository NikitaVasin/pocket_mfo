import 'dart:async';

import 'package:app_messaging_flutter/app_messaging_flutter.dart';
import 'package:flutter/material.dart';
import 'package:partner_links_flutter/partner_links_flutter.dart';
import 'package:pocketbase/pocketbase.dart';

import 'push_link_action.dart';

typedef PartnerPushLoader = Future<PartnerLinkResult?> Function(String id);

/// Pass [call] to AppMessaging.initialize(onAction: ...). Use the root
/// navigator key also supplied to MaterialApp/your router. [beforeOpen] can
/// await authentication/restoration; no token is requested before it completes.
final class PushLinkResolver {
  PushLinkResolver({
    required this.navigatorKey,
    required this.onNavigate,
    required PartnerPushLoader resolvePartnerLink,
    this.beforeOpen,
    this.embeddedViewBuilder,
  }) : _load = resolvePartnerLink;

  factory PushLinkResolver.pocketBase({
    required GlobalKey<NavigatorState> navigatorKey,
    required PocketBase pocketBase,
    required Future<void> Function(Uri uri) onNavigate,
    Future<void> Function(PushLinkAction action)? beforeOpen,
    DynamicLinkEmbeddedViewBuilder? embeddedViewBuilder,
  }) => PushLinkResolver(
    navigatorKey: navigatorKey,
    onNavigate: onNavigate,
    resolvePartnerLink: (id) => pocketBase.resolvePartnerLink(linkId: id),
    beforeOpen: beforeOpen,
    embeddedViewBuilder: embeddedViewBuilder,
  );

  final GlobalKey<NavigatorState> navigatorKey;
  final Future<void> Function(Uri uri) onNavigate;
  final Future<void> Function(PushLinkAction action)? beforeOpen;
  final DynamicLinkEmbeddedViewBuilder? embeddedViewBuilder;
  final PartnerPushLoader _load;
  bool _disposed = false;

  /// Drops pending navigation when the owning application is disposed.
  void dispose() => _disposed = true;

  Future<void> call(PushAction push) async {
    final action = PushLinkAction.fromJson(push.data);
    await beforeOpen?.call(action);
    if (_disposed) return;
    switch (action) {
      case AppRouteAction(:final uri):
        await onNavigate(uri);
      case PartnerPushAction(:final linkId):
        final navigator = navigatorKey.currentState;
        if (navigator == null || !navigator.mounted) {
          throw StateError('Root navigator is not ready');
        }
        PartnerLinkResult? result;
        try {
          result = await _load(linkId);
        } catch (_) {
          return; // Failed resolution leaves the current screen untouched.
        }
        if (result == null ||
            _disposed ||
            !navigator.mounted ||
            navigatorKey.currentState != navigator)
          return;
        final original = result.link;
        if (original.warningDialog case final warning?
            when !original.skipWarningDialog) {
          final accepted = await showDialog<bool>(
            context: navigator.overlay!.context,
            builder: (context) => AlertDialog(
              title: Text(warning.title),
              content: Text(warning.content),
              actions: [
                TextButton(
                  onPressed: () => Navigator.pop(context, false),
                  child: Text(
                    MaterialLocalizations.of(context).cancelButtonLabel,
                  ),
                ),
                FilledButton(
                  onPressed: () => Navigator.pop(context, true),
                  child: Text(
                    MaterialLocalizations.of(context).continueButtonLabel,
                  ),
                ),
              ],
            ),
          );
          if (accepted != true || _disposed || !navigator.mounted) return;
        }
        final link = DynamicLink(
          url: original.url,
          mode: DynamicLinkMode.appView,
          saveCooke: original.saveCooke,
          changeClient: original.changeClient,
          showLoader: original.showLoader,
          openUrlsInBrowser: original.openUrlsInBrowser,
          skipWarningDialog: original.skipWarningDialog,
          warningDialog: original.warningDialog,
          title: original.title,
          trackName: original.trackName,
        );
        // push preserves every existing page, including nested navigation state.
        unawaited(
          navigator.push<void>(
            MaterialPageRoute(
              settings: RouteSettings(name: '/push/partner/$linkId'),
              builder: (context) =>
                  embeddedViewBuilder?.call(context, link) ??
                  DynamicLinkWebViewScreen(link: link),
            ),
          ),
        );
    }
  }
}
