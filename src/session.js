'use strict';
const os = require('os');
const { EventEmitter } = require('events');
const { TYPE, encodeFrame, FrameReader } = require('./protocol');

// One authenticated, encrypted connection to the peer.
// Events: 'message' (obj), 'chunk' (Buffer), 'close'
class Session extends EventEmitter {
  constructor(socket, key) {
    super();
    this.socket = socket;
    this.key = key;
    this.closed = false;
    socket.setNoDelay(true);
    socket.setKeepAlive(true, 10000);
    const reader = new FrameReader(
      key,
      (type, payload) => {
        if (type === TYPE.JSON) this.emit('message', JSON.parse(payload.toString('utf8')));
        else if (type === TYPE.CHUNK) this.emit('chunk', payload);
      },
      () => this.close() // wrong token or corrupt stream
    );
    socket.on('data', (d) => reader.push(d));
    socket.on('error', () => {});
    socket.on('close', () => this.close());
    this.send({ t: 'hello', v: 1, name: os.hostname(), platform: process.platform });
  }

  send(obj) {
    if (!this.closed) this.socket.write(encodeFrame(this.key, TYPE.JSON, Buffer.from(JSON.stringify(obj))));
  }

  // Resolves once the socket buffer has drained (backpressure for big files).
  sendChunk(buf) {
    if (this.closed) return Promise.reject(new Error('closed'));
    const ok = this.socket.write(encodeFrame(this.key, TYPE.CHUNK, buf));
    return ok ? Promise.resolve() : new Promise((r) => this.socket.once('drain', r));
  }

  close() {
    if (this.closed) return;
    this.closed = true;
    this.socket.destroy();
    this.emit('close');
  }
}

module.exports = { Session };
