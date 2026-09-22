import 'dart:io';
import 'dart:isolate';

import 'package:app_messaging_flutter/src/configure_android.dart';

/// Run from the consuming application's root. iOS uses the xcodeproj Ruby gem
/// bundled with CocoaPods; it edits both CocoaPods and SPM Xcode projects.
Future<void> main(List<String> args) async {
  if (args.isNotEmpty) {
    stderr.writeln(
      'Run from the app directory: dart run app_messaging_flutter:configure',
    );
    exitCode = 64;
    return;
  }
  try {
    final root = Directory.current;
    final changes = configureAndroid(root);
    final ios = Directory('${root.path}/ios');
    final library = await Isolate.resolvePackageUri(
      Uri.parse('package:app_messaging_flutter/app_messaging_flutter.dart'),
    );
    if (library == null)
      throw StateError('Cannot locate app_messaging_flutter');
    final script = File.fromUri(library.resolve('../tool/configure_ios.rb'));
    if (ios.existsSync()) {
      if (!Platform.isMacOS)
        throw StateError(
          'Configure iOS on macOS (Ruby + xcodeproj/CocoaPods required)',
        );
      var ruby = 'ruby';
      Map<String, String>? environment;
      for (final prefix in ['/opt/homebrew', '/usr/local']) {
        final executable = File('$prefix/opt/ruby/bin/ruby');
        final gems = Directory('$prefix/opt/cocoapods/libexec');
        if (executable.existsSync() && gems.existsSync()) {
          ruby = executable.path;
          environment = {'GEM_HOME': gems.path};
          break;
        }
      }
      final check = await Process.run(ruby, [
        script.path,
        root.path,
        '--check',
      ], environment: environment);
      if (check.exitCode != 0) throw StateError(check.stderr.toString().trim());
      final result = await Process.run(ruby, [
        script.path,
        root.path,
      ], environment: environment);
      if (result.exitCode != 0)
        throw StateError(result.stderr.toString().trim());
      stdout.write(result.stdout);
    }
    for (final entry in changes.entries) {
      entry.key.writeAsStringSync(entry.value);
    }
    if (changes.isNotEmpty)
      stdout.writeln('Android: Google Services подключён.');
    stdout.writeln(
      'Настройка завершена. Ключ AppMetrica передайте в initialize().',
    );
  } catch (error) {
    stderr.writeln('Не удалось настроить приложение: $error');
    exitCode = 1;
  }
}
