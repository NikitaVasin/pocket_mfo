import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:partner_links_example/main.dart';

void main() {
  testWidgets('login screen starts without issuing network requests', (
    tester,
  ) async {
    await tester.pumpWidget(const PartnerLinksExample());
    expect(find.text('Демонстрационный вход'), findsOneWidget);
    expect(find.widgetWithText(FilledButton, 'Войти'), findsOneWidget);
    expect(find.text('default@variants.test'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
