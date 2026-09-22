import 'package:pocket_mfo_flutter/pocket_mfo_flutter.dart';
import 'package:pocketbase/pocketbase.dart';

class DemoRepository {
  DemoRepository(this.integration);
  final PocketMfo integration;
  PocketBase get pb => integration.pocketBase;
  Future<List<RecordModel>> offers() => pb
      .collection('partner_links')
      .getFullList(filter: 'active = true', sort: 'name');
  Future<List<RecordModel>> orders() => pb
      .collection('conversations')
      .getFullList(filter: 'status != "pending"', sort: '-updated');
  Future<PartnerLinkResult> resolve(String id) =>
      integration.resolvePartnerLink(linkId: id);
}
