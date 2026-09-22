import 'dart:io';

import 'package:app_messaging_flutter/src/configure_android.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  late Directory root;
  late File gradle;
  setUp(() {
    root = Directory.systemTemp.createTempSync('app-messaging-test-');
    Directory('${root.path}/android/app').createSync(recursive: true);
    File('${root.path}/android/app/google-services.json').writeAsStringSync(
      '{"client":[{"client_info":{"android_client_info":{"package_name":"dev.appbase.example"}}}]}',
    );
    gradle = File('${root.path}/android/app/build.gradle.kts');
    gradle.writeAsStringSync(
      'plugins { id("com.android.application") }\nandroid { defaultConfig { applicationId = "dev.appbase.example" } }',
    );
  });
  tearDown(() => root.deleteSync(recursive: true));

  test('configuration is idempotent and preserves app settings', () {
    final before = gradle.readAsStringSync();
    final first = configureAndroid(root).values.single;
    expect(gradle.readAsStringSync(), before); // preflight does not write
    gradle.writeAsStringSync(first);
    final second = configureAndroid(root).values.single;
    expect(first, second);
    expect(second, contains('applicationId = "dev.appbase.example"'));
    expect(
      RegExp('com.google.gms.google-services').allMatches(second),
      hasLength(1),
    );
  });

  test('mismatched Google file fails without changes', () {
    gradle.writeAsStringSync(
      gradle.readAsStringSync().replaceAll(
        'dev.appbase.example',
        'another.application',
      ),
    );
    final before = gradle.readAsStringSync();
    expect(() => configureAndroid(root), throwsFormatException);
    expect(gradle.readAsStringSync(), before);
  });

  test('uses plugin version already supplied by host settings', () {
    File('${root.path}/android/settings.gradle.kts').writeAsStringSync(
      'plugins { id("com.google.gms.google-services") version "4.4.4" apply false }',
    );
    final content = configureAndroid(root).values.single;
    expect(content, isNot(contains('version "4.4.4"')));
  });
}
