import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:appmetrica_plugin/appmetrica_plugin.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:pocketbase/pocketbase.dart';

import 'session_storage.dart';

/// Persistent installation credentials are separate from the user's session.
/// Register only after analytics and the native push SDK are activated.
final class PushDevices {
  PushDevices({
    required this.pocketBase,
    required this.storage,
    Future<String?> Function()? deviceId,
  }) : _deviceId = deviceId ?? _appMetricaDeviceId;

  // Push API's appmetrica_device_id is DeviceIdHash, not the SDK's
  // hexadecimal deviceId. Request the API identifier from the SDK itself.
  static Future<String?> _appMetricaDeviceId() async {
    final params = await AppMetrica.requestStartupParams([
      AppMetricaStartupParams.deviceIdHashKey,
    ]).timeout(const Duration(seconds: 10));
    final id = params.result?.deviceIdHash;
    if (id == null || id.isEmpty) {
      throw StateError('AppMetrica API device ID is not available yet');
    }
    if (!RegExp(r'^[0-9]{1,20}$').hasMatch(id)) {
      throw const FormatException('Unexpected AppMetrica API device ID format');
    }
    return id;
  }

  final PocketBase pocketBase;
  final SessionStorage storage;
  final Future<String?> Function() _deviceId;
  Future<Map<String, String>>? _credentials;
  Future<void> _queue = Future.value();
  bool _disposed = false;

  String get _key =>
      'push_device_${sha256.convert(utf8.encode(pocketBase.baseURL))}';
  Future<Map<String, String>> _load() => _credentials ??= (() async {
    final existing = await storage.read(_key);
    if (existing != null) {
      return Map<String, String>.from(jsonDecode(existing) as Map);
    }
    final random = Random.secure();
    const alphabet = 'abcdefghijklmnopqrstuvwxyz0123456789';
    final credentials = {
      'id': List.generate(
        15,
        (_) => alphabet[random.nextInt(alphabet.length)],
      ).join(),
      'secret': base64UrlEncode(List.generate(32, (_) => random.nextInt(256)))
          .replaceAll('=', ''),
    };
    // Persist before the request: retrying a lost response keeps the same ID.
    await storage.write(_key, jsonEncode(credentials));
    return credentials;
  })();

  Future<void> _serial(Future<void> Function() action) {
    final result = _queue.then((_) async {
      if (!_disposed) await action();
    });
    _queue = result.then((_) {}, onError: (Object _, StackTrace _) {});
    return result;
  }

  Future<void> sync({
    required bool enabled,
    required String language,
    String appVersion = '',
    String? platform,
  }) {
    final token = pocketBase.authStore.token;
    final user = pocketBase.authStore.record?.id;
    return _serial(() async {
      if (user == null || token.isEmpty) return;
      final device = await _deviceId();
      if (device == null || device.isEmpty) return;
      final credentials = await _load();
      if (_disposed ||
          pocketBase.authStore.token != token ||
          pocketBase.authStore.record?.id != user) {
        return;
      }
      await pocketBase.send<Map<String, dynamic>>(
        '/api/push/devices',
        method: 'POST',
        headers: {'Authorization': token},
        body: {
          ...credentials,
          'deviceId': device,
          'platform':
              platform ??
              (defaultTargetPlatform == TargetPlatform.iOS ? 'ios' : 'android'),
          'language': language,
          'appVersion': appVersion,
          'enabled': enabled,
        },
      );
    });
  }

  /// Await before clearing/changing auth to retire the old account's binding.
  Future<void> disable() {
    final token = pocketBase.authStore.token;
    return _serial(() async {
      if (token.isEmpty) return;
      final stored = await storage.read(_key);
      if (stored == null || _disposed) return;
      await pocketBase.send<void>(
        '/api/push/devices/disable',
        method: 'POST',
        headers: {'Authorization': token},
        body: Map<String, dynamic>.from(jsonDecode(stored) as Map),
      );
    });
  }

  Future<void> opened(Map<String, dynamic> payload) {
    final token = pocketBase.authStore.token;
    return _serial(() async {
      if (payload['pushRunId'] is! String ||
          payload['pushToken'] is! String ||
          token.isEmpty) {
        return;
      }
      final credentials = await _load();
      if (_disposed || pocketBase.authStore.token != token) return;
      await pocketBase.send<void>(
        '/api/push/open',
        method: 'POST',
        headers: {'Authorization': token},
        body: {
          'deviceId': credentials['id'],
          'secret': credentials['secret'],
          'runId': payload['pushRunId'],
          'token': payload['pushToken'],
        },
      );
    });
  }

  void dispose() {
    _disposed = true;
  }
}
