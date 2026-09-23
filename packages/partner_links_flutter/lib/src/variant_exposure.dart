import 'package:pocketbase/pocketbase.dart';

/// Server-issued context of returned content, retained with the rendered record.
/// Only the server verifies [token]; it never grants access to a partner link.
final class VariantExposure {
  VariantExposure._({
    required this.id,
    required this.token,
    required this.user,
    required this.authCollection,
    required this.collection,
    required this.record,
    required this.revision,
    required this.set,
    required this.version,
    required Map<String, String> experiments,
  }) : experiments = Map.unmodifiable(experiments);

  final String id,
      token,
      user,
      authCollection,
      collection,
      record,
      revision,
      set;
  final int version;
  final Map<String, String> experiments;

  static VariantExposure? fromRecord(RecordModel record) {
    final raw = record.data['variantContext'];
    if (raw == null) return null;
    if (raw is! Map<String, dynamic>) {
      throw const FormatException('Invalid variantContext');
    }
    return VariantExposure.fromJson(raw);
  }

  factory VariantExposure.fromJson(Map<String, dynamic> json) {
    String text(String key) {
      final value = json[key];
      if (value is! String || value.isEmpty) {
        throw FormatException('Invalid exposure $key');
      }
      return value;
    }

    final decision = json['decision'];
    final experiments = json['experiments'];
    if (decision is! Map<String, dynamic> ||
        decision['version'] is! int ||
        (decision['version'] as int) < 1 ||
        decision['set'] is! String ||
        (decision['set'] as String).isEmpty ||
        experiments is! Map<String, dynamic> ||
        experiments.values.any((value) => value is! String)) {
      throw const FormatException('Invalid exposure assignment');
    }
    return VariantExposure._(
      id: text('id'),
      token: text('token'),
      user: text('user'),
      authCollection: text('authCollection'),
      collection: text('collection'),
      record: text('record'),
      revision: text('revision'),
      set: decision['set'] as String,
      version: decision['version'] as int,
      experiments: experiments.cast<String, String>(),
    );
  }

  /// Analytics metadata excludes the signed token.
  Map<String, Object?> get analyticsParameters => {
    'id': id,
    'collection': collection,
    'record': record,
    'revision': revision,
    'set': set,
    'version': version,
  };
}
