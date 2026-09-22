sealed class PushLinkAction {
  const PushLinkAction();

  factory PushLinkAction.fromJson(Map<String, dynamic> json) {
    switch (json) {
      case {'type': 'route', 'url': final String url}:
        final uri = Uri.tryParse(url);
        if (uri == null ||
            !url.startsWith('/') ||
            url.startsWith('//') ||
            uri.hasScheme ||
            uri.hasAuthority ||
            url.contains('\\')) {
          throw const FormatException('Route must be an absolute in-app path');
        }
        return AppRouteAction(uri);
      case {'type': 'partner', 'id': final String id} when id.trim().isNotEmpty:
        return PartnerPushAction(id);
      default:
        throw const FormatException('Expected route/url or partner/id');
    }
  }
}

final class AppRouteAction extends PushLinkAction {
  const AppRouteAction(this.uri);
  final Uri uri;
}

final class PartnerPushAction extends PushLinkAction {
  const PartnerPushAction(this.linkId);
  final String linkId;
}
