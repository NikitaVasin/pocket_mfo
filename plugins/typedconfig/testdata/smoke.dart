import 'models.dart';

void main() {
  final heading = ScreenItem.fromJson({
    'id': 'heading',
    'type': 'heading',
    'data': {'text': 'Витрина', 'style': {'compact': true}},
  });
  if (heading is! ScreenHeading1 || heading.data.textValue != 'Витрина' || heading.data.styleValue?.compactValue != true) {
    throw StateError('Invalid typed heading');
  }
  final offer = ScreenItem.fromJson({
    'id': 'offer',
    'type': 'offer',
    'data': {'offer': {'title': 'Предложение', 'rating': 4.5}},
  });
  if (offer is! ScreenOffer2 || offer.data.offerValue.ratingValue != 4.5) throw StateError('Invalid projection');
  final group = ScreenItem.fromJson({
    'id': 'group',
    'type': 'group',
    'data': {'offers': [{'id': 'offer-id', 'collectionId': 'offers'}]},
  }) as ScreenGroup3;
  if (group.data.offersValue.single.idValue != 'offer-id') throw StateError('Invalid typed reference');
  try { group.data.offersValue.clear(); throw StateError('Mutable list'); } on UnsupportedError { /* expected */ }
  for (final bad in [
    {'id': 'bad', 'type': 'unknown', 'data': <String, dynamic>{}},
    {'id': 'bad', 'type': 'heading', 'data': {'text': 42}},
    {'id': 'bad', 'type': 'heading', 'data': {'text': 'ok', 'style': {'compact': 'yes'}}},
  ]) {
    try { ScreenItem.fromJson(bad); throw StateError('Invalid payload accepted'); } on FormatException { /* expected */ }
  }
  print('Typed configuration DTO smoke passed');
}
