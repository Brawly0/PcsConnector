'use strict';
// File transfer by folder: drop a file in Outbox -> it appears in the peer's Inbox.
const fs = require('fs');
const path = require('path');

const CHUNK = 256 * 1024;

function uniquePath(dir, name) {
  const ext = path.extname(name);
  const base = path.basename(name, ext);
  let p = path.join(dir, name);
  for (let i = 1; fs.existsSync(p); i++) p = path.join(dir, `${base} (${i})${ext}`);
  return p;
}

// Receiving side: feed it session messages/chunks.
class Receiver {
  constructor(inbox, log) {
    this.inbox = inbox;
    this.log = log;
    this.cur = null;
  }
  onMessage(m) {
    if (m.t === 'file-start') {
      this.abort();
      const name = path.basename(String(m.name)); // strip any path -> no traversal
      const dest = uniquePath(this.inbox, name);
      this.cur = { dest, fd: fs.openSync(dest, 'w'), got: 0, size: m.size };
    } else if (m.t === 'file-end' && this.cur) {
      fs.closeSync(this.cur.fd);
      this.log(`received ${path.basename(this.cur.dest)} (${this.cur.got} bytes)`);
      this.cur = null;
    }
  }
  onChunk(buf) {
    if (!this.cur) return;
    fs.writeSync(this.cur.fd, buf);
    this.cur.got += buf.length;
  }
  abort() {
    if (!this.cur) return;
    try { fs.closeSync(this.cur.fd); fs.unlinkSync(this.cur.dest); } catch {}
    this.cur = null;
  }
}

// Sending side: polls Outbox, sends settled files one at a time, moves them to Sent.
function watchOutbox(outbox, sentDir, getSession, log) {
  const seen = new Map(); // file -> size at previous poll (wait until stable)
  let sending = false;
  const timer = setInterval(async () => {
    const session = getSession();
    if (!session || sending) return;
    let names;
    try { names = fs.readdirSync(outbox); } catch { return; }
    for (const name of names) {
      const full = path.join(outbox, name);
      let st;
      try { st = fs.statSync(full); } catch { continue; }
      if (!st.isFile()) continue;
      if (seen.get(full) !== st.size) { seen.set(full, st.size); continue; }
      seen.delete(full);
      sending = true;
      try {
        session.send({ t: 'file-start', name, size: st.size });
        for await (const chunk of fs.createReadStream(full, { highWaterMark: CHUNK })) await session.sendChunk(chunk);
        session.send({ t: 'file-end' });
        fs.renameSync(full, uniquePath(sentDir, name));
        log(`sent ${name} (${st.size} bytes)`);
      } catch (e) {
        log(`send failed for ${name}: ${e.message}`);
      }
      sending = false;
      return;
    }
  }, 1000);
  return () => clearInterval(timer);
}

module.exports = { Receiver, watchOutbox };
