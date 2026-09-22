import 'package:flutter/material.dart';
import 'package:partner_links_flutter/partner_links_flutter.dart';
import 'package:pocketbase/pocketbase.dart';

/// Supply an authenticated PocketBase. Set AppMetrica SDK profile ID to PocketBase user.id.
class OfferButton extends StatefulWidget {
  const OfferButton({super.key, required this.pb, required this.linkId});
  final PocketBase pb;
  final String linkId;
  @override
  State<OfferButton> createState() => _OfferButtonState();
}

class _OfferButtonState extends State<OfferButton> {
  bool loading = false;
  @override
  Widget build(BuildContext context) => FilledButton(
    onPressed: loading
        ? null
        : () async {
            setState(() => loading = true);
            try {
              await widget.pb.openPartnerLink(context, linkId: widget.linkId);
            } on ClientException {
              if (context.mounted)
                ScaffoldMessenger.of(context).showSnackBar(
                  const SnackBar(
                    content: Text(
                      'Не удалось открыть предложение. Попробуйте ещё раз.',
                    ),
                  ),
                );
            } finally {
              if (mounted) setState(() => loading = false);
            }
          },
    child: Text(loading ? 'Загрузка…' : 'Открыть предложение'),
  );
}
