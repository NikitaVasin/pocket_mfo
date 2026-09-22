import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart';
import 'package:pocket_mfo_flutter/pocket_mfo_flutter.dart';
import 'package:pocketbase/pocketbase.dart';

import 'data/demo_repository.dart';
import 'ui/demo_model.dart';

void main() => runApp(const PartnerLinksExample(enableMessaging: true));

class PartnerLinksExample extends StatefulWidget {
  const PartnerLinksExample({
    super.key,
    this.enableMessaging = false,
    this.integration,
  });
  final PocketMfo? integration;
  final bool enableMessaging;
  @override
  State<PartnerLinksExample> createState() => _PartnerLinksExampleState();
}

class _PartnerLinksExampleState extends State<PartnerLinksExample> {
  final navigatorKey = GlobalKey<NavigatorState>();
  late final PocketMfo integration;
  late final DemoModel model;
  late Future<Exception?> initialization;

  @override
  void initState() {
    super.initState();
    // Client SDK key for this example; the Post API key stays on the server.
    const sdkKey = String.fromEnvironment(
      'APPMETRICA_SDK_KEY',
      defaultValue: 'fbca87ec-97c5-4df4-b4aa-aed80630f2fa',
    );
    final preview = kIsWeb || sdkKey.isEmpty;
    integration =
        widget.integration ??
        PocketMfo(
          pocketBase: PocketBase(_DemoPageState.serverUrl),
          authCollection: 'users',
          appMetricaConfig: AppMetricaConfig(preview ? 'preview-only' : sdkKey),
          analytics: preview ? PreviewAnalytics() : null,
          storage: kIsWeb ? MemorySessionStorage() : null,
          push: !preview && widget.enableMessaging
              ? PocketMfoPushConfig(
                  navigatorKey: navigatorKey,
                  requestPermissionOnStart: true,
                  onNavigate: (uri) async {
                    if (!mounted) return;
                    if (uri.path == '/orders') {
                      unawaited(
                        navigatorKey.currentState!.push<void>(
                          MaterialPageRoute(
                            builder: (_) => _OrdersPage(model: model),
                          ),
                        ),
                      );
                    } else if (uri.path == '/offers') {
                      navigatorKey.currentState!.popUntil(
                        (route) => route.isFirst,
                      );
                    } else {
                      throw const FormatException('Unknown route');
                    }
                  },
                )
              : null,
        );
    model = DemoModel(DemoRepository(integration));
    initialization = _initialize();
  }

  // Handle failures immediately, even before FutureBuilder's next frame.
  Future<Exception?> _initialize() async {
    try {
      await integration.initialize();
      await model.load();
      return null;
    } on Exception catch (error) {
      return error;
    }
  }

  @override
  void dispose() {
    unawaited(integration.dispose());
    model.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => MaterialApp(
    navigatorKey: navigatorKey,
    title: 'Партнёрские ссылки',
    theme: ThemeData(colorSchemeSeed: const Color(0xff315b7c)),
    darkTheme: ThemeData(
      brightness: Brightness.dark,
      colorSchemeSeed: const Color(0xff315b7c),
    ),
    home: FutureBuilder<Exception?>(
      future: initialization,
      builder: (context, snapshot) {
        if (snapshot.connectionState != ConnectionState.done) {
          return const Scaffold(
            body: Center(child: CircularProgressIndicator()),
          );
        }
        if (snapshot.hasError || snapshot.data != null) {
          return Scaffold(
            body: Center(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 24),
                    child: Text(
                      snapshot.data is ClientException &&
                              (snapshot.data! as ClientException).statusCode ==
                                  0
                          ? 'Сервер ${_DemoPageState.serverUrl} недоступен. '
                                'Проверьте подключение и проброс портов Android.'
                          : 'Не удалось инициализировать SDK. Попробуйте ещё раз.',
                      textAlign: TextAlign.center,
                    ),
                  ),
                  const SizedBox(height: 12),
                  FilledButton(
                    onPressed: () => setState(() {
                      initialization = _initialize();
                    }),
                    child: const Text('Повторить'),
                  ),
                ],
              ),
            ),
          );
        }
        return DemoPage(model: model, integration: integration);
      },
    ),
  );
}

class DemoPage extends StatefulWidget {
  const DemoPage({super.key, required this.model, required this.integration});
  final DemoModel model;
  final PocketMfo integration;
  @override
  State<DemoPage> createState() => _DemoPageState();
}

