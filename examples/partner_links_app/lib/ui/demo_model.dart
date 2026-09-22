import 'package:flutter/foundation.dart';
import 'package:partner_links_flutter/partner_links_flutter.dart';
import 'package:pocketbase/pocketbase.dart';

import '../data/demo_repository.dart';

class DemoModel extends ChangeNotifier {
  DemoModel(this.repository);
  final DemoRepository repository;
  bool _disposed = false;
  @override
  void notifyListeners() {
    if (!_disposed) super.notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    super.dispose();
  }

  List<RecordModel> offers = const [];
  List<RecordModel> orders = const [];
  bool busy = false;
  String? error;

  Future<void> load() => _run(_load);

  Future<void> refresh() => _run(() async {
    await _load();
    await repository.integration.refreshExperiments();
  });

  Future<void> _load() async {
    offers = List.unmodifiable(await repository.offers());
    orders = List.unmodifiable(await repository.orders());
  }

  Future<PartnerLinkResult?> resolve(String id) async {
    PartnerLinkResult? result;
    await _run(() async {
      result = await repository.resolve(id);
    });
    return result;
  }

  Future<void> _run(Future<void> Function() action) async {
    if (busy) return;
    busy = true;
    error = null;
    notifyListeners();
    try {
      await action();
    } on ClientException catch (e) {
      error = switch (e.statusCode) {
        0 => 'Сервер недоступен. Проверьте URL и подключение.',
        401 || 403 => 'Нет доступа. Проверьте пользователя и права коллекции.',
        503 => 'Сервер ещё не настроен: проверьте настройки партнёрских ссылок и AppMetrica.',
        _ =>
          e.response['message'] as String? ??
              'Ошибка запроса (${e.statusCode}).',
      };
    } catch (_) {
      error = 'Не удалось обработать ответ сервера.';
    } finally {
      busy = false;
      notifyListeners();
    }
  }

  void showOpenError() {
    error = 'Не удалось открыть ссылку. Разрешите всплывающие окна в браузере.';
    notifyListeners();
  }
}
