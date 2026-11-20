import { useState } from 'react';
import { Icon } from './Icon';
import { bytes, type Job } from './types';

const tabs = ['Overview', 'Files', 'Peers', 'Trackers'] as const;
export default function TransferDetails({ job, onReveal }: { job: Job | undefined; onReveal: (id: string) => void }) {
 const [tab, setTab] = useState<typeof tabs[number]>('Overview');
 return <section className="details-pane" aria-label="Transfer details">
  <div className="tabs" role="tablist" aria-label="Transfer details">{tabs.map((t, i) => <button key={t} role="tab" id={`tab-${t}`} aria-controls={`panel-${t}`} aria-selected={tab === t} tabIndex={tab === t ? 0 : -1}
   onKeyDown={e => { if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') { e.preventDefault(); const next = tabs[(i + (e.key === 'ArrowRight' ? 1 : 3)) % 4]; setTab(next); document.getElementById(`tab-${next}`)?.focus(); } }} onClick={() => setTab(t)}>
   {t}{t === 'Files' && job ? <span>{job.files.length}</span> : null}
  </button>)}</div>
  <div className="detail-content" role="tabpanel" id={`panel-${tab}`} aria-labelledby={`tab-${tab}`} tabIndex={0}>
   {!job ? <div className="no-selection"><Icon name="info" size={18} /><span>Transfer details will appear here.</span></div> : <>
    {tab === 'Overview' && <>
     <div className="detail-title"><h2 title={job.name}>{job.name}</h2><button className="text-button" onClick={() => onReveal(job.id)}><Icon name="folder" size={16} /> Open folder</button></div>
     <dl className="overview-grid">
      <div><dt>Downloaded</dt><dd>{bytes(job.progress.completed)} <span>/ {bytes(job.progress.total)}</span></dd></div>
      <div><dt>Uploaded</dt><dd>{bytes(job.progress.uploaded)}</dd></div>
      <div><dt>Seed ratio</dt><dd>{job.progress.total ? (job.progress.uploaded / job.progress.total).toFixed(2) : '0.00'} <span>/ {job.seedRatio.toFixed(1)}</span></dd></div>
      <div className="destination"><dt>Save location</dt><dd title={job.destination}>{job.destination}</dd></div>
      <div><dt>Download limit</dt><dd>{job.downloadLimit ? `${bytes(job.downloadLimit)}/s` : 'Unlimited'}</dd></div>
      <div><dt>Upload limit</dt><dd>{job.uploadLimit ? `${bytes(job.uploadLimit)}/s` : 'Unlimited'}</dd></div>
     </dl>
     <p className={`engine-message ${job.progress.state === 'error' ? 'error-message' : ''}`}><Icon name={job.progress.state === 'error' ? 'error' : 'info'} size={15} /><span>{job.progress.message || (job.progress.state === 'seeding' ? 'Sharing verified pieces with other peers.' : 'Pieces are verified before being saved.')}</span></p>
    </>}
    {tab === 'Files' && <table className="files-table"><thead><tr><th>File</th><th>Size</th><th>Downloaded</th></tr></thead><tbody>{job.files.map((file, i) => <tr key={file.path}><td title={file.path}>{file.path}</td><td>{bytes(file.length)}</td><td>{job.selected.includes(i) ? `${bytes(job.progress.fileCompleted[i] ?? 0)} / ${bytes(file.length)}` : 'Skipped'}</td></tr>)}</tbody></table>}
    {tab === 'Peers' && <div className="peer-summary"><Icon name="link" size={23} /><h2>{job.progress.peers} connected download {job.progress.peers === 1 ? 'peer' : 'peers'}</h2><p>Connections are retried automatically when peers disconnect.<br />The engine limits outbound connections to 40 per transfer.</p></div>}
    {tab === 'Trackers' && <><p className="detail-description">{job.private ? 'Private torrent · tracker discovery only' : 'Configured trackers · public torrents also use DHT'}</p>{job.trackers.length ? <ul className="tracker-list">{job.trackers.map((tracker, i) => <li key={`${i}-${tracker}`}><Icon name="link" size={15} /><span title={tracker}>{tracker}</span></li>)}</ul> : <p className="detail-description">{job.private ? 'No trackers configured for this private torrent.' : 'No trackers specified. Peer discovery uses DHT.'}</p>}</>}
   </>}
  </div>
 </section>;
}
