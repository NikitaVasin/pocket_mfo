import 'dart:convert';
import 'dart:io';

/// Preflight returns edits without mutating files. Intentionally fails on
/// nonstandard/flavored projects instead of guessing another Firebase client.
Map<File, String> configureAndroid(Directory app) {
  final android = Directory('${app.path}/android');
  if (!android.existsSync()) return {};
  final jsonFile = File('${android.path}/app/google-services.json');
  final json = jsonDecode(jsonFile.readAsStringSync()) as Map<String, dynamic>;
  var gradle = File('${android.path}/app/build.gradle.kts');
  final kotlin = gradle.existsSync();
  if (!kotlin) gradle = File('${android.path}/app/build.gradle');
  var content = gradle.readAsStringSync();
  final id = RegExp(r'''applicationId\s*(?:=\s*)?["']([^"']+)["']''')
      .firstMatch(content)
      ?.group(1);
  if (id == null || content.contains('productFlavors')) {
    throw const FormatException(
      'Configure requires a literal applicationId and no productFlavors. Configure custom variants using the README.',
    );
  }
  final clients = json['client'] as List;
  if (!clients.any(
    (c) => c['client_info']['android_client_info']['package_name'] == id,
  )) {
    throw const FormatException(
      'google-services.json does not match applicationId',
    );
  }
  if (!content.contains('com.google.gms.google-services')) {
    final plugins = RegExp(r'plugins\s*\{').firstMatch(content);
    if (plugins == null)
      throw const FormatException('Expected Gradle plugins block');
    final settings = [
      File('${android.path}/settings.gradle.kts'),
      File('${android.path}/settings.gradle'),
      File('${android.path}/build.gradle'),
      File('${android.path}/build.gradle.kts'),
    ];
    final supplied = settings
        .where((f) => f.existsSync())
        .any(
          (f) =>
              f.readAsStringSync().contains('com.google.gms.google-services') ||
              f.readAsStringSync().contains('com.google.gms:google-services:'),
        );
    final line = kotlin
        ? '\n    id("com.google.gms.google-services")${supplied ? '' : ' version "4.4.4"'}'
        : '\n    id "com.google.gms.google-services"${supplied ? '' : ' version "4.4.4"'}';
    content = content.replaceRange(plugins.end, plugins.end, line);
  }
  return {gradle: content};
}
