import { createServer } from 'node:http';
import { execFile, spawn } from 'node:child_process';
import { createInterface } from 'node:readline';
import { createReadStream, existsSync } from 'node:fs';
import { mkdir, readFile, writeFile, rename } from 'node:fs/promises';
import { basename, dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';
import { randomUUID } from 'node:crypto';

const directory = dirname(fileURLToPath(import.meta.url));
const scripts = dirname(directory);
const systems = dirname(scripts);
const runtime = join(directory, '.dashboard');
const historyPath = join(runtime, 'history.json');
const tamperPath = join(scripts, '.tamper-state.json');
const datasetPath = join(systems, 'experiments', 'datasets', 'spark_stratified_100000.jsonl');
const host = '127.0.0.1';
const port = Number(process.env.PORT || 4170);
const sourceRecords = 100000;
const schemes = ['HMF-Audit', 'BLAS', 'DHT-backed', 'EVSEB'];
const selected = new Set(schemes);
const actions = {
  verify: 'verify.ps1', 'verify-blas': 'verify-blas.ps1', tamper: 'tamper.ps1', localize: 'localize.ps1',
  restore: 'tamper.ps1', ingest: 'ingest.ps1',
};
const files = {
  '/': ['index.html', 'text/html; charset=utf-8'],
  '/app.js': ['app.js', 'text/javascript; charset=utf-8'],
  '/style.css': ['style.css', 'text/css; charset=utf-8'],
};
const exec = promisify(execFile);
const subscribers = new Set();
let active = null;
let history = [];
let attributeOptions = null;
try {
  history = JSON.parse(await readFile(historyPath, 'utf8'));
} catch (error) {
  if (error.code !== 'ENOENT') console.error('Could not load run history:', error.message);
}

function json(response, status, value) {
  response.writeHead(status, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' });
  response.end(JSON.stringify(value));
}

async function recordedTarget() {
  try {
    const data = JSON.parse(await readFile(tamperPath, 'utf8'));
    return { id: data.LogId, count: data.LogIds.length, description: data.Description };
  } catch (error) {
    if (error.code === 'ENOENT') return null;
    return { error: 'The saved tamper case could not be read.' };
  }
}

async function snapshot() {
  return { active, history, target: await recordedTarget(), sourceRecords };
}

function broadcast(kind, data) {
  const message = 'event: ' + kind + '\ndata: ' + JSON.stringify(data) + '\n\n';
  for (const subscriber of subscribers) subscriber.write(message);
}

async function persistHistory() {
  await mkdir(runtime, { recursive: true });
  const temporary = join(runtime, 'history.tmp');
  await writeFile(temporary, JSON.stringify(history));
  await rename(temporary, historyPath);
}

function emit(run, event) {
  const timedEvent = { ...event, time: Date.now() };
  if (event.type === 'scheme') run.currentScheme = event.scheme;
  run.events.push(timedEvent);
  broadcast('progress', { id: run.id, event: timedEvent });
}

function launch(action, args, selection, query) {
  const run = { id: randomUUID(), action, selection, query, started: Date.now(), events: [] };
  active = run;
  broadcast('start', run);
  const child = spawn('powershell.exe', [
    '-NoLogo', '-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass',
    '-File', join(scripts, actions[action]), ...args,
  ], {
    cwd: scripts, windowsHide: true, env: { ...process.env, DEMO_EVENTS: '1' },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  Object.defineProperty(run, 'child', { value: child, enumerable: false });
  const output = createInterface({ input: child.stdout });
  const errors = createInterface({ input: child.stderr });
  const lastErrors = [];
  output.on('line', line => {
    if (!line.startsWith('DEMO_EVENT ')) return;
    try {
      emit(run, JSON.parse(line.slice(11)));
    } catch (error) {
      console.error('Invalid demo event:', error.message);
    }
  });
  errors.on('line', line => {
    lastErrors.push(line);
    if (lastErrors.length > 5) lastErrors.shift();
  });
  child.on('error', error => emit(run, { type: 'error', message: error.message }));
  child.on('close', async code => {
    if (run.cancelled) {
      emit(run, { type: 'cancelled', scheme: run.currentScheme, message: 'Verification stopped by user.' });
    } else if (code !== 0 && !(code === 1 && ['verify', 'verify-blas', 'localize'].includes(action)) &&
        !run.events.some(event => event.type === 'error' || event.status === 'ERROR')) {
      emit(run, { type: 'error', message: lastErrors.join(' ').trim() || 'The demo script stopped before completing.' });
    }
    run.ended = Date.now();
    run.code = code;
    history = [run, ...history].slice(0, 12);
    try {
      await persistHistory();
    } catch (error) {
      console.error('Could not save run history:', error.message);
    }
    active = null;
    broadcast('finish', { id: run.id, code, ended: run.ended, cancelled: Boolean(run.cancelled), target: await recordedTarget() });
  });
  return run;
}

function queryArgs(query) {
  const args = [];
  for (const [key, flag] of [['tenant', '-Tenant'], ['service', '-Service'], ['logType', '-LogType'], ['region', '-Region']]) {
    const value = query[key];
    if (key === 'tenant' && value === '') {
      args.push(flag, '*');
      continue;
    }
    if (typeof value !== 'string' || !/^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,79}$/.test(value)) {
      throw new Error('Enter a valid ' + key + '.');
    }
    args.push(flag, value);
  }
  const timestamp = /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)$/;
  for (const [key, flag] of [['start', '-Start'], ['end', '-End']]) {
    if (typeof query[key] !== 'string' || !timestamp.test(query[key]) || !Number.isFinite(Date.parse(query[key]))) {
      throw new Error('Enter both times in RFC 3339 format.');
    }
    args.push(flag, query[key]);
  }
  if (Date.parse(query.start) >= Date.parse(query.end)) throw new Error('Start must be before end.');
  return args;
}

async function readBody(request) {
  let body = '';
  for await (const chunk of request) {
    body += chunk;
    if (body.length > 16384) throw new Error('The request is too large.');
  }
  try {
    return JSON.parse(body);
  } catch {
    throw new Error('Expected a JSON request.');
  }
}

async function storageStatus() {
  const sql = `SELECT json_build_object(
    'HMF-Audit', json_build_object('records', (SELECT count(*) FROM hmfq.encrypted_logs)),
    'BLAS', (SELECT json_build_object('records', record_count, 'sealed', status = 'complete') FROM blas.live_runs WHERE run_id='demo100k'),
    'DHT-backed', json_build_object('records', (SELECT count(*) FROM dht.records), 'sealed', EXISTS (SELECT 1 FROM dht.dht_epoch_anchors WHERE epoch=1)),
    'EVSEB', json_build_object('records', (SELECT count(*) FROM evseb.encrypted_logs), 'root', (SELECT encode(root, 'hex') FROM evseb.stored_tree_state WHERE tree_id=1))
  );`;
  try {
    const { stdout } = await exec('docker', ['--context', 'default', 'exec', 'fair-postgres',
      'psql', '-X', '-q', '-t', '-A', '-v', 'ON_ERROR_STOP=1', '-U', 'fair', '-d', 'fair_benchmark', '-c', sql],
    { timeout: 5000, windowsHide: true });
    const storage = JSON.parse(stdout);
    const anchor = await readFile(join(systems, 'demo-script', '.work', 'evseb.anchor'), 'utf8').catch(() => '');
    storage.EVSEB.sealed = Boolean(anchor.trim()) && storage.EVSEB.root === anchor.trim();
    delete storage.EVSEB.root;
    return { online: true, storage, dataset: existsSync(datasetPath) };
  } catch {
    return { online: false, storage: {}, dataset: existsSync(datasetPath) };
  }
}

async function queryOptions() {
  if (attributeOptions) return attributeOptions;
  const fields = { tenant: 'tenant_id', service: 'service_id', logType: 'log_type', region: 'region_id' };
  const values = Object.fromEntries(Object.keys(fields).map(key => [key, new Set()]));
  const lines = createInterface({ input: createReadStream(datasetPath), crlfDelay: Infinity });
  for await (const line of lines) {
    if (!line) continue;
    try {
      const record = JSON.parse(line);
      for (const [key, field] of Object.entries(fields)) {
        if (typeof record[field] === 'string' && record[field]) values[key].add(record[field]);
      }
    } catch {
      // Ignore malformed source lines; options still come from valid real records.
    }
  }
  attributeOptions = Object.fromEntries(Object.entries(values).map(([key, set]) => [key, [...set].sort()]));
  return attributeOptions;
}

const server = createServer(async (request, response) => {
  const authority = request.headers.host;
  if (![host + ':' + port, 'localhost:' + port].includes(authority)) {
    return json(response, 403, { error: 'This dashboard is available on loopback.' });
  }
  const route = new URL(request.url, 'http://' + authority).pathname;
  try {
    if (request.method === 'GET' && Object.hasOwn(files, route)) {
      const [filename, contentType] = files[route];
      const asset = await readFile(join(directory, 'dist', basename(filename)));
      response.writeHead(200, { 'Content-Type': contentType, 'Cache-Control': 'no-store' });
      return response.end(asset);
    }
    if (request.method === 'GET' && route === '/api/options') {
      if (!existsSync(datasetPath)) return json(response, 503, { error: 'The Spark source dataset is unavailable.' });
      return json(response, 200, await queryOptions());
    }
    if (request.method === 'GET' && route === '/api/status') return json(response, 200, await storageStatus());
    if (request.method === 'GET' && route === '/api/events') {
      response.writeHead(200, {
        'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', Connection: 'keep-alive',
      });
      // Snapshot preparation can await disk, so subscribe and send it in one turn.
      const initial = await snapshot();
      subscribers.add(response);
      response.write('event: snapshot\ndata: ' + JSON.stringify(initial) + '\n\n');
      const heartbeat = setInterval(() => response.write(': heartbeat\n\n'), 15000);
      response.on('close', () => {
        clearInterval(heartbeat);
        subscribers.delete(response);
      });
      return;
    }
    if (request.method === 'POST' && route === '/api/run') {
      if (request.headers.origin !== 'http://' + authority || request.headers['content-type'] !== 'application/json') {
        return json(response, 403, { error: 'Submit actions from this dashboard.' });
      }
      const body = await readBody(request);
      const action = body?.action;
      if (!Object.hasOwn(actions, action)) throw new Error('Unknown action.');
      const target = await recordedTarget();
      if (active) return json(response, 409, { error: 'A demo action is already running.' });
      if (target && ['ingest', 'tamper'].includes(action)) throw new Error('Restore the active tamper case first.');
      if (!target && ['localize', 'restore'].includes(action)) throw new Error('Create a tamper case first.');
      const args = [];
      if (['verify', 'verify-blas', 'tamper'].includes(action)) args.push(...queryArgs(body.query || {}));
      if (['verify', 'verify-blas', 'localize'].includes(action)) {
        if (!Number.isInteger(body.repeats) || body.repeats < 1 || body.repeats > 20) throw new Error('Runs must be between 1 and 20.');
        args.push('-Repeats', String(body.repeats));
      }
      if (action === 'restore') args.push('-Restore');
      if (action === 'ingest') {
        if (!['all', ...schemes].includes(body.selection) || body.replace !== true) throw new Error('Confirm replacement of the selected demo data.');
        args.push('-Scheme', body.selection, '-Replace');
      }
      if (action === 'ingest' && body.selection !== 'all' && !selected.has(body.selection)) {
        throw new Error('Select a known scheme.');
      }
      const run = launch(action, args, body.selection, body.query);
      return json(response, 202, { id: run.id });
    }
    if (request.method === 'POST' && route === '/api/stop') {
      if (request.headers.origin !== 'http://' + authority || request.headers['content-type'] !== 'application/json') {
        return json(response, 403, { error: 'Submit actions from this dashboard.' });
      }
      const body = await readBody(request);
      if (!active || active.id !== body.id) return json(response, 409, { error: 'That verification is no longer running.' });
      if (!['verify', 'verify-blas', 'localize'].includes(active.action)) {
        return json(response, 409, { error: 'Stop is available while verification or localization is running.' });
      }
      if (active.cancelled) return json(response, 202, { id: active.id, stopping: true });
      active.cancelled = true;
      const child = active.child;
      if (!child?.pid) {
        active.cancelled = false;
        return json(response, 409, { error: 'The workflow process is not ready to stop yet.' });
      }
      const killer = spawn('taskkill.exe', ['/pid', String(child.pid), '/t', '/f'], { windowsHide: true, stdio: 'ignore' });
      killer.on('error', () => child.kill());
      killer.on('close', code => {
        if (code !== 0 && child.exitCode === null) child.kill();
      });
      return json(response, 202, { id: active.id, stopping: true });
    }
    return json(response, 404, { error: 'Route not found.' });
  } catch (error) {
    if (response.headersSent) return response.end();
    return json(response, 400, { error: error.message });
  }
});

server.listen(port, host, () => console.log('Local dashboard http://' + host + ':' + port));
