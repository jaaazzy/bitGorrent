# bitGorrent

A Windows-first desktop torrent client built with Go, Wails v2, React, and TypeScript. It combines a custom BitTorrent engine with a compact transfer table, system light/dark themes, and controls for verifying, pausing, and resuming downloads.

Transfers run through a persistent FIFO queue, with **two download slots and one seeding slot** by default. The limits are configurable, and queued transfers can be reordered.

## Features

- BitTorrent v1 `.torrent` files and magnet links with verified metadata exchange.
- Single-file and multi-file downloads, file selection, and per-file verified progress.
- Disk-backed piece storage with SHA-1 verification before writing.
- Pause/resume, saved job recovery, and verification of existing data after restart.
- HTTP/HTTPS and UDP tracker support with fallback, plus IPv4 DHT lookup and announcement for public torrents.
- Uploading and seeding, configurable seed ratio, and per-transfer upload/download limits.
- Transfer filters, search, resizable columns, keyboard shortcuts, and file/tracker details.
- Removal that preserves downloaded files and resume data.
- Multiple managed transfers, duplicate detection by info hash, and protection against overlapping output paths.
- Independent download/seeding slots, queued transfer ordering, and aggregate speed reporting.

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
3. Set optional speed limits and a seed ratio, then choose **Add to queue**.
4. Select a transfer row and use **Pause**, **Resume**, **Verify files**, or **Remove** for that transfer.

A speed limit of `0` means unlimited. A seed ratio of `0` stops after downloading; the default `1.0` seeds until uploaded bytes equal the selected download size. Limits and the seed target are configured when adding a transfer.

After restart, all saved transfers are restored paused. Resume rehashes existing pieces before requesting missing data. Incomplete pieces are downloaded again; verified pieces are retained. Removing a transfer first stops its worker and keeps its files and checkpoints, so adding the same torrent into the same destination can resume it later.

The client rejects collisions with existing files it does not own. Checkpoints and bytes needed for pieces crossing skipped files are stored in `.bitgorrent/<infohash>` under the destination. Keep that directory while you need resume support.

### Queue controls

- Open **Queue limits** in the sidebar to set independent download and seed slots. A limit of `0` holds new starts for that category. Lowering a limit does not cancel workers already running.
- Checking existing files and waiting for peers occupy download slots. A completed download releases its download slot before waiting for a separate seed slot.
- Select a queued transfer to move it up or down. Pausing a queued transfer prevents it from starting; resuming places it at the back of the queue.
- Use the arrow keys to change row selection. Search and navigation filters preserve per-transfer controls, and the status bar totals speeds across all jobs.

Magnet metadata is resolved in the add dialog before file selection and queue admission.

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
- **Lifecycle manager:** schedules bounded workers, propagates cancellation, rejects stale worker updates, and waits for cleanup before reusing a job's resources. Saved jobs live in `%APPDATA%/bitGorrent/job.json` by default.
- **Metadata:** hashes the original encoded info dictionary and validates piece geometry, file paths, and file collisions.
- **Storage:** writes verified pieces directly to selected files and saves checkpoints through temporary-file replacement. Recovery verifies disk content rather than trusting the saved bitfield.
- **Networking:** uses bounded connections, message sizes, request pipelines, timeouts, and cancellation. In-flight download piece buffers are capped at 32 MiB per active download, with at most eight pieces; metadata, upload buffers, and runtime overhead are additional.

The engine has separate download, verification, and seed modes. A command mutex serializes durable queue transitions, while a separate state mutex protects short in-memory updates. Disk operations and worker waits happen outside the state lock. Worker generations prevent reports from old runs or removed jobs from changing current state.

Queue state is versioned and saved through a synced temporary file before replacement. Failed command saves preserve the existing queue and report an error; no new workers start while a save failure remains unresolved. Older single-transfer saves are read without modification and migrated on the next successful save. Corrupt or unsupported state is reported without overwriting the original file.

## Tests

```powershell
go test ./...
go vet ./...
go test -race ./...
cd frontend
npm run build
npm run test:e2e
```

Go tests use local trackers, DHT fixtures, and TCP peers to exercise downloads, metadata exchange, disk recovery, cancellation, protocol validation, and lifecycle handling. Queue tests cover FIFO ordering, separate slot limits, immediate pause/resume, active removal, stale updates, failed saves, migration, and shutdown. A local-swarm integration test runs two transfers while a third waits, then verifies that restarting and resuming does not download an already verified piece again. The suite does not depend on public swarms.

Race testing on Windows requires a compatible GCC toolchain and `CGO_ENABLED=1`. Playwright tests use installed Google Chrome and a test-only Wails bridge against the production frontend. They cover adding transfers, selection, queue ordering, limits, pause/resume, search, filtering, removal, event ordering, keyboard behavior, and visual captures at desktop and minimum window sizes.

An optional `npm run test:native` checks the packaged application through Windows UI Automation. It requires Node.js 22.6+ and an interactive Windows desktop. Native UI automation is separate from browser tests; a successful build alone does not validate native runtime behavior.

## Current limits

- Speed limits apply per transfer; shared global bandwidth limits are not implemented.
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

- Shared bandwidth limits across concurrent transfers.
- Watch folders, bandwidth schedules, and completion notifications.
- Tracker editing and peer exchange.

## Mascot attribution

BitGorrent's mascot is based on the Go Gopher designed by Renée French, licensed under [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/). Modified for BitGorrent.
