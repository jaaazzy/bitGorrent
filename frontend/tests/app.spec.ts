import { test, expect, type Page } from '@playwright/test';
import type { Job, Preview, Snapshot } from '../src/types';

const preview: Preview = { id: '0123456789012345678901234567890123456789', name: 'Debian 13.3.0 amd64 netinst.iso', length: 734003200, files: [{ path: 'debian-13.3.0-amd64-netinst.iso', length: 734003200, offset: 0 }], trackers: ['https://tracker.example.org/announce'], private: false };
const job: Job = { id: preview.id, name: preview.name, destination: 'C:\\Users\\example\\Downloads', files: preview.files, selected: [0], trackers: preview.trackers, seedRatio: 1, downloadLimit: 0, uploadLimit: 0, progress: { state: 'downloading', completed: 301989888, total: preview.length, downloaded: 301989888, uploaded: 15728640, downloadSpeed: 2621440, uploadSpeed: 65536, peers: 8, message: '', fileCompleted: [301989888] } };
async function mock(page: Page, initial: Job | null = null) {
 await page.addInitScript(({ initial, preview }) => {
  let snapshot: Snapshot = { revision: 0, job: initial };
  const listeners: Record<string, ((value: unknown) => void)[]> = {};
  function emit() { snapshot.revision++; for (const fn of listeners['transfers:changed'] ?? []) fn(structuredClone(snapshot)); }
  Object.assign(window, {
   runtime: { EventsOnMultiple(name: string, cb: (v: unknown) => void) { (listeners[name] ??= []).push(cb); return () => { listeners[name] = listeners[name].filter(f => f !== cb); }; } },
   go: { main: { App: {
    GetSnapshot: async () => structuredClone(snapshot),
    SelectTorrentFile: async () => 'C:\\fixtures\\debian.torrent', SelectFolder: async () => 'C:\\Downloads',
    InspectTorrent: async () => preview, CancelMetadata: async () => {},
    AddTorrent: async (_id: string, destination: string, selected: number[], seedRatio: number, downloadLimit: number, uploadLimit: number) => {
     snapshot.job = { id: preview.id, name: preview.name, destination, selected, files: preview.files, trackers: preview.trackers, seedRatio, downloadLimit, uploadLimit, progress: { state: 'checking', completed: 0, total: preview.length, downloaded: 0, uploaded: 0, downloadSpeed: 0, uploadSpeed: 0, peers: 0, message: 'Verifying existing pieces', fileCompleted: [0] } }; emit();
    },
    PauseTorrent: async () => { if (snapshot.job) { snapshot.job.progress.state = 'paused'; emit(); } },
    ResumeTorrent: async () => { if (snapshot.job) { snapshot.job.progress.state = 'downloading'; emit(); } },
    RecheckTorrent: async () => { if (snapshot.job) { snapshot.job.progress.state = 'checking'; emit(); } },
    RemoveTorrent: async () => { snapshot.job = null; emit(); }, RevealDestination: async () => {},
   } } },
   __testSnapshot(s: Snapshot) { snapshot = s; for (const fn of listeners['transfers:changed'] ?? []) fn(structuredClone(snapshot)); },
  });
 }, { initial, preview });
}
async function setSnapshot(page: Page, snapshot: Snapshot) {
 await page.evaluate(s => (window as unknown as { __testSnapshot: (s: Snapshot) => void }).__testSnapshot(s), snapshot);
}
test('empty state and complete add flow', async ({ page }) => {
 await mock(page); await page.goto('/');
 await expect(page.getByRole('heading', { name: 'Your next download starts here' })).toBeVisible();
 await page.getByRole('button', { name: 'Add torrent', exact: true }).click();
 const dialog = page.getByRole('dialog'); await expect(dialog.getByRole('heading', { name: preview.name })).toBeVisible();
 await expect(dialog.getByRole('button', { name: 'Start download' })).toBeDisabled();
 await dialog.getByRole('button', { name: 'Browse' }).click();
 await dialog.getByRole('button', { name: 'Start download' }).click();
 await expect(page.getByRole('dialog')).toHaveCount(0);
 await expect(page.getByRole('row', { name: /Debian.*Checking/ })).toBeVisible();
 await expect(page.getByRole('button', { name: 'Add torrent', exact: true })).toBeDisabled();
});
test('pause, resume, filters, search, and conservative removal', async ({ page }) => {
 await mock(page, job); await page.goto('/');
 await expect(page.getByRole('button', { name: 'Remove transfer', exact: true })).toBeDisabled();
 await page.getByRole('button', { name: 'Pause', exact: true }).click();
 await expect(page.getByRole('button', { name: 'Resume', exact: true })).toBeEnabled();
 await page.getByRole('button', { name: 'Downloading 0', exact: true }).click();
 await expect(page.getByRole('heading', { name: 'No matching transfers' })).toBeVisible();
 await page.getByRole('button', { name: 'Show all transfers' }).click();
 await page.keyboard.press('Control+f'); await expect(page.getByRole('textbox', { name: 'Search transfers' })).toBeFocused();
 await page.getByRole('textbox', { name: 'Search transfers' }).fill('missing');
 await expect(page.getByRole('heading', { name: 'No matching transfers' })).toBeVisible();
 await page.getByRole('button', { name: 'Show all transfers' }).click();
 await page.getByRole('button', { name: 'Resume', exact: true }).click();
 await expect(page.getByRole('button', { name: 'Pause', exact: true })).toBeEnabled();
 await page.getByRole('button', { name: 'Pause', exact: true }).click();
 await page.getByRole('button', { name: 'Remove transfer', exact: true }).click();
 await expect(page.getByRole('dialog')).toContainText('Downloaded files and verified pieces stay');
 await page.getByRole('dialog').getByRole('button', { name: 'Remove transfer', exact: true }).click();
 await expect(page.getByRole('heading', { name: 'Your next download starts here' })).toBeVisible();
});
test('events ignore stale snapshots and details use real progress', async ({ page }) => {
 await mock(page, job); await page.goto('/');
 await page.getByRole('tab', { name: 'Files' }).click();
 await expect(page.getByRole('tabpanel')).toContainText('288 MiB / 700 MiB');
 await page.getByRole('tab', { name: 'Files' }).press('ArrowRight');
 await expect(page.getByRole('tab', { name: 'Peers' })).toBeFocused();
 await expect(page.getByRole('tabpanel')).toContainText('8 connected download peers');
 await setSnapshot(page, { revision: 20, job: { ...job, progress: { ...job.progress, state: 'error', message: 'Disk is full. Choose another destination.' } } });
 await setSnapshot(page, { revision: 2, job });
 await expect(page.getByRole('button', { name: 'Retry', exact: true })).toBeVisible();
 await page.getByRole('tab', { name: 'Overview' }).click();
 await expect(page.getByRole('tabpanel')).toContainText('Disk is full');
});
test('magnet entry and dialog keyboard cancellation', async ({ page }) => {
 await mock(page); await page.goto('/');
 await page.getByRole('button', { name: 'Magnet link' }).click();
 await page.getByRole('textbox', { name: 'Magnet link' }).fill('magnet:?xt=urn:btih:0123456789012345678901234567890123456789');
 await page.getByRole('button', { name: 'Read metadata' }).click();
 await expect(page.getByRole('dialog').getByRole('heading', { name: preview.name })).toBeVisible();
 await page.keyboard.press('Escape'); await expect(page.getByRole('dialog')).toHaveCount(0);
});
test('browser preview explains the missing desktop bridge', async ({ page }) => {
 await page.goto('/'); await expect(page.getByText('Browser preview.', { exact: false })).toBeVisible();
 await page.getByRole('button', { name: 'Add torrent', exact: true }).click();
 await expect(page.getByRole('alert')).toContainText('Open the desktop app');
});
test('visual evidence at desktop, minimum window, dark theme, and add dialog', async ({ page }) => {
 const errors: string[] = []; page.on('pageerror', e => errors.push(e.message));
 await mock(page); await page.goto('/');
 await expect(page.getByRole('heading', { name: 'Your next download starts here' })).toBeVisible();
 await page.screenshot({ path: 'test-results/screenshots/desktop-empty.png', fullPage: true });
 await setSnapshot(page, { revision: 1, job });
 await page.screenshot({ path: 'test-results/screenshots/desktop.png', fullPage: true });
 await page.setViewportSize({ width: 760, height: 560 });
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
 await page.screenshot({ path: 'test-results/screenshots/minimum.png', fullPage: true });
 await page.emulateMedia({ colorScheme: 'dark' });
 await page.screenshot({ path: 'test-results/screenshots/dark.png', fullPage: true });
 await page.setViewportSize({ width: 1180, height: 760 });
 await setSnapshot(page, { revision: 2, job: null });
 await page.getByRole('button', { name: 'Add torrent', exact: true }).click();
 await expect(page.getByRole('dialog').getByRole('heading', { name: preview.name })).toBeVisible();
 await page.screenshot({ path: 'test-results/screenshots/add-dialog.png', fullPage: true });
 expect(errors).toEqual([]);
});
