import 'package:partner_links_flutter/partner_links_flutter.dart';
import 'package:pocketbase/pocketbase.dart';

/// Reuses one PocketBase instance, including its authentication state.
class DemoRepository {
  DemoRepository(this.pb);
  final PocketBase pb;

  Future<void> login(String email, String password) async {
    await pb.collection('demo_members').authWithPassword(email, password);
  }

  void logout() => pb.authStore.clear();
  Future<List<RecordModel>> offers() => pb
      .collection('partner_links')
      .getFullList(filter: 'active = true', sort: 'name');
  // Ownership is enforced by the server's collection rules.
  Future<List<RecordModel>> orders() => pb
      .collection('conversations')
      .getFullList(filter: 'status != "pending"', sort: '-updated');

  Future<PartnerLinkResult> resolve(String id) =>
      pb.resolvePartnerLink(linkId: id);
}
