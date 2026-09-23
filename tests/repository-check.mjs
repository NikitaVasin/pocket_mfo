// Publication metadata and local documentation links; no network or credentials.
import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { dirname, resolve } from 'node:path';

const files = [...new Set(execFileSync('git', ['ls-files', '--cached', '--others', '--exclude-standard', '-z'], { encoding: 'utf8' }).split('\0').filter(Boolean))];
const errors = [];
const read = path => readFileSync(path, 'utf8');
for (const file of files.filter(f => f.endsWith('.md'))) {
    // Code examples may contain placeholder links; inspect actual Markdown only.
    const text = read(file).replace(/^```[^\n]*\n[\s\S]*?^```\s*$/gm, '');
    for (const match of text.matchAll(/\[[^\]]*\]\(([^\s)]+)(?:\s+"[^"]*")?\)/g)) {
        const target = match[1].replace(/^<|>$/g, '').split('#')[0];
        if (!target || /^(?:[a-z][a-z\d+.-]*:|\/\/)/i.test(target)) continue;
        if (!existsSync(resolve(dirname(file), decodeURIComponent(target)))) errors.push(`${file}: missing ${target}`);
    }
}
for (const entry of readdirSync('plugins', { withFileTypes: true }).filter(e => e.isDirectory())) {
    const plugin = `plugins/${entry.name}`;
    for (const doc of ['README.md', 'AGENTS.md']) if (!existsSync(`${plugin}/${doc}`)) errors.push(`${plugin}: missing ${doc}`);
    for (const index of ['README.md', 'AGENTS.md', 'docs/AI_INTEGRATION.md']) {
        if (!read(index).includes(`${plugin}/AGENTS.md`)) errors.push(`${index}: missing agent instructions for ${plugin}`);
    }
    if (!read('docs/AI_INTEGRATION.md').includes(`"github.com/NikitaVasin/pocket_mfo/${plugin}"`)) errors.push(`Integration registration omits ${plugin}`);
}
for (const entry of readdirSync('packages', { withFileTypes: true }).filter(e => e.isDirectory())) {
    const pkg = `packages/${entry.name}`;
    for (const doc of ['README.md', 'AGENTS.md']) if (!existsSync(`${pkg}/${doc}`)) errors.push(`${pkg}: missing ${doc}`);
    if (!read('AGENTS.md').includes(`${pkg}/AGENTS.md`)) errors.push(`AGENTS.md: missing instructions for ${pkg}`);
    if (!read('docs/AI_INTEGRATION.md').includes(`${pkg}/README.md`)) errors.push(`Integration guide omits ${pkg}`);
}
if (!read('go.mod').startsWith('module github.com/NikitaVasin/pocket_mfo\n')) errors.push('Incorrect Go module path');
for (const file of files) {
    if (/(^|\/)(pb_data|pb_backups|node_modules|\.dart_tool|\.fvm|build|test-results)\/|\.(?:db(?:-wal|-shm)?|sqlite3?|p8|p12|pem|key|jks|keystore)$|(^|\/)\.env(?:$|\.(?!example$))/.test(file)) errors.push(`Private/generated artifact included: ${file}`);
}
if (errors.length) throw new Error(errors.join('\n'));
console.log(`Repository checks passed: ${files.length} files, plugin instructions, local Markdown links and publication artifacts.`);
