import { useEffect, useState } from 'react';
import { Icon } from './Icon';
import type { Limits } from './types';

export default function QueueSettings({ limits, busy, onSave }: { limits: Limits; busy: boolean; onSave: (downloads: number, seeds: number) => void }) {
 const [downloads, setDownloads] = useState(String(limits.downloads));
 const [seeds, setSeeds] = useState(String(limits.seeds));
 useEffect(() => { setDownloads(String(limits.downloads)); setSeeds(String(limits.seeds)); }, [limits.downloads, limits.seeds]);
 const downloadValue = Number(downloads), seedValue = Number(seeds);
 const valid = downloads.trim() !== '' && seeds.trim() !== '' && [downloadValue, seedValue].every(n => Number.isInteger(n) && n >= 0 && n <= 16);
 return <details className="queue-settings"><summary><Icon name="settings" size={16} /> Queue limits</summary>
  <form onSubmit={event => { event.preventDefault(); if (valid) onSave(downloadValue, seedValue); }}>
   <label>Download slots<input aria-label="Download slots" type="number" min="0" max="16" value={downloads} onChange={event => setDownloads(event.target.value)} /></label>
   <label>Seed slots<input aria-label="Seed slots" type="number" min="0" max="16" value={seeds} onChange={event => setSeeds(event.target.value)} /></label>
   <p>0 holds new starts. Running transfers keep their slots.</p>
   <button disabled={busy || !valid || (downloadValue === limits.downloads && seedValue === limits.seeds)} type="submit">Apply limits</button>
  </form>
 </details>;
}
