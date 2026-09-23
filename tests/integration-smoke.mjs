// Compile the actual agent-guide examples as independent consumers.
import { execFileSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

const root = resolve('.');
const guide = readFileSync('docs/AI_INTEGRATION.md', 'utf8');
const block = (language, marker) => {
    const blocks = [...guide.matchAll(new RegExp('```' + language + '\\n([\\s\\S]*?)\\n```', 'g'))];
    const result = blocks.filter(match => match[1].includes(marker));
    if (result.length !== 1) throw new Error(`Expected one ${language} guide example containing ${marker}`);
    return result[0][1];
};
const dir = mkdtempSync(join(tmpdir(), 'pocket-mfo-integration-'));
const run = (command, args, cwd) => execFileSync(command, args, { cwd, stdio: 'inherit', env: { ...process.env, GOTOOLCHAIN: 'auto' } });
try {
    const go = join(dir, 'server');
    mkdirSync(go);
    const version = readFileSync('go.mod', 'utf8').match(/^go (.+)$/m)[1];
    writeFileSync(join(go, 'go.mod'), `module integrationcheck\n\ngo ${version}\n\nrequire github.com/NikitaVasin/pocket_mfo v0.0.0\nreplace github.com/NikitaVasin/pocket_mfo => ${JSON.stringify(root)}\n`);
    writeFileSync(join(go, 'integration.go'), block('go', 'type IntegrationConfig struct'));
    run('go', ['build', '-mod=mod', './...'], go);

    const dart = join(dir, 'client');
    mkdirSync(join(dart, 'lib'), { recursive: true });
    const sdk = JSON.stringify(readFileSync('pubspec.yaml', 'utf8').match(/^  sdk: (.+)$/m)[1]);
    writeFileSync(join(dart, 'pubspec.yaml'), `name: integrationcheck\npublish_to: none\nenvironment:\n  sdk: ${sdk}\ndependencies:\n  flutter:\n    sdk: flutter\n  pocketbase: ^0.25.1\n  pocket_mfo_flutter:\n    path: ${JSON.stringify(join(root, 'packages/pocket_mfo_flutter'))}\n`);
    writeFileSync(join(dart, 'lib/integration.dart'), block('dart', 'PocketMfo createIntegration'));
    // Use the SDK selected by FVM in the repository, not a global Flutter.
    const flutter = join(root, '.fvm/flutter_sdk/bin', process.platform === 'win32' ? 'flutter.bat' : 'flutter');
    run(flutter, ['pub', 'get'], dart);
    run(flutter, ['analyze', '--no-pub'], dart);
    console.log('Integration smoke passed: guide Go registration and Flutter facade compile in independent consumers.');
} finally {
    rmSync(dir, { recursive: true, force: true });
}
