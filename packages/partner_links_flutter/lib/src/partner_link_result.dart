import 'package:dynamic_link/dynamic_link.dart';

/// Server-issued link. Resolving it does not open or track the offer.
final class PartnerLinkResult {
  const PartnerLinkResult({
    required this.clickId,
    required this.expiresAt,
    required this.link,
  });

  final String clickId;
  final DateTime expiresAt;
  final DynamicLink link;

  factory PartnerLinkResult.fromJson(Map<String, dynamic> json) {
    String requiredString(Map<String, dynamic> source, String key) {
      final value = source[key];
      if (value is! String || value.isEmpty) {
        throw FormatException('Missing or invalid $key');
      }
      return value;
    }

    String? optionalString(Map<String, dynamic> source, String key) {
      final value = source[key];
      if (value == null) return null;
      if (value is! String) throw FormatException('Invalid $key');
      return value;
    }

    final raw = json['link'];
    if (raw is! Map<String, dynamic>) {
      throw const FormatException('Missing link');
    }
    bool flag(String key) {
      final value = raw[key];
      if (value is! bool) throw FormatException('Invalid $key');
      return value;
    }

    final uri = Uri.tryParse(requiredString(raw, 'url'));
    if (uri == null ||
        !['http', 'https'].contains(uri.scheme) ||
        uri.host.isEmpty ||
        uri.userInfo.isNotEmpty) {
      throw const FormatException('Invalid link URL');
    }
    final mode = switch (raw['mode']) {
      'appView' => DynamicLinkMode.appView,
      'view' => DynamicLinkMode.view,
      'browser' => DynamicLinkMode.browser,
      _ => throw const FormatException('Invalid link mode'),
    };
    final expiresAt = DateTime.tryParse(requiredString(json, 'expiresAt'));
    if (expiresAt == null) throw const FormatException('Invalid expiresAt');
    DynamicLinkWarningDialog? warning;
    final rawWarning = raw['warningDialog'];
    if (rawWarning != null) {
      if (rawWarning is! Map<String, dynamic>) {
        throw const FormatException('Invalid warningDialog');
      }
      warning = DynamicLinkWarningDialog(
        title: requiredString(rawWarning, 'title'),
        content: requiredString(rawWarning, 'content'),
      );
    }
    return PartnerLinkResult(
      clickId: requiredString(json, 'clickId'),
      expiresAt: expiresAt.toUtc(),
      link: DynamicLink(
        url: uri,
        mode: mode,
        saveCooke: flag('saveCooke'),
        changeClient: flag('changeClient'),
        showLoader: flag('showLoader'),
        openUrlsInBrowser: flag('openUrlsInBrowser'),
        skipWarningDialog: flag('skipWarningDialog'),
        warningDialog: warning,
        title: optionalString(raw, 'title'),
        trackName: optionalString(raw, 'trackName'),
        category: optionalString(raw, 'category'),
      ),
    );
  }
}
