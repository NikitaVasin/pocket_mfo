import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:image/image.dart' as image;
import 'package:image_picker/image_picker.dart';
import 'package:path_provider/path_provider.dart';
import 'package:webview_flutter/webview_flutter.dart';
import 'package:webview_flutter_android/webview_flutter_android.dart'
    as webview_flutter_android;

Future<void> configureAndroidFilePicker(WebViewController controller) async {
  if (defaultTargetPlatform != .android) return;

  final androidController =
      controller.platform as webview_flutter_android.AndroidWebViewController;
  await androidController.setOnShowFileSelector(_pickFiles);
}

Future<List<String>> _pickFiles(
  webview_flutter_android.FileSelectorParams params,
) async {
  final isMultiple =
      params.mode == webview_flutter_android.FileSelectorMode.openMultiple;
  if (!params.acceptTypes.any((type) => type.startsWith('image/'))) {
    return [];
  }

  final picker = ImagePicker();
  final photo = await picker.pickImage(source: isMultiple ? .gallery : .camera);
  if (photo == null) return [];

  final imageData = await photo.readAsBytes();
  final decodedImage = image.decodeImage(imageData);
  if (decodedImage == null) return [];
  final scaledImage = image.copyResize(decodedImage, width: 500);
  final jpg = image.encodeJpg(scaledImage, quality: 90);
  final filePath = (await getTemporaryDirectory()).uri.resolve(
    './image_${DateTime.now().microsecondsSinceEpoch}.jpg',
  );
  final file = await File.fromUri(filePath).create(recursive: true);
  await file.writeAsBytes(jpg, flush: true);

  return [file.uri.toString()];
}
