import 'package:pocket_mfo_flutter/pocket_mfo_flutter.dart';

class FailingSessionStorage implements SessionStorage {
  final memory = MemorySessionStorage();
  final writes = <String>[];
  int readFailures = 0;
  int writeFailures = 0;

  @override
  Future<String?> read(String key) async {
    if (readFailures > 0) {
      readFailures--;
      throw StateError('Storage unavailable');
    }
    return memory.read(key);
  }

  @override
  Future<void> write(String key, String value) async {
    writes.add(value);
    if (writeFailures > 0) {
      writeFailures--;
      throw StateError('Storage unavailable');
    }
    await memory.write(key, value);
  }

  @override
  Future<void> delete(String key) => memory.delete(key);
}
