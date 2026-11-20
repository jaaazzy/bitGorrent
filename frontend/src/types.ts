export type State = 'queued' | 'paused' | 'checking' | 'downloading' | 'waiting' | 'pausing' | 'removing' | 'seeding' | 'completed' | 'error';
export interface TorrentFile { path: string; length: number; offset: number }
export interface Progress {
 state: State; completed: number; total: number; downloaded: number; uploaded: number;
 downloadSpeed: number; uploadSpeed: number; peers: number; message: string; fileCompleted: number[];
}
export interface Job {
 id: string; name: string; destination: string; selected: number[]; files: TorrentFile[]; trackers: string[];
 seedRatio: number; downloadLimit: number; uploadLimit: number; progress: Progress;
 intent: string; private: boolean;
}
export interface Limits { downloads: number; seeds: number }
export interface Snapshot { revision: number; jobs: Job[]; order: string[]; limits: Limits; error: string }
export interface Preview { id: string; name: string; length: number; files: TorrentFile[]; trackers: string[]; private: boolean }
export const labels: Record<State, string> = { queued: 'Queued', paused: 'Paused', checking: 'Checking', downloading: 'Downloading', waiting: 'Waiting for peers', pausing: 'Pausing', removing: 'Removing', seeding: 'Seeding', completed: 'Completed', error: 'Needs attention' };
export function percent(job: Job): number { return job.progress.total ? Math.min(100, job.progress.completed / job.progress.total * 100) : job.progress.state === 'completed' ? 100 : 0; }
export function isSeeding(job: Job): boolean { return job.progress.state === 'seeding' || (job.intent === 'seed' && job.progress.state === 'checking'); }
export function bytes(n: number): string {
 if (!Number.isFinite(n) || n <= 0) return '0 B';
 const unit = Math.min(Math.floor(Math.log(n) / Math.log(1024)), 4);
 return `${(n / 1024 ** unit).toLocaleString(undefined, { maximumFractionDigits: unit ? 1 : 0 })} ${['B', 'KiB', 'MiB', 'GiB', 'TiB'][unit]}`;
}
export function eta(p: Progress): string {
 if (p.downloadSpeed <= 0 || !['downloading', 'waiting'].includes(p.state)) return '—';
 const seconds = Math.max(0, Math.ceil((p.total - p.completed) / p.downloadSpeed));
 if (seconds < 60) return `${seconds}s`;
 if (seconds < 3600) return `${Math.ceil(seconds / 60)}m`;
 return `${Math.floor(seconds / 3600)}h ${Math.ceil(seconds % 3600 / 60)}m`;
}
export function errorText(e: unknown): string { return e instanceof Error ? e.message : String(e); }
