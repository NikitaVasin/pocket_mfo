import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// Stores the entire session envelope atomically under a server/collection key.
/// Implementations must not log values or silently fall back to plaintext.
abstract interface class SessionStorage {
  Future<String?> read(String key);
  Future<void> write(String key, String value);
  Future<void> delete(String key);
}

final class SecureSessionStorage implements SessionStorage {
  const SecureSessionStorage({
    this.storage = const FlutterSecureStorage(
      iOptions: IOSOptions(
        accessibility: KeychainAccessibility.first_unlock_this_device,
      ),
    ),
  });
  final FlutterSecureStorage storage;
  @override
  Future<String?> read(String key) => storage.read(key: key);
  @override
  Future<void> write(String key, String value) =>
      storage.write(key: key, value: value);
  @override
  Future<void> delete(String key) => storage.delete(key: key);
}

/// Nonpersistent adapter for tests and the browser demo; not production storage.
final class MemorySessionStorage implements SessionStorage {
  final _values = <String, String>{};
  @override
  Future<String?> read(String key) async => _values[key];
  @override
  Future<void> write(String key, String value) async {
    _values[key] = value;
  }

  @override
  Future<void> delete(String key) async {
    _values.remove(key);
  }
}
