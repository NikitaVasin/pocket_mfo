import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:appmetrica_plugin/appmetrica_plugin.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/widgets.dart';
import 'package:partner_links_flutter/partner_links_flutter.dart';
import 'package:pocketbase/pocketbase.dart';
import 'package:push_links_flutter/push_links_flutter.dart';

import 'analytics.dart';
import 'session_storage.dart';
import 'push_devices.dart';

typedef PocketMfoErrorHandler = void Function(Object error, StackTrace stack);

/// No permission prompt unless explicitly enabled. Navigation waits for ready.
final class PocketMfoPushConfig {
  const PocketMfoPushConfig({
    required this.navigatorKey,
    required this.onNavigate,
    this.requestPermissionOnStart = false,
    this.messagingDriver,
    this.embeddedViewBuilder,
    this.registerDevices = false,
    this.deviceId,
  });
  final GlobalKey<NavigatorState> navigatorKey;
  final Future<void> Function(Uri uri) onNavigate;
  final bool requestPermissionOnStart;
  final MessagingDriver? messagingDriver;
  final DynamicLinkEmbeddedViewBuilder? embeddedViewBuilder;

  /// Enable when the server installs plugins/push.
  final bool registerDevices;
  /// Override for the numeric AppMetrica API identifier (SDK DeviceIdHash).
  /// Do not return the hexadecimal AppMetrica.deviceId.
  final Future<String?> Function()? deviceId;
}

class PocketMfoAuthRequired implements Exception {
  const PocketMfoAuthRequired();
  @override
  String toString() =>
      'Sign in again or explicitly logout to start a new guest session.';
}

/// Own exactly one instance per server/auth collection for the app lifetime.
/// Calls through this facade serialize SDK identity changes with event sending.
/// External PocketBase authentication is observed without replacing authStore.
final class PocketMfo with WidgetsBindingObserver {
  PocketMfo({
    required this.pocketBase,
    required this.authCollection,
    required this.appMetricaConfig,
    SessionStorage? storage,
    PocketMfoAnalytics? analytics,
    this.push,
    this.onError,
    this.guestFields = const {},
  }) : storage = storage ?? const SecureSessionStorage(),
       analytics = analytics ?? NativePocketMfoAnalytics() {
    if (authCollection.isEmpty || authCollection == '_superusers') {
      throw ArgumentError.value(authCollection, 'authCollection');
    }
    if (appMetricaConfig.apiKey.trim().isEmpty) {
      throw ArgumentError('AppMetrica SDK API key is required');
    }
    const reserved = {'id', 'password', 'passwordConfirm', 'email', 'verified'};
    if (guestFields.keys.any(reserved.contains)) {
      throw ArgumentError(
        'Guest fields must not override identity or credentials',
      );
    }
  }

  final PocketBase pocketBase;
  final String authCollection;
  final AppMetricaConfig appMetricaConfig;
  final SessionStorage storage;
  final PocketMfoAnalytics analytics;
  final PocketMfoPushConfig? push;
  PushDevices? _pushDevices;
  final PocketMfoErrorHandler? onError;

  /// Nonprivileged required fields; their permission is enforced by server rules.
  final Map<String, Object?> guestFields;
  static final _owners = <String, PocketMfo>{};
  String get _storageKey =>
      'pocket_mfo:${sha256.convert(utf8.encode('${pocketBase.baseURL.replaceFirst(RegExp(r"/+$"), "")}|$authCollection')).toString()}';
  Future<void> _tail = Future.value();
  Future<void>? _initializing;
  Future<void>? _refreshing;
  int? _refreshEpoch;
  StreamSubscription<AuthStoreEvent>? _authSubscription;
  AppMessaging? _messaging;
  PushLinkResolver? _resolver;
  bool _prepared = false;
  bool _ready = false;
  bool _activated = false;
  bool _disposed = false;
  String? _sdkUser;
  String _identity = '';
  int _epoch = 0;
  Map<String, String>? _experiments;
  Map<String, dynamic>? _guest;

  RecordModel? get user {
    final record = pocketBase.authStore.record;
    if (record == null) return null;
    if (record.collectionName != authCollection &&
        record.collectionId != authCollection) {
      throw StateError('PocketBase is authenticated in a different collection');
    }
    return record;
  }

  bool get isGuest => user != null && user!.id == _guest?['id'];
  Map<String, String>? get experiments {
    _noticeIdentity();
    return _experiments == null ? null : Map.unmodifiable(_experiments!);
  }

  AppMessaging? get messaging => _messaging;

