import { useEffect, useRef, useState } from 'react';
import AddTorrent from './AddTorrent';
import QueueSettings from './QueueSettings';
import TransferTable from './TransferTable';
import TransferDetails from './TransferDetails';
import { backend, desktop } from './backend';
import { Icon, type IconName } from './Icon';
import { bytes, errorText, isSeeding, type Job, type Snapshot } from './types';

const filters: { name: string; icon: IconName; match: (job: Job) => boolean }[] = [
 { name: 'All transfers', icon: 'all', match: () => true },
 { name: 'Queued', icon: 'all', match: job => job.progress.state === 'queued' },
 { name: 'Downloading', icon: 'download', match: job => !isSeeding(job) && ['downloading', 'checking', 'waiting', 'pausing'].includes(job.progress.state) },
 { name: 'Paused', icon: 'pause', match: job => job.progress.state === 'paused' },
 { name: 'Seeding', icon: 'upload', match: isSeeding },
 { name: 'Completed', icon: 'check', match: job => job.progress.state === 'completed' },
 { name: 'Needs attention', icon: 'error', match: job => job.progress.state === 'error' },
];
const running = (job: Job) => ['checking', 'downloading', 'waiting', 'seeding', 'pausing'].includes(job.progress.state);

export default function App() {
 const [snapshot, setSnapshot] = useState<Snapshot>({ revision: 0, jobs: [], order: [], limits: { downloads: 2, seeds: 1 }, error: '' });
 const [selectedId, setSelectedId] = useState<string>();
 const [filter, setFilter] = useState('All transfers');
 const [search, setSearch] = useState('');
 const [add, setAdd] = useState<{ source: string; magnet: boolean } | null>(null);
 const [error, setError] = useState('');
 const [busy, setBusy] = useState(false);
 const [removeId, setRemoveId] = useState<string | null>(null);
 const searchInput = useRef<HTMLInputElement>(null);
 const removeDialog = useRef<HTMLDialogElement>(null);
 const filterRule = filters.find(rule => rule.name === filter)!;
 const jobs = snapshot.order.map(id => snapshot.jobs.find(job => job.id === id)).filter((job): job is Job => !!job);
 const visible = jobs.filter(job => filterRule.match(job) && job.name.toLocaleLowerCase().includes(search.toLocaleLowerCase()));
 const job = visible.find(job => job.id === selectedId) ?? visible[0];
 const queued = jobs.filter(job => job.progress.state === 'queued');
 const queuePosition = queued.findIndex(item => item.id === job?.id);
 const state = job?.progress.state;
 const pausable = !!job && (running(job) || state === 'queued' || (state === 'completed' && job.intent === 'seed')) && state !== 'pausing';
 const activeCount = jobs.filter(running).length;
 const downloadSpeed = jobs.reduce((total, item) => total + item.progress.downloadSpeed, 0);
 const uploadSpeed = jobs.reduce((total, item) => total + item.progress.uploadSpeed, 0);
 const removing = jobs.find(item => item.id === removeId);

 useEffect(() => {
  let alive = true;
  const update = (next: Snapshot) => { if (alive) setSnapshot(old => next.revision >= old.revision ? next : old); };
  const stop = backend.onSnapshot(update);
  if (desktop()) void backend.snapshot().then(update).catch(err => { if (alive) setError(errorText(err)); });
  return () => { alive = false; stop(); };
 }, []);
 useEffect(() => { if (removeId) removeDialog.current?.showModal(); }, [removeId]);
 async function act(action: () => Promise<unknown>) {
  setError(''); setBusy(true);
  try { await action(); if (desktop()) { const next = await backend.snapshot(); setSnapshot(old => next.revision >= old.revision ? next : old); } }
  catch (err) { setError(errorText(err)); }
  finally { setBusy(false); }
 }
 async function pickTorrent() { await act(async () => { const source = await backend.pickTorrent(); if (source) setAdd({ source, magnet: false }); }); }
 useEffect(() => {
  function key(event: KeyboardEvent) {
   if (!(event.ctrlKey || event.metaKey) || add || removeId) return;
   if (event.key.toLowerCase() === 'f') { event.preventDefault(); searchInput.current?.focus(); }
   if (event.key.toLowerCase() === 'o') { event.preventDefault(); if (!busy) void pickTorrent(); }
  }
  window.addEventListener('keydown', key); return () => window.removeEventListener('keydown', key);
 });
 return <main className="app">
  <aside className="sidebar" aria-label="Transfer navigation">
   <div className="brand"><span className="brand-symbol"><Icon name="download" size={21} /></span><span>bitGorrent</span></div>
   <nav aria-label="Filter transfers">{filters.map(rule => <button key={rule.name} className={`nav-item ${filter === rule.name ? 'selected' : ''}`} aria-current={filter === rule.name ? 'page' : undefined} onClick={() => setFilter(rule.name)}>
    <Icon name={rule.icon} size={17} /><span>{rule.name}</span><span className="count">{jobs.filter(rule.match).length}</span>
   </button>)}</nav>
   <div className="sidebar-bottom"><QueueSettings limits={snapshot.limits} busy={busy} onSave={(downloads, seeds) => void act(() => backend.limits(downloads, seeds))} />
    <div className="sidebar-note"><Icon name="info" size={16} /><p>{snapshot.limits.downloads} download {snapshot.limits.downloads === 1 ? 'slot' : 'slots'}<br /><span>{snapshot.limits.seeds} seed {snapshot.limits.seeds === 1 ? 'slot' : 'slots'}</span></p></div>
   </div>
  </aside>
  <section className="workspace" aria-label="Torrent transfers">
   <header className="toolbar">
    <div className="toolbar-group"><button className="primary" disabled={busy} onClick={() => void pickTorrent()} title="Add torrent (Ctrl+O)"><Icon name="add" /> Add torrent</button>
     <button disabled={busy} onClick={() => setAdd({ source: '', magnet: true })}><Icon name="link" size={16} /><span>Magnet link</span></button></div>
    <div className="toolbar-group transfer-actions"><button disabled={!job || busy || state === 'removing'} onClick={() => job && void act(() => pausable ? backend.pause(job.id) : backend.resume(job.id))}>
     <Icon name={pausable ? 'pause' : 'play'} size={16} />{pausable ? 'Pause' : state === 'error' ? 'Retry' : 'Resume'}
    </button><button className="icon-button" title="Remove selected transfer" aria-label="Remove transfer" disabled={!job || state === 'removing' || busy} onClick={() => job && setRemoveId(job.id)}><Icon name="close" /></button></div>
    <label className="search"><Icon name="search" size={16} /><input ref={searchInput} aria-label="Search transfers" placeholder="Search transfers" value={search} onChange={event => setSearch(event.target.value)} /><kbd>Ctrl F</kbd></label>
   </header>
   {!desktop() && <div className="notice"><Icon name="info" size={16} />Browser preview. Open the Wails desktop app to download.</div>}
   {(error || snapshot.error) && <div className="notice error-notice" role="alert"><Icon name="error" size={16} /><span>{error || snapshot.error}</span>{error && <button className="icon-button" aria-label="Dismiss error" onClick={() => setError('')}><Icon name="close" size={15} /></button>}</div>}
   <section className="transfer-section" aria-labelledby="transfer-heading">
    <div className="section-heading"><div><h1 id="transfer-heading">{filter}</h1><span>{visible.length} {visible.length === 1 ? 'transfer' : 'transfers'}</span></div>
     {job && <div className="queue-actions">
      {state === 'queued' && <><button className="icon-button" aria-label="Move up in queue" title="Move up in queue" disabled={busy || queuePosition <= 0} onClick={() => void act(() => backend.move(job.id, -1))}><Icon name="upload" size={15} /></button>
       <button className="icon-button" aria-label="Move down in queue" title="Move down in queue" disabled={busy || queuePosition < 0 || queuePosition === queued.length - 1} onClick={() => void act(() => backend.move(job.id, 1))}><Icon name="download" size={15} /></button></>}
      <button className="text-button" disabled={busy || running(job) || state === 'removing'} onClick={() => void act(() => backend.recheck(job.id))}><Icon name="refresh" size={15} /> Verify files</button>
     </div>}
    </div>
    <TransferTable jobs={visible} selected={job?.id} onSelect={setSelectedId} />
    {!visible.length && <div className="empty-state"><div className="empty-icon"><Icon name={jobs.length ? 'search' : 'download'} size={31} /></div><h2>{jobs.length ? 'No matching transfers' : 'Your next download starts here'}</h2>
     <p>{jobs.length ? 'Try another filter or search term.' : 'Add a .torrent file or paste a magnet link.\nPause and resume without losing verified pieces.'}</p>
     {jobs.length ? <button onClick={() => { setSearch(''); setFilter('All transfers'); }}>Show all transfers</button> : <button disabled={busy} onClick={() => void pickTorrent()}><Icon name="add" size={16} /> Add a torrent</button>}
    </div>}
   </section>
   <TransferDetails job={job} onReveal={id => void act(() => backend.reveal(id))} />
   <footer className="statusbar"><span className="status-summary"><span className={`status-dot ${activeCount ? 'running' : ''}`} />{jobs.length ? `${activeCount} active · ${queued.length} queued · ${jobs.length} total` : 'Ready'}</span>
    <span><Icon name="download" size={13} />{bytes(downloadSpeed)}/s</span><span><Icon name="upload" size={13} />{bytes(uploadSpeed)}/s</span></footer>
  </section>
  {add && <AddTorrent initialSource={add.source} magnet={add.magnet} onClose={() => setAdd(null)} onAdded={id => { setSelectedId(id); setFilter('All transfers'); setSearch(''); }} />}
  {removeId && <dialog ref={removeDialog} className="remove-dialog" onCancel={() => setRemoveId(null)} aria-labelledby="remove-title"><h2 id="remove-title">Remove this transfer?</h2>
   <p>{removing?.name}<br />Active workers will stop first. Downloaded files and verified pieces stay in their destination.</p>
   <div className="dialog-buttons"><button autoFocus onClick={() => setRemoveId(null)}>Keep transfer</button><button className="danger" onClick={() => { const id = removeId; setRemoveId(null); void act(() => backend.remove(id)); }}>Remove transfer</button></div>
  </dialog>}
 </main>;
}
