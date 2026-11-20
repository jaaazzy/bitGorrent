import { test, expect, type Page } from '@playwright/test';
import type { Job, Preview, Snapshot } from '../src/types';

const preview: Preview = { id: '0123456789012345678901234567890123456789', name: 'Debian 13.3.0 amd64 netinst.iso', length: 734003200, files: [{ path: 'debian-13.3.0-amd64-netinst.iso', length: 734003200, offset: 0 }], trackers: ['https://tracker.example.org/announce'], private: false };
const base: Job = { id: preview.id, name: preview.name, destination: 'C:\\Users\\example\\Downloads', files: preview.files, selected: [0], trackers: preview.trackers, seedRatio: 1, downloadLimit: 0, uploadLimit: 0, intent: 'download', private: false, progress: { state: 'downloading', completed: 301989888, total: preview.length, downloaded: 301989888, uploaded: 15728640, downloadSpeed: 2621440, uploadSpeed: 65536, peers: 8, message: '', fileCompleted: [301989888] } };
function transfer(id: string, name: string, state: Job['progress']['state'] = 'queued'): Job {
 return { ...base, id, name, progress: { ...base.progress, state, completed: state === 'completed' ? base.progress.total : base.progress.completed, fileCompleted: state === 'completed' ? [base.progress.total] : base.progress.fileCompleted, downloadSpeed: state === 'downloading' ? base.progress.downloadSpeed : 0, uploadSpeed: 0, peers: 0 } };
}
function snapshot(jobs: Job[], revision = 1): Snapshot { return { revision, jobs, order: jobs.map(job => job.id), limits: { downloads: 2, seeds: 1 }, error: '' }; }
async function mock(page: Page, initial: Job[] = [], candidate = preview) {
 await page.addInitScript(({ initial, candidate }) => {
  let current: Snapshot = { revision: 0, jobs: initial, order: initial.map(job => job.id), limits: { downloads: 2, seeds: 1 }, error: '' };
  const listeners: Record<string, ((value: unknown) => void)[]> = {};
  const get = (id: string) => { const job = current.jobs.find(job => job.id === id); if (!job) throw new Error('torrent not found'); return job; };
  function emit() { current.revision++; for (const fn of listeners['transfers:changed'] ?? []) fn(structuredClone(current)); }
  Object.assign(window, {
   runtime: { EventsOnMultiple(name: string, cb: (value: unknown) => void) { (listeners[name] ??= []).push(cb); return () => { listeners[name] = listeners[name].filter(fn => fn !== cb); }; } },
   go: { main: { App: {
    GetSnapshot: async () => structuredClone(current),
    SelectTorrentFile: async () => 'C:\\fixtures\\debian.torrent', SelectFolder: async () => 'C:\\Downloads',
    InspectTorrent: async () => candidate, CancelMetadata: async () => {},
    AddTorrent: async (id: string, destination: string, selected: number[], seedRatio: number, downloadLimit: number, uploadLimit: number) => {
     if (current.jobs.some(job => job.id === id)) throw new Error('this torrent is already in the transfer list');
     const active = current.jobs.filter(job => ['checking', 'downloading', 'waiting'].includes(job.progress.state)).length;
     current.jobs.push({ id, name: candidate.name, destination, selected, files: candidate.files, trackers: candidate.trackers, seedRatio, downloadLimit, uploadLimit, intent: 'download', private: false, progress: { state: active < current.limits.downloads ? 'checking' : 'queued', completed: 0, total: candidate.length, downloaded: 0, uploaded: 0, downloadSpeed: 0, uploadSpeed: 0, peers: 0, message: 'Verifying existing pieces', fileCompleted: [0] } }); current.order.push(id); emit();
    },
    PauseTorrent: async (id: string) => { const job = get(id); job.intent = 'paused'; job.progress = { ...job.progress, state: 'paused', downloadSpeed: 0, uploadSpeed: 0 }; emit(); },
    ResumeTorrent: async (id: string) => { const job = get(id); job.intent = 'download'; job.progress.state = 'downloading'; emit(); },
    RecheckTorrent: async (id: string) => { get(id).progress.state = 'checking'; emit(); },
    RemoveTorrent: async (id: string) => { get(id); current.jobs = current.jobs.filter(job => job.id !== id); current.order = current.order.filter(key => key !== id); emit(); },
    RevealDestination: async (id: string) => { get(id); },
    SetQueueLimits: async (downloads: number, seeds: number) => { current.limits = { downloads, seeds }; emit(); },
    MoveTorrent: async (id: string, direction: number) => {
     const ids = current.order.filter(key => get(key).progress.state === 'queued'); const index = ids.indexOf(id); const other = ids[index + direction]; if (!other) throw new Error('queue boundary');
     const a = current.order.indexOf(id), b = current.order.indexOf(other); [current.order[a], current.order[b]] = [current.order[b], current.order[a]]; emit();
    },
   } } },
   __testSnapshot(next: Snapshot) { current = next; for (const fn of listeners['transfers:changed'] ?? []) fn(structuredClone(current)); },
  });
 }, { initial, candidate });
}
async function setSnapshot(page: Page, value: Snapshot) {
 await page.evaluate(value => (window as unknown as { __testSnapshot: (snapshot: Snapshot) => void }).__testSnapshot(value), value);
}
test('empty state and complete add flow', async ({ page }) => {
 await mock(page); await page.goto('/');
 await expect(page.getByRole('heading', { name: 'Your next download starts here' })).toBeVisible();
 await page.getByRole('button', { name: 'Add torrent', exact: true }).click();
 const dialog = page.getByRole('dialog'); await expect(dialog.getByRole('heading', { name: preview.name })).toBeVisible();
 await expect(dialog.getByRole('button', { name: 'Add to queue' })).toBeDisabled();
 await dialog.getByRole('button', { name: 'Browse' }).click(); await dialog.getByRole('button', { name: 'Add to queue' }).click();
 await expect(page.getByRole('dialog')).toHaveCount(0);
 await expect(page.getByRole('row', { name: /Debian.*Checking/ })).toBeVisible();
 await expect(page.getByRole('button', { name: 'Add torrent', exact: true })).toBeEnabled();
});
test('multiple rows, selection, counts, filtering, and aggregate speeds', async ({ page }) => {
 const second = transfer('second', 'Fedora Workstation.iso', 'downloading');
 await mock(page, [base, second, transfer('third', 'Archive.zip')]); await page.goto('/');
 await expect(page.getByRole('button', { name: 'All transfers 3', exact: true })).toBeVisible();
 await expect(page.locator('.statusbar')).toContainText('5 MiB/s');
 await page.getByRole('row', { name: /Fedora.*Downloading/ }).click();
 await expect(page.getByRole('tabpanel').getByRole('heading', { name: second.name })).toBeVisible();
 await page.getByRole('button', { name: 'Pause', exact: true }).click();
 await expect(page.getByRole('row', { name: /Fedora.*Paused/ })).toBeVisible();
 await expect(page.getByRole('row', { name: /Debian.*Downloading/ })).toBeVisible();
 await page.getByRole('button', { name: 'Paused 1', exact: true }).click();
 await expect(page.getByRole('table', { name: 'Transfers' }).locator('tbody tr')).toHaveCount(1);
 await page.getByRole('button', { name: 'Resume', exact: true }).click();
 await expect(page.getByRole('heading', { name: 'No matching transfers' })).toBeVisible();
 await page.getByRole('button', { name: 'Show all transfers' }).click();
 await page.keyboard.press('Control+f'); await expect(page.getByRole('textbox', { name: 'Search transfers' })).toBeFocused();
 await page.getByRole('textbox', { name: 'Search transfers' }).fill('Archive');
 await expect(page.getByRole('table', { name: 'Transfers' }).locator('tbody tr')).toHaveCount(1);
});
test('queue reorder and configurable limits', async ({ page }) => {
 await mock(page, [base, transfer('b', 'B.zip'), transfer('c', 'C.zip')]); await page.goto('/');
 await page.getByRole('row', { name: 'C.zip, Queued', exact: true }).click();
 await page.getByRole('button', { name: 'Move up in queue' }).click();
 await expect.poll(() => page.getByRole('table', { name: 'Transfers' }).locator('tbody tr').evaluateAll(rows => rows.map(row => row.getAttribute('aria-label')))).toEqual(['Debian 13.3.0 amd64 netinst.iso, Downloading', 'C.zip, Queued', 'B.zip, Queued']);
 await expect(page.getByRole('button', { name: 'Move up in queue' })).toBeDisabled();
 await page.locator('.queue-settings summary').click();
 await page.getByRole('spinbutton', { name: 'Download slots' }).fill('0');
 await page.getByRole('spinbutton', { name: 'Seed slots' }).fill('2');
 await page.getByRole('button', { name: 'Apply limits' }).click();
 await expect(page.locator('.sidebar-note')).toContainText('0 download slots');
 await expect(page.getByRole('row', { name: /Debian.*Downloading/ })).toBeVisible();
});
test('remove an active selected job without changing another job', async ({ page }) => {
 await mock(page, [base, transfer('b', 'Keep.zip')]); await page.goto('/');
 await page.getByRole('button', { name: 'Remove transfer', exact: true }).click();
 await expect(page.getByRole('dialog')).toContainText('Active workers will stop first');
 await page.getByRole('dialog').getByRole('button', { name: 'Remove transfer', exact: true }).click();
 await expect(page.getByRole('row', { name: 'Keep.zip, Queued', exact: true })).toBeVisible();
 await expect(page.getByRole('table', { name: 'Transfers' }).locator('tbody tr')).toHaveCount(1);
});
test('duplicate hash errors preserve the list', async ({ page }) => {
 await mock(page, [base]); await page.goto('/');
 await page.getByRole('button', { name: 'Add torrent', exact: true }).click();
 await page.getByRole('button', { name: 'Browse' }).click(); await page.getByRole('button', { name: 'Add to queue' }).click();
 await expect(page.getByRole('dialog').getByRole('alert')).toContainText('already in the transfer list');
 await page.keyboard.press('Escape'); await expect(page.getByRole('table', { name: 'Transfers' }).locator('tbody tr')).toHaveCount(1);
});
test('stale snapshots, real file progress, row and tab keyboard navigation', async ({ page }) => {
 await mock(page, [base, transfer('b', 'Other.zip')]); await page.goto('/');
 await page.getByRole('row', { name: /Debian.*Downloading/ }).focus(); await page.keyboard.press('ArrowDown');
 await expect(page.getByRole('row', { name: 'Other.zip, Queued', exact: true })).toBeFocused();
 await page.keyboard.press('ArrowUp');
 await page.getByRole('tab', { name: 'Files' }).click(); await expect(page.getByRole('tabpanel')).toContainText('288 MiB / 700 MiB');
 await page.getByRole('tab', { name: 'Files' }).press('ArrowRight'); await expect(page.getByRole('tab', { name: 'Peers' })).toBeFocused();
 await expect(page.getByRole('tabpanel')).toContainText('8 connected download peers');
 await setSnapshot(page, snapshot([{ ...base, progress: { ...base.progress, state: 'error', message: 'Disk is full. Choose another destination.' } }], 20));
 await setSnapshot(page, snapshot([base], 2)); await expect(page.getByRole('button', { name: 'Retry', exact: true })).toBeVisible();
 await page.getByRole('tab', { name: 'Overview' }).click(); await expect(page.getByRole('tabpanel')).toContainText('Disk is full');
});
test('magnet entry, cancellation, and browser-only errors', async ({ page }) => {
 await mock(page); await page.goto('/'); await page.getByRole('button', { name: 'Magnet link' }).click();
 await page.getByRole('textbox', { name: 'Magnet link' }).fill('magnet:?xt=urn:btih:0123456789012345678901234567890123456789');
 await page.getByRole('button', { name: 'Read metadata' }).click(); await expect(page.getByRole('dialog').getByRole('heading', { name: preview.name })).toBeVisible();
 await page.keyboard.press('Escape'); await expect(page.getByRole('dialog')).toHaveCount(0);
});
test('browser preview explains the missing desktop bridge', async ({ page }) => {
 await page.goto('/'); await expect(page.getByText('Browser preview.', { exact: false })).toBeVisible();
 await page.getByRole('button', { name: 'Add torrent', exact: true }).click(); await expect(page.getByRole('alert')).toContainText('Open the desktop app');
});
test('visual evidence for multiple transfers, themes, settings, and long names', async ({ page }) => {
 const errors: string[] = []; page.on('pageerror', err => errors.push(err.message)); await mock(page); await page.goto('/');
 await page.screenshot({ path: 'test-results/screenshots/queue-empty.png', fullPage: true });
 const jobs = [base, transfer('b', 'Fedora-Workstation-Live-x86_64.iso'), { ...transfer('c', 'Research dataset archive with a very long filename.zip', 'completed'), intent: 'seed' }, transfer('d', 'Documents.tar', 'error')];
 await setSnapshot(page, snapshot(jobs)); await page.screenshot({ path: 'test-results/screenshots/queue-desktop.png', fullPage: true });
 await page.setViewportSize({ width: 760, height: 560 }); expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth && document.documentElement.scrollHeight <= window.innerHeight)).toBe(true);
 await page.screenshot({ path: 'test-results/screenshots/queue-minimum.png', fullPage: true });
 await page.emulateMedia({ colorScheme: 'dark' }); await page.screenshot({ path: 'test-results/screenshots/queue-dark.png', fullPage: true });
 await page.locator('.queue-settings summary').click(); expect(await page.evaluate(() => document.documentElement.scrollHeight <= window.innerHeight)).toBe(true); await page.screenshot({ path: 'test-results/screenshots/queue-settings.png', fullPage: true });
 await page.setViewportSize({ width: 1180, height: 760 }); await page.getByRole('button', { name: 'Add torrent', exact: true }).click();
 await expect(page.getByRole('dialog').getByRole('heading', { name: preview.name })).toBeVisible(); await page.screenshot({ path: 'test-results/screenshots/queue-add.png', fullPage: true });
 expect(errors).toEqual([]);
});
