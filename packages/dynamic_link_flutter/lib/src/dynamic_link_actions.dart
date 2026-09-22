import 'package:url_launcher/url_launcher.dart';

/// Platform actions used by dynamic link navigation.
abstract interface class DynamicLinkActions {
  Future<bool> openInAppBrowser(Uri uri);

  Future<bool> openExternal(Uri uri);
}

/// Default actions backed by `url_launcher`.
final class const PlatformDynamicLinkActions() implements DynamicLinkActions {
  @override
  Future<bool> openInAppBrowser(Uri uri) {
    return launchUrl(uri, mode: .inAppBrowserView);
  }

  @override
  Future<bool> openExternal(Uri uri) {
    return launchUrl(uri, mode: .externalApplication);
  }
}
