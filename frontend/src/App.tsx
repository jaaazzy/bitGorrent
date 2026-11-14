import { useEffect, useRef, useState } from 'react';
import AddTorrent from './AddTorrent';
import { backend, desktop } from './backend';
import { Icon, type IconName } from './Icon';
import { bytes, errorText, eta, labels, type Job, type Snapshot } from './types';

const filters: { name: string; icon: IconName; match: (j: Job) => boolean }[] = [
 { name: 'All transfers', icon: 'all', match: () => true },
 { name: 'Downloading', icon: 'download', match: j => ['downloading', 'checking', 'waiting', 'pausing'].includes(j.progress.state) },
 { name: 'Paused', icon: 'pause', match: j => j.progress.state === 'paused' },
 { name: 'Seeding', icon: 'upload', match: j => j.progress.state === 'seeding' },
 { name: 'Completed', icon: 'check', match: j => j.progress.state === 'completed' },
 { name: 'Needs attention', icon: 'error', match: j => j.progress.state === 'error' },
];
type Tab = 'Overview' | 'Files' | 'Peers' | 'Trackers';

export default function App() {
 const [snapshot, setSnapshot] = useState<Snapshot>({ revision: 0, job: null });
 const [filter, setFilter] = useState('All transfers');
 const [search, setSearch] = useState('');
 const [tab, setTab] = useState<Tab>('Overview');
 const [add, setAdd] = useState<{ source: string; magnet: boolean } | null>(null);
 const [error, setError] = useState('');
 const [busy, setBusy] = useState(false);
 const [remove, setRemove] = useState(false);
 const searchInput = useRef<HTMLInputElement>(null);
 const removeDialog = useRef<HTMLDialogElement>(null);
 const transferTable = useRef<HTMLTableElement>(null);
 const [columnWidths, setColumnWidths] = useState<number[]>([]);
 const resizing = useRef<{ index: number; x: number; widths: number[] } | null>(null);
 function resize(index: number, delta: number) {
  const widths = Array.from(transferTable.current?.querySelectorAll('th') ?? [], th => th.getBoundingClientRect().width);
  if (!widths.length) return; widths[index] = Math.max(65, Math.min(600, widths[index] + delta)); setColumnWidths(widths);
 }
 const job = snapshot.job;
 const visible = !!job && !!filters.find(f => f.name === filter)?.match(job) && job.name.toLocaleLowerCase().includes(search.toLocaleLowerCase());
 const state = job?.progress.state;
 const active = state !== undefined && ['checking', 'downloading', 'waiting', 'seeding', 'pausing'].includes(state);
 const percent = job ? (job.progress.total ? Math.min(100, job.progress.completed / job.progress.total * 100) : job.progress.state === 'completed' ? 100 : 0) : 0;
 useEffect(() => {
  let alive = true;
  const update = (s: Snapshot) => { if (alive) setSnapshot(old => s.revision >= old.revision ? s : old); };
  const stop = backend.onSnapshot(update);
  if (desktop()) void backend.snapshot().then(update).catch(e => { if (alive) setError(errorText(e)); });
  return () => { alive = false; stop(); };
 }, []);
 useEffect(() => { if (remove) removeDialog.current?.showModal(); }, [remove]);
 async function act(action: () => Promise<unknown>) {
  setError(''); setBusy(true);
  try { await action(); if (desktop()) { const s = await backend.snapshot(); setSnapshot(old => s.revision >= old.revision ? s : old); } }
  catch (e) { setError(errorText(e)); }
  finally { setBusy(false); }
 }
 async function pickTorrent() {
  await act(async () => { const source = await backend.pickTorrent(); if (source) setAdd({ source, magnet: false }); });
 }
 useEffect(() => {
  function key(e: KeyboardEvent) {
   if (!(e.ctrlKey || e.metaKey) || add || remove) return;
   if (e.key.toLowerCase() === 'f') { e.preventDefault(); searchInput.current?.focus(); }
   if (e.key.toLowerCase() === 'o') { e.preventDefault(); if (!job) void pickTorrent(); }
  }
  window.addEventListener('keydown', key); return () => window.removeEventListener('keydown', key);
 });
 return <main className="app">
  <aside className="sidebar" aria-label="Transfer navigation">
   <div className="brand"><span className="brand-symbol"><Icon name="download" size={21} /></span><span>bitGorrent</span></div>
   <nav aria-label="Filter transfers">{filters.map(f => <button key={f.name} className={`nav-item ${filter === f.name ? 'selected' : ''}`} aria-current={filter === f.name ? 'page' : undefined} onClick={() => setFilter(f.name)}><Icon name={f.icon} size={17} /><span>{f.name}</span><span className="count">{job && f.match(job) ? 1 : 0}</span></button>)}</nav>
   <div className="sidebar-note"><Icon name="info" size={16} /><p>One transfer at a time.<br /><span>Pause to keep your progress.</span></p></div>
  </aside>
  <section className="workspace" aria-label="Torrent transfers">
   <header className="toolbar">
    <div className="toolbar-group"><button className="primary" disabled={busy || !!job} onClick={() => void pickTorrent()} title={job ? 'Remove the current transfer before adding another' : 'Add torrent (Ctrl+O)'}><Icon name="add" /> Add torrent</button><button disabled={busy || !!job} onClick={() => setAdd({ source: '', magnet: true })}><Icon name="link" size={16} /> <span>Magnet link</span></button></div>
    <div className="toolbar-group transfer-actions"><button disabled={!job || busy || state === 'pausing'} onClick={() => void act(active ? backend.pause : backend.resume)}><Icon name={active ? 'pause' : 'play'} size={16} /> {state === 'pausing' ? 'Pausing…' : active ? 'Pause' : state === 'error' ? 'Retry' : 'Resume'}</button><button className="icon-button" title="Remove transfer" aria-label="Remove transfer" disabled={!job || active || busy} onClick={() => setRemove(true)}><Icon name="close" /></button></div>
    <label className="search"><Icon name="search" size={16} /><input ref={searchInput} aria-label="Search transfers" placeholder="Search transfers" value={search} onChange={e => setSearch(e.target.value)} /><kbd>Ctrl F</kbd></label>
   </header>
   {!desktop() && <div className="notice"><Icon name="info" size={16} />Browser preview. Open the Wails desktop app to download.</div>}
   {error && <div className="notice error-notice" role="alert"><Icon name="error" size={16} /><span>{error}</span><button className="icon-button" aria-label="Dismiss error" onClick={() => setError('')}><Icon name="close" size={15} /></button></div>}
   <section className="transfer-section" aria-labelledby="transfer-heading">
    <div className="section-heading"><div><h1 id="transfer-heading">{filter}</h1><span>{visible ? '1 transfer' : '0 transfers'}</span></div>{job && <button className="text-button" disabled={active || busy} onClick={() => void act(backend.recheck)}><Icon name="refresh" size={15} /> Verify files</button>}</div>
    <div className="table-scroll"><table ref={transferTable} className="transfer-table" style={columnWidths.length ? { width: columnWidths.reduce((a, b) => a + b, 0) } : undefined}><colgroup>{Array.from({ length: 6 }, (_, i) => <col key={i} style={columnWidths[i] ? { width: columnWidths[i] } : undefined} />)}</colgroup><thead><tr>{['Name', 'Progress', 'Status', 'Down / Up', 'ETA', 'Peers'].map((name, index) => <th key={name} scope="col">{name}<span className="column-grip" role="separator" aria-label={`Resize ${name} column`} aria-orientation="vertical" aria-valuenow={Math.round(columnWidths[index] ?? 100)} tabIndex={0} onKeyDown={e => { if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') { e.preventDefault(); resize(index, e.key === 'ArrowRight' ? 15 : -15); } }} onPointerDown={e => { e.currentTarget.setPointerCapture(e.pointerId); resizing.current = { index, x: e.clientX, widths: Array.from(transferTable.current?.querySelectorAll('th') ?? [], th => th.getBoundingClientRect().width) }; }} onPointerMove={e => { const drag = resizing.current; if (!drag || drag.index !== index) return; const widths = [...drag.widths]; widths[index] = Math.max(65, Math.min(600, drag.widths[index] + e.clientX - drag.x)); setColumnWidths(widths); }} onPointerUp={() => { resizing.current = null; }} onPointerCancel={() => { resizing.current = null; }} /></th>)}</tr></thead><tbody>{visible && job && <tr className="transfer-row" tabIndex={0} aria-label={`${job.name}, ${labels[job.progress.state]}`}><td><div className="transfer-name"><span className="file-icon"><Icon name="torrent" size={23} /></span><div><strong title={job.name}>{job.name}</strong><small>{bytes(job.progress.completed)} of {bytes(job.progress.total)}</small></div></div></td><td><div className="progress-cell"><span>{percent.toFixed(1)}%</span><progress value={percent} max={100} aria-label="Download progress" /></div></td><td><span className={`state state-${job.progress.state}`}>{labels[job.progress.state]}</span></td><td className="speed-cell"><span>{bytes(job.progress.downloadSpeed)}/s</span><small>{bytes(job.progress.uploadSpeed)}/s</small></td><td>{eta(job.progress)}</td><td>{job.progress.peers}</td></tr>}</tbody></table></div>
    {!visible && <div className="empty-state"><div className="empty-icon"><Icon name={job ? 'search' : 'download'} size={31} /></div><h2>{job ? 'No matching transfers' : 'Your next download starts here'}</h2><p>{job ? 'Try another filter or search term.' : 'Add a .torrent file or paste a magnet link.\nYour files stay on your computer.'}</p>{job ? <button onClick={() => { setSearch(''); setFilter('All transfers'); }}>Show all transfers</button> : <button disabled={busy} onClick={() => void pickTorrent()}><Icon name="add" size={16} /> Add a torrent</button>}</div>}
   </section>
   <section className="details-pane" aria-label="Transfer details">
    <div className="tabs" role="tablist" aria-label="Transfer details">{(['Overview', 'Files', 'Peers', 'Trackers'] as const).map((t, i) => <button key={t} role="tab" id={`tab-${t}`} aria-controls={`panel-${t}`} aria-selected={tab === t} tabIndex={tab === t ? 0 : -1} onKeyDown={e => { if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') { e.preventDefault(); const tabs = ['Overview', 'Files', 'Peers', 'Trackers'] as const; const next = tabs[(i + (e.key === 'ArrowRight' ? 1 : 3)) % 4]; setTab(next); document.getElementById(`tab-${next}`)?.focus(); } }} onClick={() => setTab(t)}>{t}{t === 'Files' && job ? <span>{job.files.length}</span> : null}</button>)}</div>
    <div className="detail-content" role="tabpanel" id={`panel-${tab}`} aria-labelledby={`tab-${tab}`} tabIndex={0}>
     {!job || !visible ? <div className="no-selection"><Icon name="info" size={18} /><span>{job ? 'Show a transfer to see its details.' : 'Transfer details will appear here.'}</span></div> : <>
      {tab === 'Overview' && <><div className="detail-title"><h2 title={job.name}>{job.name}</h2><button className="text-button" onClick={() => void act(backend.reveal)}><Icon name="folder" size={16} /> Open folder</button></div><dl className="overview-grid"><div><dt>Downloaded</dt><dd>{bytes(job.progress.completed)} <span>/ {bytes(job.progress.total)}</span></dd></div><div><dt>Uploaded</dt><dd>{bytes(job.progress.uploaded)}</dd></div><div><dt>Seed ratio</dt><dd>{job.progress.total ? (job.progress.uploaded / job.progress.total).toFixed(2) : '0.00'} <span>/ {job.seedRatio.toFixed(1)}</span></dd></div><div className="destination"><dt>Save location</dt><dd title={job.destination}>{job.destination}</dd></div><div><dt>Download limit</dt><dd>{job.downloadLimit ? `${bytes(job.downloadLimit)}/s` : 'Unlimited'}</dd></div><div><dt>Upload limit</dt><dd>{job.uploadLimit ? `${bytes(job.uploadLimit)}/s` : 'Unlimited'}</dd></div></dl><p className={`engine-message ${state === 'error' ? 'error-message' : ''}`}><Icon name={state === 'error' ? 'error' : 'info'} size={15} /><span>{job.progress.message || (state === 'seeding' ? 'Sharing verified pieces with other peers.' : 'Pieces are verified before being saved.')}</span></p></>}
      {tab === 'Files' && <table className="files-table"><thead><tr><th>File</th><th>Size</th><th>Downloaded</th></tr></thead><tbody>{job.files.map((f, i) => <tr key={f.path}><td title={f.path}>{f.path}</td><td>{bytes(f.length)}</td><td>{job.selected.includes(i) ? `${bytes(job.progress.fileCompleted?.[i] ?? 0)} / ${bytes(f.length)}` : 'Skipped'}</td></tr>)}</tbody></table>}
      {tab === 'Peers' && <div className="peer-summary"><Icon name="link" size={23} /><h2>{job.progress.peers} connected download {job.progress.peers === 1 ? 'peer' : 'peers'}</h2><p>Connections are retried automatically when peers disconnect.<br />The engine limits outbound connections to 40 per transfer.</p></div>}
      {tab === 'Trackers' && <><p className="detail-description">Configured trackers · public torrents also use DHT</p>{job.trackers?.length ? <ul className="tracker-list">{job.trackers.map((t, i) => <li key={`${i}-${t}`}><Icon name="link" size={15} /><span title={t}>{t}</span></li>)}</ul> : <p className="detail-description">No trackers specified. Peer discovery uses DHT.</p>}</>}
     </>}
    </div>
   </section>
   <footer className="statusbar"><span className="status-summary"><span className={`status-dot ${active ? 'running' : ''}`} />{active ? labels[state!] : 'Ready'}{job ? ' · 1 transfer' : ''}</span><span><Icon name="download" size={13} />{bytes(job?.progress.downloadSpeed ?? 0)}/s</span><span><Icon name="upload" size={13} />{bytes(job?.progress.uploadSpeed ?? 0)}/s</span></footer>
  </section>
  {add && <AddTorrent initialSource={add.source} magnet={add.magnet} onClose={() => setAdd(null)} />}
  {remove && <dialog ref={removeDialog} className="remove-dialog" onCancel={() => setRemove(false)} aria-labelledby="remove-title"><h2 id="remove-title">Remove this transfer?</h2><p>The transfer leaves the list. Downloaded files and verified pieces stay in their destination.</p><div className="dialog-buttons"><button autoFocus onClick={() => setRemove(false)}>Keep transfer</button><button className="danger" onClick={() => { setRemove(false); void act(backend.remove); }}>Remove transfer</button></div></dialog>}
 </main>;
}
