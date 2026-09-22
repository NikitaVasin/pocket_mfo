import 'package:dynamic_link/dynamic_link.dart';
import 'package:test/test.dart';

void main() {
  test('links with the same configuration are equal', () {
    final first = _link();
    final second = _link();

    expect(first, second);
  });

  test('opening mode participates in equality', () {
    final first = _link();
    final second = _link(mode: .browser);

    expect(first, isNot(second));
  });
}

DynamicLink _link({DynamicLinkMode mode = .view}) {
  return DynamicLink(
    url: Uri.parse('https://example.com'),
    mode: mode,
    saveCooke: true,
    changeClient: true,
    showLoader: true,
    openUrlsInBrowser: false,
    skipWarningDialog: false,
    warningDialog: const DynamicLinkWarningDialog(
      title: 'Warning',
      content: 'Continue?',
    ),
    title: 'Example',
    trackName: 'example_link',
  );
}
