import assert from 'node:assert/strict';
import { spawn, execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdirSync, mkdtempSync, realpathSync, writeFileSync } from 'node:fs';
import { win32 } from 'node:path';
import { fileURLToPath } from 'node:url';

const executable = realpathSync(process.argv[2]);
const tools = fileURLToPath(new URL('../../.tools/', import.meta.url));
mkdirSync(tools, { recursive: true });
const runDirectory = mkdtempSync(win32.join(tools, 'tray-smoke-'));
const identity = createHash('sha256').update('{96B5A9E0-7F0D-4A67-BAC4-A7D887F23051}\0' + win32.normalize(executable).toLowerCase()).digest();
identity[6] = identity[6] & 15 | 128;
identity[8] = identity[8] & 63 | 128;
const hex = identity.subarray(0, 16).toString('hex');
const guid = `${hex.slice(0,8)}-${hex.slice(8,12)}-${hex.slice(12,16)}-${hex.slice(16,20)}-${hex.slice(20)}`;
const children = [];
const checks = [];
let completed = false;
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
const alive = child => child.exitCode === null && child.signalCode === null;
function launch() {
    const child = spawn(executable, [], {
        windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'],
        // This path never exists: no real configuration, keys, scans or tunnels.
        env: { ...process.env, SCANSSH_ENV: win32.join(runDirectory, 'missing.env') },
    });
    child.logs = '';
    child.stdout.on('data', data => child.logs += data);
    child.stderr.on('data', data => child.logs += data);
    child.on('error', error => child.logs += error.message);
    children.push(child);
    return child;
}
function state(child, action = 'snapshot') {
    return JSON.parse(execFileSync('powershell.exe', [
        '-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File',
        fileURLToPath(new URL('./native-window.ps1', import.meta.url)),
        '-AppProcessId', String(child.pid), '-IconGuid', guid, '-Action', action,
    ], { windowsHide: true, encoding: 'utf8', timeout: 6000, stdio: ['ignore', 'pipe', 'pipe'] }).trim());
}
async function waitFor(child, predicate, description) {
    const deadline = Date.now() + 10000;
    let lastError;
    while (Date.now() < deadline) {
        assert.ok(alive(child), `Process exited while waiting for ${description}: ${child.logs}`);
        try { const current = state(child); if (predicate(current)) return current; } catch (error) { lastError = error.message; }
        await sleep(75);
    }
    throw new Error(`Timed out waiting for ${description}. ${lastError || ''} ${child.logs}`);
}
async function waitExited(child) {
    const deadline = Date.now() + 5000;
    while (alive(child) && Date.now() < deadline) await sleep(50);
    assert.ok(!alive(child), 'Process did not exit');
}
function check(name, value) {
    assert.ok(value, name);
    checks.push(name);
    console.log('PASS ' + name);
}
async function show(child, action = 'select') {
    state(child, action);
    await waitFor(child, current => current.visible && current.registered, 'visible window and registered icon');
}
async function hide(child, action = 'close') {
    state(child, action);
    await waitFor(child, current => !current.visible && current.registered, 'hidden window and registered icon');
}
try {
    const first = launch();
    await waitFor(first, current => current.tray !== 0 && current.registered, 'notification icon registration');
    check('Windows shell retains the packaged notification icon', true);
    await show(first, 'open');
    await hide(first);
    check('Close hides the window and preserves the process and icon', alive(first));
    const duplicate = launch();
    await waitExited(duplicate);
    check('Second launch exits cleanly', duplicate.exitCode === 0);
    await waitFor(first, current => current.visible && current.registered, 'restoration by the second launch');
    check('Second launch restores the existing instance', true);
    await hide(first, 'minimise');
    await show(first);
    check('Native minimise and version-4 activation hide and restore', true);
    first.kill();
    await waitExited(first);
    check('Forced termination is detected including a signal exit', !alive(first));
    const restarted = launch();
    await waitFor(restarted, current => current.tray !== 0 && current.registered, 'icon registration after forced termination');
    check('Restart registers the same icon GUID under the new process', restarted.pid !== first.pid);
    for (let cycle = 1; cycle <= 4; cycle++) {
        await hide(restarted);
        await show(restarted);
        check(`Close and notification activation cycle ${cycle}`, true);
    }
    state(restarted, 'exit');
    await waitExited(restarted);
    check('Tray Exit shuts down cleanly', restarted.exitCode === 0);
    completed = true;
    console.log(`Passed ${checks.length} packaged EXE checks.`);
} finally {
    for (const child of children) {
        if (alive(child)) {
            try { state(child, 'exit'); await waitExited(child); } catch { /* Only our own child may need forced cleanup. */ }
            if (alive(child)) { child.kill(); await waitExited(child); }
        }
    }
    writeFileSync(win32.join(runDirectory, 'results.json'), JSON.stringify({ executable, guid, completed, checks }, null, 2));
}
