# PcsConnector

Use an Ubuntu laptop as an extended screen + shared input/clipboard/files for a Windows PC.

- What it is: zero-dependency Node.js (>=18) tool, `pcsc` CLI. See README.md and docs/ARCHITECTURE.md.
- How to run it: `node bin/pcsc.js host` (Windows) / `node bin/pcsc.js join <ip> --token T` (Ubuntu). Test: `npm test`.
- Conventions: no npm dependencies; OS access via system tools (PowerShell, xclip, wl-clipboard, ffmpeg). Env `PCSC_HOME` overrides the data dir (used by tests).
