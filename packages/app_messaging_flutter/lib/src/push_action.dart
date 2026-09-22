import 'dart:convert';

enum PushSource { firebase, appMetrica }

/// An explicit notification tap. Receiving a push never produces an action.
final class PushAction {
  PushAction({
    required this.id,
    required this.source,
    required Map<String, dynamic> data,
  }) : data = Map.unmodifiable(data);

  final String id;
  final PushSource source;
  final Map<String, dynamic> data;

  factory PushAction.fromPayload({
    required String id,
    required PushSource source,
    required String payload,
  }) {
    final value = jsonDecode(payload);
    if (value is! Map<String, dynamic>) {
      throw const FormatException('Push payload must be a JSON object');
    }
    return PushAction(id: id, source: source, data: value);
  }
}

enum NotificationPermission {
  notDetermined,
  denied,
  deniedPermanently,
  authorized,
  provisional,
}
