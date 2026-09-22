import 'dart:convert';

import 'package:appmetrica_plugin/appmetrica_plugin.dart';

/// All methods are awaited by PocketMfo; use one owner of the native SDK.
abstract interface class PocketMfoAnalytics {
  Future<void> activate(AppMetricaConfig config, String userId);
  Future<void> setUserId(String? userId);
  Future<void> reportEvent(String name, Map<String, Object?> parameters);
}

final class NativePocketMfoAnalytics implements PocketMfoAnalytics {
  static NativePocketMfoAnalytics? _owner;
  @override
  Future<void> activate(AppMetricaConfig config, String userId) async {
    if (_owner != null && _owner != this) {
      throw StateError('AppMetrica already has an owner in this isolate');
    }
    _owner = this;
    try {
      await AppMetrica.activate(
        AppMetricaConfig(
          config.apiKey,
          advIdentifiersTracking: config.advIdentifiersTracking,
          anrMonitoring: config.anrMonitoring,
          anrMonitoringTimeout: config.anrMonitoringTimeout,
          appBuildNumber: config.appBuildNumber,
          appEnvironment: config.appEnvironment,
          appOpenTrackingEnabled: config.appOpenTrackingEnabled,
          appVersion: config.appVersion,
          crashReporting: config.crashReporting,
          customHosts: config.customHosts,
          dataSendingEnabled: config.dataSendingEnabled,
          deviceType: config.deviceType,
          dispatchPeriodSeconds: config.dispatchPeriodSeconds,
          errorEnvironment: config.errorEnvironment,
          flutterCrashReporting: config.flutterCrashReporting,
          firstActivationAsUpdate: config.firstActivationAsUpdate,
          location: config.location,
          locationTracking: config.locationTracking,
          logs: config.logs,
          maxReportsCount: config.maxReportsCount,
          maxReportsInDatabaseCount: config.maxReportsInDatabaseCount,
          nativeCrashReporting: config.nativeCrashReporting,
          preloadInfo: config.preloadInfo,
          revenueAutoTrackingEnabled: config.revenueAutoTrackingEnabled,
          sessionTimeout: config.sessionTimeout,
          sessionsAutoTrackingEnabled: config.sessionsAutoTrackingEnabled,
          userProfileID: userId,
        ),
      );
    } catch (_) {
      _owner = null;
      rethrow;
    }
  }

  @override
  Future<void> setUserId(String? userId) => AppMetrica.setUserProfileID(userId);
  @override
  Future<void> reportEvent(String name, Map<String, Object?> parameters) =>
      AppMetrica.reportEventWithJson(name, jsonEncode(parameters));
}
