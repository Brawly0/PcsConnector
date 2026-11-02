'use strict';
const fs = require('fs');
const os = require('os');
const net = require('net');
const path = require('path');
const crypto = require('crypto');
const { execSync } = require('child_process');
const { deriveKey } = require('./protocol');
const { Session } = require('./session');
const clipboard = require('./clipboard');
const { Receiver, watchOutbox } = require('./files');

const HOME = process.env.PCSC_HOME || path.join(os.homedir(), 'PcsConnector');
const DIRS = { inbox: path.join(HOME, 'Inbox'), outbox: path.join(HOME, 'Outbox'), sent: path.join(HOME, 'Sent') };
const CONFIG = path.join(HOME, 'config.json');
const log = (m) => console.log(`[${new Date().toLocaleTimeString()}] ${m}`);

function parseArgs(argv) {
  const o = { _: [] };
  for (let i = 0; i < argv.length; i++) {
    if (argv[i].startsWith('--')) {
      const k = argv[i].slice(2);
      o[k] = argv[i + 1] && !argv[i + 1].startsWith('--') ? argv[++i] : true;
    } else o._.push(argv[i]);
  }
  return o;
}

function loadToken(opts) {
  if (opts.token && opts.token !== true) return opts.token;
  if (process.env.PCSC_TOKEN) return process.env.PCSC_TOKEN;
  try { return JSON.parse(fs.readFileSync(CONFIG, 'utf8')).token; } catch { return null; }
}

function needToken(opts) {
  const t = loadToken(opts);
  if (!t) { console.error('No token. Run `pcsc token` on one machine, then pass the same value with --token on the other.'); process.exit(1); }
  return t;
}

// Wire clipboard + file transfer onto a freshly connected session.
function attach(session, opts, state) {
  state.session = session;
  const receiver = new Receiver(DIRS.inbox, log);
  const clip = opts['no-clipboard'] ? null : clipboard.watch((text) => session.send({ t: 'clip', text }));
  session.on('message', (m) => {
    if (m.t === 'hello') log(`connected to ${m.name} (${m.platform})`);
    else if (m.t === 'clip' && clip) clip.apply(String(m.text)).catch((e) => log(`clipboard write failed: ${e.message}`));
    else receiver.onMessage(m);
  });
  session.on('chunk', (b) => receiver.onChunk(b));
  session.on('close', () => {
    if (state.session === session) state.session = null;
    receiver.abort();
    clip && clip.stop();
    log('disconnected');
  });
}

function setup(opts) {
  Object.values(DIRS).forEach((d) => fs.mkdirSync(d, { recursive: true }));
  const state = { session: null };
  watchOutbox(DIRS.outbox, DIRS.sent, () => state.session, log);
  log(`Inbox: ${DIRS.inbox}\n           Outbox: ${DIRS.outbox} (drop files here to send)`);
  return state;
}

const commands = {
  token() {
    fs.mkdirSync(HOME, { recursive: true });
    let token;
    try { token = JSON.parse(fs.readFileSync(CONFIG, 'utf8')).token; } catch {}
    if (!token) {
      token = crypto.randomBytes(18).toString('base64url');
      fs.writeFileSync(CONFIG, JSON.stringify({ token }, null, 2));
    }
    console.log(`Shared token (use the same on both machines):\n  ${token}`);
  },

  host(opts) {
    const key = deriveKey(needToken(opts));
    const port = Number(opts.port) || 24800;
    const state = setup(opts);
    net.createServer((socket) => {
      if (state.session) state.session.close(); // newest connection wins
      attach(new Session(socket, key), opts, state);
    }).listen(port, () => log(`hosting on port ${port} - waiting for the other machine...`));
  },

  join(opts) {
    const key = deriveKey(needToken(opts));
    const host = opts._[1];
    const port = Number(opts.port) || 24800;
    if (!host) { console.error('Usage: pcsc join <host-ip> [--token T]'); process.exit(1); }
    const state = setup(opts);
    const connect = () => {
      const socket = net.connect(port, host);
      socket.once('connect', () => attach(new Session(socket, key), opts, state));
      socket.once('error', () => {});
      socket.once('close', () => setTimeout(connect, 3000));
    };
    connect();
  },

  doctor() {
    const have = (c) => { try { execSync(process.platform === 'win32' ? `where ${c}` : `command -v ${c}`, { stdio: 'ignore' }); return true; } catch { return false; } };
    const need = process.platform === 'win32' ? ['powershell', 'ffmpeg'] : process.env.WAYLAND_DISPLAY ? ['wl-paste', 'wl-copy', 'ffplay'] : ['xclip', 'ffplay'];
    console.log(`platform: ${process.platform}  node: ${process.version}  session: ${process.env.WAYLAND_DISPLAY ? 'wayland' : process.env.DISPLAY ? 'x11' : 'n/a'}`);
    for (const c of need) console.log(`${have(c) ? 'ok     ' : 'MISSING'} ${c}`);
  },

  help() {
    console.log(`pcsc - PcsConnector
  pcsc token                  create/show the shared secret
  pcsc host [--port N]        run on the Windows PC (clipboard + file sync)
  pcsc join <ip> [--port N]   run on the Ubuntu laptop
  pcsc doctor                 check required tools
  options: --token T  --no-clipboard   env: PCSC_TOKEN, PCSC_HOME`);
  },
};

function main(argv) {
  const opts = parseArgs(argv);
  (commands[opts._[0]] || commands.help)(opts);
}

module.exports = { main };
