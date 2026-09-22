// Keep the same loopback URLs in PocketBase, the partner, and Flutter.
import { existsSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';

const executable = process.platform === 'win32' ? 'adb.exe' : 'adb';
const roots = [
  process.env.ANDROID_HOME,
  process.env.ANDROID_SDK_ROOT,
  join(homedir(), 'Library', 'Android', 'sdk'),
  join(homedir(), 'Android', 'Sdk'),
  process.env.LOCALAPPDATA && join(process.env.LOCALAPPDATA, 'Android', 'Sdk'),
].filter(Boolean);
const adb = roots.map(root => join(root, 'platform-tools', executable))
  .find(existsSync) ?? executable;
const devices = spawnSync(adb, ['devices'], { encoding: 'utf8' });
if (devices.error?.code === 'ENOENT') {
  console.log('adb не найден: для Android установите SDK и задайте ANDROID_HOME. Для iOS/web проброс не нужен.');
  process.exit(0);
}
if (devices.error || devices.status !== 0) {
  console.error(devices.error?.message ?? devices.stderr);
  process.exit(1);
}
const serials = devices.stdout.split(/\r?\n/)
  .map(line => line.trim().split(/\s+/))
  .filter(([serial, state]) => state === 'device' &&
    (process.env.ANDROID_SERIAL ? serial === process.env.ANDROID_SERIAL : serial.startsWith('emulator-')))
  .map(([serial]) => serial);
if (serials.length === 0) {
  console.log('Нет запущенного Android-эмулятора. Для Android запустите его перед F5; для iOS/web проброс не нужен.');
}
for (const serial of serials) {
  for (const port of [8090, 8091]) {
    const result = spawnSync(adb, ['-s', serial, 'reverse', `tcp:${port}`, `tcp:${port}`], { encoding: 'utf8' });
    if (result.error || result.status !== 0) {
      console.error(result.error?.message ?? result.stderr);
      process.exit(1);
    }
    console.log(`${serial}: localhost:${port} → Mac/PC:${port}`);
  }
}
