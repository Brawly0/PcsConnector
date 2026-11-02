'use strict';
// Starts a host and a client on localhost with separate homes and checks a file crosses over.
const fs = require('fs');
const os = require('os');
const path = require('path');
const { spawn } = require('child_process');

const bin = path.join(__dirname, '..', 'bin', 'pcsc.js');
const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'pcsc-'));
const env = (n) => ({ ...process.env, PCSC_HOME: path.join(tmp, n), PCSC_TOKEN: 'test-token' });
const procs = [];
const start = (args, n) => procs.push(spawn(process.execPath, [bin, ...args, '--no-clipboard', '--port', '24899'], { env: env(n), stdio: 'ignore' }));

start(['host'], 'a');
setTimeout(() => start(['join', '127.0.0.1'], 'b'), 500);
setTimeout(() => {
  fs.mkdirSync(path.join(tmp, 'b', 'Outbox'), { recursive: true });
  fs.writeFileSync(path.join(tmp, 'b', 'Outbox', 'hello.bin'), Buffer.alloc(1_000_000, 7));
}, 1500);
setTimeout(() => {
  const got = path.join(tmp, 'a', 'Inbox', 'hello.bin');
  const ok = fs.existsSync(got) && fs.statSync(got).size === 1_000_000;
  procs.forEach((p) => p.kill());
  console.log(ok ? 'PASS: file transferred' : 'FAIL: file missing/size mismatch');
  process.exit(ok ? 0 : 1);
}, 6000);