class _DemoPageState extends State<DemoPage> {
  static const serverUrl = String.fromEnvironment(
    'POCKETBASE_URL',
    defaultValue: 'http://127.0.0.1:8090',
  );
  DemoModel get model => widget.model;
  Future<void> open(String id) async {
    // A fresh token for every tap. No HTTP prefetch and no second click event.
    final result = await model.resolve(id);
    if (!mounted || result == null) return;
    try {
      final opened = await result.link.open(context);
      if (mounted && !opened) model.showOpenError();
    } catch (_) {
      if (mounted) model.showOpenError();
    }
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: model,
    builder: (context, _) => Scaffold(
      appBar: AppBar(
        title: const Text('Партнёрские ссылки'),
        actions: [
          IconButton(
            tooltip: 'Обновить',
            onPressed: model.busy ? null : model.refresh,
            icon: const Icon(Icons.refresh),
          ),
        ],
      ),
      body: Align(
        alignment: Alignment.topCenter,
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 760),
          child: ListView(
            padding: const EdgeInsets.all(24),
            children: [
              Text(serverUrl, style: Theme.of(context).textTheme.bodySmall),
              ...[
                Semantics(
                  container: true,
                  child: Text(
                    'Пользователь: ${widget.integration.user?.id} · ${widget.integration.isGuest ? "гость" : "аккаунт"}',
                  ),
                ),
                Text(
                  'Эксперименты: ${jsonEncode(widget.integration.experiments)}',
                ),
                TextButton(
                  onPressed: model.busy
                      ? null
                      : () async {
                          try {
                            await widget.integration.reportEvent(
                              'demo_button',
                              parameters: {'screen': 'offers'},
                            );
                            if (context.mounted) {
                              ScaffoldMessenger.of(context).showSnackBar(
                                const SnackBar(
                                  content: Text('Событие отправлено'),
                                ),
                              );
                            }
                          } catch (_) {
                            model.showOpenError();
                          }
                        },
                  child: const Text('Отправить тестовое событие'),
                ),
                if (widget.integration.messaging != null)
                  TextButton(
                    onPressed: () =>
                        widget.integration.messaging!.requestPermission(),
                    child: const Text('Разрешить уведомления'),
                  ),
              ],
              const SizedBox(height: 20),
              if (model.busy) const LinearProgressIndicator(),
              if (model.error != null)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 16),
                  child: Text(
                    model.error!,
                    style: TextStyle(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                ),
              ...[
                Text(
                  'Офферы',
                  style: Theme.of(context).textTheme.headlineSmall,
                ),
                const SizedBox(height: 12),
                if (model.offers.isEmpty) const Text('Нет доступных офферов.'),
                for (final offer in model.offers)
                  Card(
                    child: ListTile(
                      title: Text(offer.getStringValue('name')),
                      subtitle: Text(offer.getStringValue('provider')),
                      trailing: const Icon(Icons.open_in_new),
                      onTap: model.busy ? null : () => open(offer.id),
                    ),
                  ),
                const SizedBox(height: 28),
                Text(
                  'Мои заказы',
                  style: Theme.of(context).textTheme.headlineSmall,
                ),
                const SizedBox(height: 12),
                if (model.orders.isEmpty)
                  const Text('Заказы появятся после постбеков партнёра.'),
                for (final order in model.orders)
                  Card(
                    child: ListTile(
                      title: Text(
                        'Заявка ${order.getStringValue('leadId').isEmpty ? order.id : order.getStringValue('leadId')}',
                      ),
                      subtitle: Text(
                        statusLabel(order.getStringValue('status')),
                      ),
                    ),
                  ),
              ],
            ],
          ),
        ),
      ),
    ),
  );
}

class _OrdersPage extends StatelessWidget {
  const _OrdersPage({required this.model});
  final DemoModel model;
  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Мои заказы')),
    body: ListenableBuilder(
      listenable: model,
      builder: (context, _) => ListView(
        padding: const EdgeInsets.all(24),
        children: [
          if (model.orders.isEmpty)
            const Text('Заказы появятся после постбеков партнёра.'),
          for (final order in model.orders)
            ListTile(
              title: Text(
                order.getStringValue('leadId').isEmpty
                    ? order.id
                    : order.getStringValue('leadId'),
              ),
              subtitle: Text(statusLabel(order.getStringValue('status'))),
            ),
        ],
      ),
    ),
  );
}

String statusLabel(String status) => switch (status) {
  'lead' => 'Заявка создана',
  'approved' => 'Подтверждена',
  'hold' => 'Ожидает решения',
  'rejected' => 'Отказ',
  _ => status,
};

// Browser/no-key adapter: exercises the integration without sending analytics.
class PreviewAnalytics implements PocketMfoAnalytics {
  @override
  Future<void> activate(AppMetricaConfig config, String userId) async {}
  @override
  Future<void> setUserId(String? userId) async {}
  @override
  Future<void> reportEvent(
    String name,
    Map<String, Object?> parameters,
  ) async {}
}
