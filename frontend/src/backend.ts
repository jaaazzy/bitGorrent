import * as app from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { labels, type State, type Preview, type Snapshot } from './types';
import type { queue } from '../wailsjs/go/models';

function normalizeSnapshot(value: queue.Snapshot): Snapshot {
 if (!value.job) return { revision: value.revision, job: null };
 const job = value.job;
 if (!(job.progress.state in labels)) throw new Error('The engine returned an unknown transfer state.');
 return { revision: value.revision, job: { ...job, selected: job.selected ?? [], files: job.files ?? [], trackers: job.trackers ?? [], progress: { ...job.progress, state: job.progress.state as State, fileCompleted: job.progress.fileCompleted ?? [] } } };
}

export const desktop = () => 'go' in window;
function requireDesktop(): void { if (!desktop()) throw new Error('Open the desktop app to access files and download torrents.'); }
export const backend = {
 async snapshot(): Promise<Snapshot> { requireDesktop(); return normalizeSnapshot(await app.GetSnapshot()); },
 async inspect(source: string): Promise<Preview> { requireDesktop(); return app.InspectTorrent(source); },
 async pickTorrent() { requireDesktop(); return app.SelectTorrentFile(); },
 async pickFolder() { requireDesktop(); return app.SelectFolder(); },
 async add(p: Preview, destination: string, selected: number[], ratio: number, dl: number, ul: number) {
  requireDesktop(); return app.AddTorrent(p.id, destination, selected, ratio, dl, ul);
 },
 async pause() { requireDesktop(); return app.PauseTorrent(); },
 async resume() { requireDesktop(); return app.ResumeTorrent(); },
 async remove() { requireDesktop(); return app.RemoveTorrent(); },
 async recheck() { requireDesktop(); return app.RecheckTorrent(); },
 async reveal() { requireDesktop(); return app.RevealDestination(); },
 cancelMetadata() { if (desktop()) void app.CancelMetadata(); },
 onSnapshot(callback: (s: Snapshot) => void) { return desktop() ? EventsOn('transfers:changed', (value: queue.Snapshot) => callback(normalizeSnapshot(value))) : () => {}; },
 onMetadata(callback: (s: string) => void) { return desktop() ? EventsOn('metadata:status', callback) : () => {}; },
};
