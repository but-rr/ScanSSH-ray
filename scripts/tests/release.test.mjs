import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { prepareRelease, releaseNotes, releaseVersion } from '../release.mjs';

test('release tags must be stable versions compatible with Windows resources', () => {
  assert.equal(releaseVersion('v0.2.2'), '0.2.2');
  assert.equal(releaseVersion('v1.0.65535'), '1.0.65535');
  for (const tag of [undefined, '', '0.2.2', 'v.0.2.2', 'v0.02.2', 'v0.2', 'v0.2.2-beta.1', 'v0.2.2+build', 'v1.0.65536', 'v1.0.0\n']) {
    assert.throws(() => releaseVersion(tag), /Release tag|Windows version/);
  }
});

test('only the exact requested version is extracted, preserving Markdown and Russian text', () => {
  const changelog = '\uFEFF# Changelog\r\n\r\n## [Unreleased]\r\n- Будущие изменения\r\n\r\n## [0.2.20]\r\n- Другая версия\r\n\r\n## [0.2.2] - 2026-10-29\r\n\r\n### Исправлено\r\n- Исправление трея\r\n\r\n## [0.2.1]\r\n- Старые изменения\r\n';
  assert.equal(releaseNotes(changelog, 'v0.2.2'), '### Исправлено\n- Исправление трея\n');
  assert.equal(releaseNotes(changelog, 'v0.2.1'), '- Старые изменения\n');
  for (const heading of ['## [v0.2.2]', '## v0.2.2', '## 0.2.2']) {
    assert.equal(releaseNotes(`${heading}\n\n- Изменения\n`, 'v0.2.2'), '- Изменения\n');
  }
});

test('headings inside fenced code examples do not split release notes', () => {
  const body = '- Example\n\n```md\n## [0.2.1]\n```\n\n~~~md\n## [0.2.0]\n~~~\n\n- Done';
  assert.equal(releaseNotes(`## [0.2.2]\n${body}\n## [0.2.1]\nOld`, 'v0.2.2'), `${body}\n`);
});

test('missing, empty and duplicate entries fail instead of using other release descriptions', () => {
  assert.throws(() => releaseNotes('## [0.2.20]\n- Wrong version', 'v0.2.2'), /no section/);
  assert.throws(() => releaseNotes('## [0.2.2]\n\n## [0.2.1]\n- Old', 'v0.2.2'), /no release notes/);
  assert.throws(() => releaseNotes('## [0.2.2]\n<!-- Write changes here -->', 'v0.2.2'), /no release notes/);
  assert.throws(() => releaseNotes('## [0.2.2]\n- First\n## v0.2.2\n- Second', 'v0.2.2'), /duplicate/);
});

function projectFixture(t, changelog) {
  const root = mkdtempSync(join(tmpdir(), 'scanssh-release-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, 'frontend'));
  const files = {
    'wails.json': { info: { productVersion: '0.2.1', productName: 'ScanSSH-ray' }, 'frontend:build': 'npm run build' },
    'frontend/package.json': { version: '0.2.1', dependencies: { svelte: '^5.0.0' } },
    'frontend/package-lock.json': { version: '0.2.1', lockfileVersion: 3, packages: { '': { version: '0.2.1' }, 'node_modules/svelte': { version: '5.0.0', integrity: 'unchanged' } } },
  };
  for (const [file, json] of Object.entries(files)) writeFileSync(join(root, file), `${JSON.stringify(json, null, 2)}\n`);
  writeFileSync(join(root, 'CHANGELOG.md'), changelog);
  return { root, files };
}

test('release preparation synchronizes UI, lockfile and Windows versions without changing dependencies', t => {
  // No README exists: preparing a release must never depend on it.
  const { root, files } = projectFixture(t, '## [0.2.2]\n- Новый релиз\n## [0.2.1]\n- Old\n');
  const notesPath = join(root, 'notes.md');
  assert.equal(prepareRelease(root, 'v0.2.2', notesPath), '0.2.2');
  const read = file => JSON.parse(readFileSync(join(root, file), 'utf8'));
  assert.deepEqual(read('wails.json'), { ...files['wails.json'], info: { productVersion: '0.2.2', productName: 'ScanSSH-ray' } });
  assert.deepEqual(read('frontend/package.json'), { ...files['frontend/package.json'], version: '0.2.2' });
  const expectedLock = structuredClone(files['frontend/package-lock.json']);
  expectedLock.version = '0.2.2';
  expectedLock.packages[''].version = '0.2.2';
  assert.deepEqual(read('frontend/package-lock.json'), expectedLock);
  const description = readFileSync(notesPath, 'utf8');
  assert.ok(description.startsWith('# ScanSSH-ray v0.2.2 🚀\n'));
  assert.ok(description.includes('\n- Новый релиз\n'));
  assert.ok(description.includes('### 📥 Скачать\n'));
  assert.ok(description.includes('**Платформа:** Windows 10 (x64).'));
  assert.ok(description.includes('`ScanSSH-ray.exe`'));
  assert.ok(description.includes('`.env.example`'));
  assert.ok(!description.includes('Old'));
  assert.ok(!description.includes('0.2.1'));
});

test('invalid changelog leaves version files unchanged', t => {
  const { root, files } = projectFixture(t, '## [0.2.1]\n- Old');
  const originals = Object.keys(files).map(file => readFileSync(join(root, file), 'utf8'));
  assert.throws(() => prepareRelease(root, 'v0.2.2', join(root, 'notes.md')), /no section/);
  assert.deepEqual(Object.keys(files).map(file => readFileSync(join(root, file), 'utf8')), originals);
});
