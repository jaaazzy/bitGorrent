# bitGorrent

A Windows-first desktop torrent client built with Go, Wails v2, React, and TypeScript. It combines a custom BitTorrent engine with a compact transfer table, system light/dark themes, and controls for verifying, pausing, and resuming downloads.

The current version manages **one torrent at a time**. Multi-job scheduling is planned.

## Features

- BitTorrent v1 `.torrent` files and magnet links with verified metadata exchange.
- Single-file and multi-file downloads, file selection, and per-file verified progress.
- Disk-backed piece storage with SHA-1 verification before writing.
- Pause/resume, saved job recovery, and verification of existing data after restart.
- HTTP/HTTPS and UDP tracker support with fallback, plus IPv4 DHT lookup and announcement for public torrents.
- Uploading and seeding, configurable seed ratio, and per-transfer upload/download limits.
- Transfer filters, search, resizable columns, keyboard shortcuts, and file/tracker details.
- Removal that preserves downloaded files and resume data.

## Requirements

- Windows with Microsoft WebView2 Runtime.
- Go 1.23 or newer.
- Node.js 22 or newer and npm.
- [Wails v2](https://v2.wails.io/docs/gettingstarted/installation/).

## Development

```powershell
git clone https://github.com/jaaazzy/bitGorrent.git
cd bitGorrent
cd frontend
npm ci
npm run build
cd ..
wails dev
```

The frontend can also run independently:

```powershell
cd frontend
npm run dev
```

Browser preview displays the interface; native file dialogs and torrent downloads require the Wails desktop host.

### Build

```powershell
wails build
```

The Windows executable is written to `build/bin/bitGorrent.exe`.

## Using the client

1. Choose **Add torrent** or paste a magnet link.
2. Review the metadata, select files, and choose an existing destination folder.
3. Set optional speed limits and a seed ratio, then start the download.
4. Use **Pause**, **Resume**, or **Verify files** to control the transfer.

A speed limit of `0` means unlimited. A seed ratio of `0` stops after downloading; the default `1.0` seeds until uploaded bytes equal the selected download size. Limits and the seed target are configured when adding a transfer.

After restart, the saved transfer is restored paused. Resume rehashes existing pieces before requesting missing data. Incomplete pieces are downloaded again; verified pieces are retained. Removing a transfer keeps its files and checkpoints, so adding the same torrent into the same destination can resume it later.

The client rejects collisions with existing files it does not own. Checkpoints and bytes needed for pieces crossing skipped files are stored in `.bitgorrent/<infohash>` under the destination. Keep that directory while you need resume support.

## Architecture

```text
React / TypeScript
        |
    Wails bridge
        |
  Job lifecycle manager
        |
 Custom BitTorrent engine
        |
 Trackers / DHT / TCP peers / verified disk storage
```

- **Frontend:** renders revisioned Go snapshots and sends commands through generated Wails bindings. Go owns transfer state.
- **Lifecycle manager:** runs the engine in a background worker, propagates cancellation, saves job descriptors, and waits for worker shutdown. Saved jobs live in `%APPDATA%/bitGorrent/job.json` by default.
- **Metadata:** hashes the original encoded info dictionary and validates piece geometry, file paths, and file collisions.
- **Storage:** writes verified pieces directly to selected files and saves checkpoints through temporary-file replacement. Recovery verifies disk content rather than trusting the saved bitfield.
- **Networking:** uses bounded connections, message sizes, request pipelines, timeouts, and cancellation. In-flight download piece buffers are capped at 32 MiB, with at most eight pieces; metadata, upload buffers, and runtime overhead are additional.

## Tests

```powershell
go test ./...
go vet ./...
go test -race ./...
cd frontend
npm run build
npm run test:e2e
```

Go tests use local trackers, DHT fixtures, and TCP peers to exercise downloads, metadata exchange, disk recovery, cancellation, protocol validation, and lifecycle handling. The suite does not depend on public swarms.

Race testing on Windows requires a compatible GCC toolchain and `CGO_ENABLED=1`. Playwright tests use installed Google Chrome and a test-only Wails bridge against the production frontend. They cover adding transfers, file selection, pause/resume, search, filtering, removal, event ordering, keyboard behavior, and visual captures.

An optional `npm run test:native` checks the packaged application through Windows UI Automation. It requires Node.js 22.6+ and an interactive Windows desktop. Native UI automation is separate from browser tests; a successful build alone does not validate native runtime behavior.

## Current limits

- One managed torrent; no concurrent queue yet.
- BitTorrent v1 over TCP. No v2, uTP, peer exchange, encryption, or compact IPv6 peer support.
- DHT performs bounded outbound lookup and announcement; it does not maintain a full public routing table. Known private torrents skip DHT.
- Incoming seeding uses port 6881, falling back to an available port when occupied. Automatic port forwarding and NAT traversal are not implemented.
- Windows is the primary platform; other desktop platforms have not been release-tested.
- Deterministic local tests cover correctness. Public-swarm interoperability and throughput at scale require additional testing.

## Troubleshooting

### OneDrive: "no Go source files"

OneDrive can mark readable Go files as cloud reparse points, causing Go's module scanner to skip them. From the project root:

```powershell
.\scripts\repair-onedrive.ps1
wails dev
```

The repair replaces affected project Go files with byte-identical ordinary local files, skips explicit symbolic links, and runs `go mod tidy`. If OneDrive marks them again, rerun it. A development checkout outside OneDrive avoids this recurring issue.

### Go cache access errors

If the default build cache is inaccessible, point Go at a writable directory for the current session:

```powershell
$env:GOCACHE = Join-Path $env:TEMP 'bitgorrent-go-cache'
```

## Roadmap

- Persistent multi-job queue, FIFO ordering, and separate download/seeding limits.
- Shared bandwidth limits across concurrent transfers.
- Watch folders, bandwidth schedules, and completion notifications.
- Tracker editing and peer exchange.
