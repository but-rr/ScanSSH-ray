import { appendFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { releaseVersion } from './release.mjs';

export function githubClient({ repository, token, apiUrl = 'https://api.github.com', fetchImpl = fetch }) {
  if (!/^[\w.-]+\/[\w.-]+$/.test(repository ?? '') || !token) {
    throw new Error('Нужны GITHUB_REPOSITORY и GH_TOKEN из окружения GitHub Actions.');
  }
  return async (method, path, body) => {
    const response = await fetchImpl(`${apiUrl}/repos/${repository}/${path}`, {
      method,
      headers: { Authorization: `Bearer ${token}`, Accept: 'application/vnd.github+json', 'Content-Type': 'application/json', 'X-GitHub-Api-Version': '2022-11-28' },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      signal: AbortSignal.timeout(30000),
    });
    if (response.status === 404 && method === 'GET') return null;
    if (!response.ok) throw new Error(`GitHub API ${method} ${path}: HTTP ${response.status}. Проверьте права workflow и доступность GitHub.`);
    return response.json();
  };
}

function validateCommit(commit) {
  if (!/^[0-9a-f]{40}$/i.test(commit ?? '')) throw new Error('Не удалось определить точный Git-коммит сборки.');
}

export async function taggedCommit(api, tag) {
  releaseVersion(tag);
  let object = (await api('GET', `git/ref/tags/${encodeURIComponent(tag)}`))?.object;
  if (!object) return null;
  for (let depth = 0; depth < 8; depth++) {
    validateCommit(object.sha);
    if (object.type === 'commit') return object.sha;
    if (object.type !== 'tag') break;
    object = (await api('GET', `git/tags/${object.sha}`))?.object;
    if (!object) break;
  }
  throw new Error(`Не удалось определить коммит тега ${tag}.`);
}

export async function verifySource(api, { tag, commit, event, ref }) {
  releaseVersion(tag);
  validateCommit(commit);
  if (event === 'workflow_dispatch') {
    if (ref !== 'refs/heads/main') throw new Error('Запускайте Windows Release через Run workflow с выбранной веткой main.');
  } else if (event !== 'push' || ref !== `refs/tags/${tag}`) {
    throw new Error('Неподдерживаемый источник запуска релиза.');
  }
  const main = await api('GET', 'git/ref/heads/main');
  if (main?.object?.sha !== commit) {
    throw new Error(`Выбран старый коммит ${commit}. Последний main: ${main?.object?.sha ?? 'не найден'}. Откройте Actions → Windows Release → Run workflow → main. Re-run старого запуска не обновляет исходники.`);
  }
}

export async function inspectRelease(api, tag, commit) {
  releaseVersion(tag);
  validateCommit(commit);
  const existing = await taggedCommit(api, tag);
  if (existing && existing !== commit) {
    throw new Error(`Тег ${tag} указывает на другой коммит: ${existing}; выбранный коммит: ${commit}. Эта версия уже занята старым тегом. Выберите новый номер версии либо отдельно исправьте ещё не опубликованный тег. Workflow не перемещает существующие теги.`);
  }
  const release = await api('GET', `releases/tags/${encodeURIComponent(tag)}`);
  if (release && !release.draft) throw new Error(`Release ${tag} уже опубликован. Для новых изменений используйте новую версию.`);
  return existing;
}

// Called by the workflow only after tests and the EXE build succeed. Creating
// a new ref is atomic; this script never updates or deletes an existing ref.
export async function ensureReleaseTag(api, tag, commit) {
  if (!(await inspectRelease(api, tag, commit))) {
    await api('POST', 'git/refs', { ref: `refs/tags/${tag}`, sha: commit });
  }
  await verifyReleaseTag(api, tag, commit);
}

export async function verifyReleaseTag(api, tag, commit) {
  validateCommit(commit);
  if ((await taggedCommit(api, tag)) !== commit) {
    throw new Error(`Тег ${tag} отсутствует или изменился. Публикация остановлена: тег должен указывать на проверенный коммит ${commit}.`);
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try {
    const mode = process.argv[2];
    const tag = process.env.RELEASE_TAG;
    releaseVersion(tag);
    const commit = execFileSync('git', ['rev-parse', 'HEAD'], { encoding: 'utf8' }).trim();
    if (commit !== process.env.GITHUB_SHA) throw new Error('Исходники не соответствуют коммиту запуска workflow.');
    const api = githubClient({ repository: process.env.GITHUB_REPOSITORY, token: process.env.GH_TOKEN, apiUrl: process.env.GITHUB_API_URL });
    if (mode === 'check') {
      await verifySource(api, { tag, commit, event: process.env.GITHUB_EVENT_NAME, ref: process.env.GITHUB_REF });
      await inspectRelease(api, tag, commit);
      if (process.env.GITHUB_STEP_SUMMARY) {
        appendFileSync(process.env.GITHUB_STEP_SUMMARY, `## Release ${tag}\n\nКоммит сборки: [${commit}](https://github.com/${process.env.GITHUB_REPOSITORY}/commit/${commit}).\n\nНовый тег будет создан после успешных тестов и сборки EXE.\n`);
      }
    } else if (mode === 'ensure-tag') {
      await ensureReleaseTag(api, tag, commit);
    } else if (mode === 'verify-tag') {
      await verifyReleaseTag(api, tag, commit);
    } else {
      throw new Error('Usage: node scripts/check-release.mjs check|ensure-tag|verify-tag');
    }
    console.log(`${mode}: ${tag} → ${commit}`);
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
