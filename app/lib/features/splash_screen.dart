import 'package:flutter/material.dart';

import '../core/theme/theme_tokens.dart';

/// Shown while the profile is being read from storage: the router cannot decide
/// between onboarding and the session list before that answer is in.
class SplashScreen extends StatelessWidget {
  const SplashScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    return Scaffold(
      body: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Container(
              width: 56,
              height: 56,
              decoration: BoxDecoration(
                color: tokens.accent,
                borderRadius: BorderRadius.circular(tokens.radiusLg),
              ),
              alignment: Alignment.center,
              child: const Text(
                'π',
                style: TextStyle(
                  color: Colors.white,
                  fontSize: 26,
                  fontWeight: FontWeight.w700,
                ),
              ),
            ),
            SizedBox(height: tokens.spaceMd),
            Text('pi-ui', style: Theme.of(context).textTheme.titleMedium),
          ],
        ),
      ),
    );
  }
}
