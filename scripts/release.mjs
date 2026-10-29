import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

export function releaseVersion(tag) {
  if (typeof tag !== 'string' || tag !== tag.trim() || !/^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(tag)) {
    throw new Error('Release tag must have the form vMAJOR.MINOR.PATCH, for example v0.2.2.');
  }
  const version = tag.slice(1);
  // Each component in Windows VERSIONINFO and the manifest is a 16-bit integer.
  if (version.split('.').some(part => Number(part) > 65535)) {
    throw new Error('Windows version components must be between 0 and 65535.');
  }
  return version;
}

export function releaseNotes(changelog, tag) {
  const version = releaseVersion(tag);
  const escapedVersion = version.replaceAll('.', '\\.');
  const heading = new RegExp(`^##[ \\t]+(?:\\[v?${escapedVersion}\\]|v?${escapedVersion})(?:[ \\t]+.*)?$`);
  const lines = changelog.replace(/^\uFEFF/, '').replaceAll('\r\n', '\n').split('\n');
  const sections = [];
  let current;
  let fence;

  for (const line of lines) {
    // A Markdown heading inside a fenced example must not end the version section.
    if (fence) {
      if (new RegExp(`^ {0,3}${fence.character}{${fence.length},}[ \\t]*$`).test(line)) fence = undefined;
    } else {
      const opening = /^ {0,3}(`{3,}|~{3,})/.exec(line);
      if (opening) {
        fence = { character: opening[1][0], length: opening[1].length };
      } else if (/^##(?:[ \t]|$)/.test(line)) {
        current = undefined;
        if (heading.test(line)) {
          current = [];
          sections.push(current);
        }
        continue;
      }
    }
    if (current) current.push(line);
  }

  if (sections.length === 0) throw new Error(`CHANGELOG.md has no section for ${tag}. Add ## [${version}].`);
  if (sections.length > 1) throw new Error(`CHANGELOG.md has duplicate sections for ${tag}.`);
  const notes = sections[0].join('\n').trim();
  if (!notes.replace(/<!--[\s\S]*?-->/g, '').trim()) {
    throw new Error(`CHANGELOG.md has no release notes for ${tag}.`);
  }
  return `${notes}\n`;
}

export function prepareRelease(projectRoot, tag, notesPath) {
  const version = releaseVersion(tag);
  // Validate notes and all JSON before changing any version files.
  const notes = releaseNotes(readFileSync(join(projectRoot, 'CHANGELOG.md'), 'utf8'), tag);
  const description = `# ScanSSH-ray ${tag} 🚀

Обновление ScanSSH-ray — настольного приложения для мониторинга SSH-узлов, проверки доступа в интернет через прокси и управления SSH-туннелями.

${notes.trimEnd()}

### 📥 Скачать

**Платформа:** Windows 10 (x64).

Скачайте \`ScanSSH-ray.exe\` из раздела **Assets** ниже и запустите приложение.

**Первый запуск:** скачайте \`.env.example\`, переименуйте в \`.env\` и заполните настройки рядом с EXE.

**Обновление:** замените EXE, сохранив свой существующий \`.env\`.
`;
  const files = ['wails.json', 'frontend/package.json', 'frontend/package-lock.json'];
  const documents = files.map(file => {
    const path = join(projectRoot, file);
    const source = readFileSync(path, 'utf8');
    return { path, source, json: JSON.parse(source) };
  });
  const [wails, frontend, lock] = documents.map(document => document.json);
  wails.info.productVersion = version;
  frontend.version = version;
  lock.version = version;
  lock.packages[''].version = version;

  for (const { path, source, json } of documents) {
    const indent = /\n([ \t]+)"/.exec(source)?.[1] ?? '  ';
    const newline = source.includes('\r\n') ? '\r\n' : '\n';
    writeFileSync(path, `${JSON.stringify(json, null, indent)}\n`.replaceAll('\n', newline), 'utf8');
  }
  writeFileSync(notesPath, description, 'utf8');
  return version;
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const projectRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
  try {
    const [, , tag, notesPath] = process.argv;
    if (!notesPath) throw new Error('Usage: node scripts/release.mjs v0.2.2 <release-notes-path>');
    const version = prepareRelease(projectRoot, tag, resolve(notesPath));
    console.log(`Prepared ${tag}: application version ${version}; notes from CHANGELOG.md only.`);
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
