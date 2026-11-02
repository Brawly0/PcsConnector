'use strict';
// Encrypted framing. Wire frame: [u32 len][iv 12][tag 16][ciphertext]
// plaintext = [u8 type][payload]. The AES-256-GCM key is derived from the shared
// token, so a wrong token fails authentication on the very first frame.
const crypto = require('crypto');

const TYPE = { JSON: 0, CHUNK: 1 };
const MAX_FRAME = 4 * 1024 * 1024;

const deriveKey = (token) => crypto.scryptSync(token, 'pcsconnector-v1', 32);

function encodeFrame(key, type, payload) {
  const iv = crypto.randomBytes(12);
  const c = crypto.createCipheriv('aes-256-gcm', key, iv);
  const ct = Buffer.concat([c.update(Buffer.concat([Buffer.from([type]), payload])), c.final()]);
  const body = Buffer.concat([iv, c.getAuthTag(), ct]);
  const head = Buffer.alloc(4);
  head.writeUInt32BE(body.length);
  return Buffer.concat([head, body]);
}

class FrameReader {
  constructor(key, onFrame, onError) {
    this.key = key;
    this.onFrame = onFrame;
    this.onError = onError;
    this.buf = Buffer.alloc(0);
  }
  push(chunk) {
    this.buf = Buffer.concat([this.buf, chunk]);
    try {
      while (this.buf.length >= 4) {
        const len = this.buf.readUInt32BE(0);
        if (len < 29 || len > MAX_FRAME) throw new Error('bad frame length');
        if (this.buf.length < 4 + len) return;
        const body = this.buf.subarray(4, 4 + len);
        this.buf = this.buf.subarray(4 + len);
        const d = crypto.createDecipheriv('aes-256-gcm', this.key, body.subarray(0, 12));
        d.setAuthTag(body.subarray(12, 28));
        const plain = Buffer.concat([d.update(body.subarray(28)), d.final()]);
        this.onFrame(plain[0], plain.subarray(1));
      }
    } catch (e) {
      this.onError(e);
    }
  }
}

module.exports = { TYPE, deriveKey, encodeFrame, FrameReader };