  void _alive() {
    if (_disposed) throw StateError('PocketMfo is disposed');
  }

  Future<T> _serial<T>(Future<T> Function() action) {
    _alive();
    final result = _tail.then((_) {
      _alive();
      return action();
    });
    _tail = result.then<void>((_) {}, onError: (Object _, StackTrace _) {});
    return result;
  }

  void _diagnose(Object error, StackTrace stack) {
    if (!_disposed) onError?.call(error, stack);
  }

  void _noticeIdentity() => _trackIdentity(user);

  void _trackIdentity(RecordModel? record) {
    final identity = record == null
        ? ''
        : '${record.collectionId}/${record.id}';
    if (identity != _identity) {
      _identity = identity;
      _epoch++;
      _experiments = null;
    }
  }

  bool get _valid {
    try {
      return user != null && pocketBase.authStore.isValid;
    } catch (_) {
      return false;
    }
  }

  Future<void> _prepare() async {
    if (_prepared) return;
    final owner = _owners[_storageKey];
    if (owner != null && owner != this) {
      throw StateError(
        'Use one PocketMfo instance per server and auth collection',
      );
    }
    _owners[_storageKey] = this;
    final saved = await storage.read(_storageKey);
    _alive();
    if (saved != null) {
      final data = jsonDecode(saved) as Map<String, dynamic>;
      _guest = data['guest'] == null
          ? null
          : Map<String, dynamic>.from(data['guest'] as Map);
      if (pocketBase.authStore.record == null && data['record'] != null) {
        pocketBase.authStore.save(
          data['token'] as String,
          RecordModel.fromJson(
            Map<String, dynamic>.from(data['record'] as Map),
          ),
        );
      }
    }
    _noticeIdentity();
    _authSubscription = pocketBase.authStore.onChange.listen((event) {
      if (_disposed) return;
      try {
        // AuthStore delivers events asynchronously. A clear/save pair can
        // already have restored the same user when the clear event arrives.
        _trackIdentity(event.record);
        _noticeIdentity();
        if (_ready) {
          unawaited(
            _serial(() async {
              await _syncAnalytics();
              await _persist();
              await _syncPushDevice();
            }).then((_) => refreshExperiments()).catchError(_diagnose),
          );
        }
      } catch (error, stack) {
        _diagnose(error, stack);
      }
    });
    WidgetsBinding.instance.addObserver(this);
    _prepared = true;
  }

  Future<void> _persist() {
    _alive();
    return storage.write(
      _storageKey,
      jsonEncode({
        'token': pocketBase.authStore.token,
        'record': pocketBase.authStore.record?.toJson(),
        'guest': _guest,
      }),
    );
  }

  /// Restores the saved session or signs in a guest with SDK-generated credentials.
  /// The application only awaits readiness; no email, ID or password is required.
  /// Concurrent calls share initialization and do not create additional guests.
  Future<void> initialize() {
    _alive();
    if (_ready) return Future.value();
    if (_initializing != null) return _initializing!;
    final result = _serial(() async {
      await _prepare();
      await _restoreAuth();
      await _syncAnalytics();
      await _persist();
      _alive();
      _ready = true;
      await _startMessaging();
    }).then((_) => refreshExperiments());
    _initializing = result;
    unawaited(
      result.then(
        (_) {
          _initializing = null;
        },
        onError: (Object _, StackTrace _) {
          _initializing = null;
        },
      ),
    );
    return result;
  }

  Future<void> _restoreAuth() async {
    if (user != null) {
      try {
        await pocketBase.collection(authCollection).authRefresh();
        return;
      } on ClientException catch (error) {
        if (error.statusCode == 0 || error.statusCode >= 500) {
          if (_valid) return; // keep an unexpired restored session offline
          rethrow;
        }
        if (error.statusCode != 401 && error.statusCode != 403) rethrow;
        if (!isGuest) throw const PocketMfoAuthRequired();
      }
    }
    await _guestAuth();
  }

  static String _random(int length, String alphabet) {
    final random = Random.secure();
    return List.generate(
      length,
      (_) => alphabet[random.nextInt(alphabet.length)],
    ).join();
  }

