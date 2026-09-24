import 'package:equatable/equatable.dart';

/// Defines how a [DynamicLink] should be opened by a platform adapter.
enum DynamicLinkMode {
  /// Opens the URL in the platform-provided in-app browser.
  view,

  /// Opens the URL in an external application.
  browser,

  /// Opens the URL in a configurable embedded WebView when supported.
  appView,
}

/// Content displayed before opening a potentially sensitive link.
final class const DynamicLinkWarningDialog({
  required final String title,
  required final String content,
}) extends Equatable {
  @override
  List<Object?> get props => [title, content];
}

/// Platform-independent description of a link and its opening behavior.
final class const DynamicLink({
  required final Uri url,
  required final DynamicLinkMode mode,
  required final bool saveCooke,
  required final bool changeClient,
  required final bool showLoader,
  required final bool openUrlsInBrowser,
  required final bool skipWarningDialog,
  final DynamicLinkWarningDialog? warningDialog,
  final String? title,
  final String? trackName,
  final String? category,
}) extends Equatable {
  @override
  List<Object?> get props => [
    url,
    mode,
    saveCooke,
    changeClient,
    showLoader,
    openUrlsInBrowser,
    skipWarningDialog,
    warningDialog,
    title,
    trackName,
    category,
  ];
}
