import * as app from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { labels, type State, type Preview, type Snapshot } from './types';
import type { queue } from '../wailsjs/go/models';

function normalizeSnapshot(value: queue.Snapshot): Snapshot {
 const jobs = (value.jobs ?? []).map(job => {
  if (!(job.progress.state in labels)) throw new Error('The engine returned an unknown transfer state.');
  return { ...job, selected: job.selected ?? [], files: job.files ?? [], trackers: job.trackers ?? [], progress: { ...job.progress, state: job.progress.state as State, fileCompleted: job.progress.fileCompleted ?? [] } };
 });
 return { revision: value.revision, jobs, order: value.order ?? [], limits: value.limits, error: value.error ?? '' };
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
 async pause(id: string) { requireDesktop(); return app.PauseTorrent(id); },
 async resume(id: string) { requireDesktop(); return app.ResumeTorrent(id); },
 async remove(id: string) { requireDesktop(); return app.RemoveTorrent(id); },
 async recheck(id: string) { requireDesktop(); return app.RecheckTorrent(id); },
 async reveal(id: string) { requireDesktop(); return app.RevealDestination(id); },
 async limits(downloads: number, seeds: number) { requireDesktop(); return app.SetQueueLimits(downloads, seeds); },
 async move(id: string, direction: number) { requireDesktop(); return app.MoveTorrent(id, direction); },
 cancelMetadata() { if (desktop()) void app.CancelMetadata(); },
 onSnapshot(callback: (s: Snapshot) => void) { return desktop() ? EventsOn('transfers:changed', (value: queue.Snapshot) => callback(normalizeSnapshot(value))) : () => {}; },
 onMetadata(callback: (s: string) => void) { return desktop() ? EventsOn('metadata:status', callback) : () => {}; },
};