  Future<void> _guestAuth() async {
    if (_guest == null) {
      _guest = {
        'id': _random(15, 'abcdefghijklmnopqrstuvwxyz0123456789'),
        'password': _random(
          48,
          'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_',
        ),
        'registered': false,
      };
      // Save BEFORE any network operation. A lost response must reuse credentials.
      await _persist();
    }
    final guest = _guest!;
    final id = guest['id'] as String;
    final password = guest['password'] as String;
    final service = pocketBase.collection(authCollection);
    Future<RecordAuth> login() =>
        service.authWithPassword(id, password, body: {'identityField': 'id'});
    try {
      await login();
    } on ClientException catch (error) {
      // Only a never-confirmed registration can be retried. Network errors and
      // MFA/authorization errors must never cause creation of another account.
      if (error.statusCode != 400 ||
          guest['registered'] == true ||
          error.response['mfaId'] != null) {
        rethrow;
      }
      try {
        await service.create(
          body: {
            ...guestFields,
            'id': id,
            'email': '',
            'verified': false,
            'password': password,
            'passwordConfirm': password,
          },
        );
      } on ClientException catch (createError) {
        if (createError.statusCode != 400) rethrow;
        // Another request may have created the same ID. Authenticate, don't
        // replace credentials or generate another ID on an ambiguous failure.
      }
      await login();
    }
    guest['registered'] = true;
    _noticeIdentity();
    await _persist();
  }

  Future<void> _syncAnalytics() async {
    _alive();
    _noticeIdentity();
    final id = user?.id;
    if (!_activated) {
      if (id == null) throw const PocketMfoAuthRequired();
      await analytics.activate(appMetricaConfig, id);
      _activated = true;
      _sdkUser = id;
    } else if (_sdkUser != id) {
      await analytics.setUserId(id);
      _sdkUser = id;
    }
    _alive();
  }

  Future<void> signIn(String identity, String password) async {
    await _serial(() async {
      await _prepare();
      await _pushDevices?.disable();
      try {
        await pocketBase
            .collection(authCollection)
            .authWithPassword(identity, password);
      } catch (_) {
        // Failed authentication leaves the previous account active.
        await _syncPushDevice();
        rethrow;
      }
      _guest = null;
      _noticeIdentity();
      await _syncAnalytics();
      await _persist();
      _ready = true;
      await _startMessaging();
    });
    await refreshExperiments();
  }

  Future<void> logout() async {
    await _serial(() async {
      await _prepare();
      await _pushDevices?.disable();
      _ready = false;
      _guest = null;
      pocketBase.authStore.clear();
      _noticeIdentity();
      if (_activated) {
        await analytics.setUserId(null);
        _sdkUser = null;
      }
      await storage.delete(_storageKey);
      await _guestAuth();
      await _syncAnalytics();
      await _persist();
      _ready = true;
      await _startMessaging();
    });
    await refreshExperiments();
  }

  /// Reports fetch failures through onError; never blocks event delivery.
  Future<void> refreshExperiments() {
    _alive();
    _noticeIdentity();
    if (!_ready || user == null) return Future.value();
    final epoch = _epoch;
    if (_refreshEpoch == epoch && _refreshing != null) return _refreshing!;
    _refreshEpoch = epoch;
    final result = _fetchExperiments(epoch);
    _refreshing = result;
    unawaited(
      result.whenComplete(() {
        if (_refreshEpoch == epoch) {
          _refreshing = null;
          _refreshEpoch = null;
        }
      }),
    );
    return result;
  }

  Future<void> _fetchExperiments(int epoch) async {
    try {
      final response = await pocketBase
          .send<Map<String, dynamic>>('/api/variants/me')
          .timeout(const Duration(seconds: 2));
      if (_disposed) return;
      _noticeIdentity();
      if (_epoch != epoch) return;
      final raw = response['experiments'];
      if (raw is! Map<String, dynamic> ||
          raw.values.any((value) => value is! String)) {
        throw const FormatException('Invalid experiments response');
      }
      _experiments = Map.unmodifiable(raw.cast<String, String>());
    } catch (error, stack) {
      if (!_disposed && _epoch == epoch) {
        _diagnose(
          StateError('Could not refresh experiment assignments'),
          stack,
        );
      }
    }
  }

  Future<void> reportEvent(
    String name, {
    Map<String, Object?> parameters = const {},
  }) async {
    if (name.trim().isEmpty) throw ArgumentError.value(name, 'name');
    // Deep copy immediately so application mutations during initialize cannot
    // change an already requested event.
    final copy = (jsonDecode(jsonEncode(parameters)) as Map<String, dynamic>)
      ..remove('experiments');
    await initialize();
    await _serial(() async {
      await _syncAnalytics();
      final id = user?.id;
      if (id == null || _sdkUser != id || !_valid) {
        throw const PocketMfoAuthRequired();
      }
      _noticeIdentity();
      if (_experiments != null) {
        copy['experiments'] = Map<String, String>.of(_experiments!);
      }
      await analytics.reportEvent(name, copy);
    });
  }

