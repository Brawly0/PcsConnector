'use strict';
// Text clipboard access via OS tools (no native modules).
//   Windows: PowerShell   Linux/X11: xclip   Linux/Wayland: wl-clipboard
const { spawn } = require('child_process');

const PS = ['-NoProfile', '-NonInteractive', '-Command'];
const UTF8 = '$u=New-Object Text.UTF8Encoding $false;';

function run(cmd, args, input, waitFor) {
  return new Promise((resolve, reject) => {
    // For writers (xclip -i / wl-copy) the process stays alive to own the selection,
    // so don't wait for its stdio to close - resolve on 'exit'.
    const p = spawn(cmd, args, { stdio: ['pipe', waitFor === 'exit' ? 'ignore' : 'pipe', 'ignore'], windowsHide: true });
    const out = [];
    if (p.stdout) p.stdout.on('data', (d) => out.push(d));
    p.on('error', reject);
    p.on(waitFor === 'exit' ? 'exit' : 'close', (code) =>
      code === 0 ? resolve(Buffer.concat(out).toString('utf8').replace(/^﻿/, '')) : reject(new Error(`${cmd} exit ${code}`))
    );
    p.stdin.on('error', () => {});
    p.stdin.end(input || '');
  });
}

function backend() {
  if (process.platform === 'win32') {
    return {
      read: () => run('powershell', [...PS, `${UTF8}[Console]::OutputEncoding=$u;[Console]::Out.Write((Get-Clipboard -Raw))`]),
      write: (t) => run('powershell', [...PS, `${UTF8}[Console]::InputEncoding=$u;Set-Clipboard -Value ([Console]::In.ReadToEnd())`], t),
    };
  }
  if (process.env.WAYLAND_DISPLAY) {
    return {
      read: () => run('wl-paste', ['-n']).catch(() => ''), // exits 1 when empty
      write: (t) => run('wl-copy', [], t, 'exit'),
    };
  }
  return {
    read: () => run('xclip', ['-selection', 'clipboard', '-o']).catch(() => ''),
    write: (t) => run('xclip', ['-selection', 'clipboard', '-i'], t, 'exit'),
  };
}

// Calls onLocalChange(text) when the local clipboard changes; returns {apply, stop}.
function watch(onLocalChange, intervalMs = 700) {
  const b = backend();
  let last = null;
  let busy = false;
  const timer = setInterval(async () => {
    if (busy) return;
    busy = true;
    try {
      const text = await b.read();
      if (last === null) last = text; // don't push whatever was there before connecting
      else if (text !== last) {
        last = text;
        if (text) onLocalChange(text);
      }
    } catch {
      /* tool missing or transient failure - try again next tick */
    }
    busy = false;
  }, intervalMs);
  return {
    async apply(text) {
      last = text; // prevents echoing it straight back
      await b.write(text);
    },
    stop: () => clearInterval(timer),
  };
}

module.exports = { watch };
