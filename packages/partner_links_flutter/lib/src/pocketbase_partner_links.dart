import 'package:dynamic_link_flutter/dynamic_link_flutter.dart';
import 'package:flutter/widgets.dart';
import 'package:pocketbase/pocketbase.dart';

import 'partner_link_result.dart';
import 'variant_exposure.dart';

/// Uses this PocketBase instance and its current authentication.
/// The server uses PocketBase user.id as the AppMetrica profile ID.
/// Does not cache tokens, report SDK events, or follow redirects.
extension PocketBasePartnerLinks on PocketBase {
  /// Issues a new token for each call. API errors remain [ClientException]s.
  Future<PartnerLinkResult> resolvePartnerLink({
    required String linkId,
    List<VariantExposure> exposures = const [],
  }) async {
    if (linkId.isEmpty) {
      throw ArgumentError('linkId must not be empty');
    }
    final result = await send<Map<String, dynamic>>(
      '/api/partnerlinks/links/${Uri.encodeComponent(linkId)}/resolve',
      method: 'POST',
      body: {
        if (exposures.isNotEmpty)
          'exposureTokens': exposures.map((e) => e.token).toList(),
      },
    );
    return PartnerLinkResult.fromJson(result);
  }

  /// Requests a fresh link, then lets WebView/the browser load its public URL.
  /// Returns false when the context was disposed before the request completed.
  Future<bool> openPartnerLink(
    BuildContext context, {
    required String linkId,
    List<VariantExposure> exposures = const [],
    DynamicLinkActions actions = const PlatformDynamicLinkActions(),
    DynamicLinkEmbeddedViewBuilder? embeddedViewBuilder,
    DynamicLinkWarningDialogBuilder? warningDialogBuilder,
  }) async {
    final result = await resolvePartnerLink(
      linkId: linkId,
      exposures: exposures,
    );
    if (!context.mounted) return false;
    return result.link.open(
      context,
      actions: actions,
      embeddedViewBuilder: embeddedViewBuilder,
      warningDialogBuilder: warningDialogBuilder,
    );
  }
}