  Future<PartnerLinkResult> resolvePartnerLink({required String linkId}) async {
    await initialize();
    return _serial(() async {
      if (!_valid) throw const PocketMfoAuthRequired();
      _noticeIdentity();
      final epoch = _epoch;
      final result = await pocketBase.resolvePartnerLink(linkId: linkId);
      if (_disposed || !_valid) throw const PocketMfoAuthRequired();
      _noticeIdentity();
      if (_epoch != epoch) throw const PocketMfoAuthRequired();
      return result;
    });
  }

  Future<bool> openPartnerLink(
    BuildContext context, {
    required String linkId,
    DynamicLinkActions actions = const PlatformDynamicLinkActions(),
    DynamicLinkEmbeddedViewBuilder? embeddedViewBuilder,
    DynamicLinkWarningDialogBuilder? warningDialogBuilder,
  }) async {
    final result = await resolvePartnerLink(linkId: linkId);
    if (_disposed || !context.mounted) return false;
    return result.link.open(
      context,
      actions: actions,
      embeddedViewBuilder: embeddedViewBuilder,
      warningDialogBuilder: warningDialogBuilder,
    );
  }

  Future<void> _startMessaging() async {
    final config = push;
    if (config == null) return;
    if (_messaging != null) {
      await _syncPushDevice();
      return;
    }
    if (config.registerDevices) {
      _pushDevices ??= PushDevices(
        pocketBase: pocketBase,
        storage: storage,
        deviceId: config.deviceId,
      );
    }
    _resolver = PushLinkResolver(
      navigatorKey: config.navigatorKey,
      onNavigate: config.onNavigate,
      resolvePartnerLink: (id) => resolvePartnerLink(linkId: id),
      beforeOpen: (_) async {
        await initialize();
        while (!_disposed && config.navigatorKey.currentState == null) {
          await WidgetsBinding.instance.endOfFrame;
        }
      },
      embeddedViewBuilder: config.embeddedViewBuilder,
    );
    _alive();
    final messaging = AppMessaging(
      driver:
          config.messagingDriver ??
          NativeMessagingDriver(analyticsAlreadyActivated: true),
    );
    _messaging = messaging;
    try {
      await messaging.initialize(
        appMetricaApiKey: appMetricaConfig.apiKey,
        onAction: (action) async {
          await initialize();
          await _syncPushDevice();
          try {
            await _pushDevices?.opened(action.data);
          } catch (error, stack) {
            _diagnose(error, stack);
          }
          if (action.data['type'] != 'app') await _resolver?.call(action);
        },
        onError: _diagnose,
        requestPermissionOnStart: config.requestPermissionOnStart,
      );
      await _syncPushDevice();
      if (_disposed) await messaging.dispose();
    } catch (error, stack) {
      await messaging.dispose();
      if (identical(_messaging, messaging)) _messaging = null;
      _resolver?.dispose();
      _resolver = null;
      _diagnose(error, stack);
    }
  }

  /// Refresh after changing notification permissions, including in OS settings.
  Future<void> refreshPushDevice() => _serial(_syncPushDevice);

  Future<void> _syncPushDevice() async {
    final devices = _pushDevices;
    final messaging = _messaging;
    if (devices == null || messaging == null || !_ready || _disposed) return;
    try {
      final permission = await messaging.getPermission();
      await devices.sync(
        enabled:
            permission == NotificationPermission.authorized ||
            permission == NotificationPermission.provisional,
        language: WidgetsBinding.instance.platformDispatcher.locale
            .toLanguageTag(),
        appVersion: appMetricaConfig.appVersion ?? '',
      );
    } catch (error, stack) {
      _diagnose(error, stack);
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed && _ready && !_disposed) {
      unawaited(refreshExperiments());
      unawaited(_serial(_syncPushDevice).catchError(_diagnose));
    }
  }

  Future<void> dispose() async {
    if (_disposed) return;
    _disposed = true;
    _epoch++;
    _experiments = null;
    WidgetsBinding.instance.removeObserver(this);
    await _authSubscription?.cancel();
    _resolver?.dispose();
    _pushDevices?.dispose();
    // Pending HTTP calls may finish later; _alive/epoch guards prevent them
    // from activating analytics or restoring subscriptions after disposal.
    await _messaging?.dispose();
    if (_owners[_storageKey] == this) _owners.remove(_storageKey);
  }
}
