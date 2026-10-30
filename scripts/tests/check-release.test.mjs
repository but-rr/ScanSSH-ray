import assert from 'node:assert/strict';
import test from 'node:test';
import { ensureReleaseTag, githubClient, inspectRelease, taggedCommit, verifyReleaseTag, verifySource } from '../check-release.mjs';

const commit = 'a'.repeat(40);
const oldCommit = 'b'.repeat(40);
const annotation = 'c'.repeat(40);
const tag = 'v0.3.0';

function fixture({ target = null, main = commit, release = null, annotated = false } = {}) {
  const calls = [];
  let ref = target ? { object: { type: annotated ? 'tag' : 'commit', sha: annotated ? annotation : target } } : null;
  const api = async (method, path, body) => {
    calls.push({ method, path, body });
    if (method === 'GET' && path === 'git/ref/heads/main') return { object: { type: 'commit', sha: main } };
    if (method === 'GET' && path === `git/ref/tags/${tag}`) return ref;
    if (method === 'GET' && path === `git/tags/${annotation}`) return { object: { type: 'commit', sha: target } };
    if (method === 'GET' && path === `releases/tags/${tag}`) return release;
    if (method === 'POST' && path === 'git/refs') {
      assert.equal(ref, null, 'an existing tag must never be overwritten');
      ref = { object: { type: 'commit', sha: body.sha } };
      return ref;
    }
    throw new Error(`Unexpected API call: ${method} ${path}`);
  };
  return { api, calls };
}

test('manual release checks latest main without creating a tag', async () => {
  const { api, calls } = fixture();
  await verifySource(api, { tag, commit, event: 'workflow_dispatch', ref: 'refs/heads/main' });
  assert.equal(await inspectRelease(api, tag, commit), null);
  assert.ok(calls.every(call => call.method === 'GET'));
});

test('an old workflow rerun or tag on an old main commit fails before any mutations', async () => {
  for (const event of ['workflow_dispatch', 'push']) {
    const { api, calls } = fixture({ main: commit, target: oldCommit });
    await assert.rejects(verifySource(api, { tag, commit: oldCommit, event, ref: event === 'push' ? `refs/tags/${tag}` : 'refs/heads/main' }), /Run workflow/);
    assert.ok(calls.every(call => call.method === 'GET'));
  }
});

test('dispatch from another branch and invalid version tags are rejected before API calls', async () => {
  const { api, calls } = fixture();
  await assert.rejects(verifySource(api, { tag, commit, event: 'workflow_dispatch', ref: 'refs/heads/feature' }), /веткой main/);
  await assert.rejects(inspectRelease(api, 'v.0.3.0', commit), /Release tag/);
  assert.equal(calls.length, 0);
});

test('an existing stale tag cannot be reused for a different commit', async () => {
  const { api, calls } = fixture({ target: oldCommit });
  await assert.rejects(ensureReleaseTag(api, tag, commit), /уже занята старым тегом/);
  assert.ok(calls.every(call => call.method === 'GET'));
});

test('a matching draft tag is reusable without creating or moving it', async () => {
  const { api, calls } = fixture({ target: commit, release: { draft: true } });
  await ensureReleaseTag(api, tag, commit);
  assert.ok(calls.every(call => call.method === 'GET'));
});

test('retry resumes an unpublished release at its original tag after main advances', async () => {
  for (const release of [null, { draft: true }]) {
    for (const event of ['workflow_dispatch', 'push']) {
      const { api, calls } = fixture({ main: oldCommit, target: commit, release });
      await verifySource(api, { tag, commit, event, ref: event === 'push' ? `refs/tags/${tag}` : 'refs/heads/main', runAttempt: 2 });
      await inspectRelease(api, tag, commit);
      await ensureReleaseTag(api, tag, commit);
      await verifyReleaseTag(api, tag, commit);
      assert.ok(calls.every(call => call.method === 'GET'));
    }
  }
});

test('retry cannot publish an old untagged build, move a tag or replace a published release', async () => {
  for (const options of [
    { main: oldCommit },
    { main: oldCommit, target: oldCommit, release: { draft: true } },
    { main: oldCommit, target: commit, release: { draft: false } },
  ]) {
    const { api, calls } = fixture(options);
    await assert.rejects(verifySource(api, { tag, commit, event: 'workflow_dispatch', ref: 'refs/heads/main', runAttempt: 2 }));
    assert.ok(calls.every(call => call.method === 'GET'));
  }
});

test('published versions are never replaced', async () => {
  const { api, calls } = fixture({ target: commit, release: { draft: false } });
  await assert.rejects(ensureReleaseTag(api, tag, commit), /уже опубликован/);
  assert.ok(calls.every(call => call.method === 'GET'));
});

test('post-build tag creation uses the exact tested commit and verifies the remote ref', async () => {
  const { api, calls } = fixture();
  await ensureReleaseTag(api, tag, commit);
  assert.deepEqual(calls.filter(call => call.method !== 'GET'), [{ method: 'POST', path: 'git/refs', body: { ref: `refs/tags/${tag}`, sha: commit } }]);
  assert.deepEqual(calls.at(-1), { method: 'GET', path: `git/ref/tags/${tag}`, body: undefined });
});

test('annotated tags are resolved to their underlying commit', async () => {
  const { api } = fixture({ target: commit, annotated: true });
  assert.equal(await taggedCommit(api, tag), commit);
  await verifyReleaseTag(api, tag, commit);
});

test('tag deletion or movement blocks publication', async () => {
  for (const target of [null, oldCommit]) {
    const { api } = fixture({ target });
    await assert.rejects(verifyReleaseTag(api, tag, commit), /Публикация остановлена/);
  }
});

test('GitHub transport handles a missing ref but does not hide authorization errors', async () => {
  const calls = [];
  let status = 404;
  const api = githubClient({ repository: 'owner/repo', token: 'test-token', fetchImpl: async (url, options) => {
    calls.push({ url, options });
    return { status, ok: false };
  } });
  assert.equal(await api('GET', 'git/ref/tags/v0.3.0'), null);
  assert.equal(calls[0].url, 'https://api.github.com/repos/owner/repo/git/ref/tags/v0.3.0');
  assert.equal(calls[0].options.headers.Authorization, 'Bearer test-token');
  status = 403;
  await assert.rejects(api('GET', 'git/ref/tags/v0.3.0'), /HTTP 403/);
  status = 404;
  await assert.rejects(api('POST', 'git/refs', { ref: 'refs/tags/v0.3.0', sha: commit }), /HTTP 404/);
});
