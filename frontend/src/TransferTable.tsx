import { useRef, useState } from 'react';
import { Icon } from './Icon';
import { bytes, eta, labels, percent, type Job } from './types';

export default function TransferTable({ jobs, selected, onSelect }: {
 jobs: Job[]; selected: string | undefined; onSelect: (id: string) => void;
}) {
 const table = useRef<HTMLTableElement>(null);
 const [widths, setWidths] = useState<number[]>([]);
 const drag = useRef<{ index: number; x: number; widths: number[] } | null>(null);
 const measure = () => Array.from(table.current?.querySelectorAll('th') ?? [], th => th.getBoundingClientRect().width);
 return <div className={`table-scroll ${jobs.length ? 'has-rows' : 'empty-table'}`}><table ref={table} className="transfer-table" aria-label="Transfers"
  style={widths.length ? { width: widths.reduce((a, b) => a + b, 0) } : undefined}>
  <colgroup>{Array.from({ length: 6 }, (_, i) => <col key={i} style={widths[i] ? { width: widths[i] } : undefined} />)}</colgroup>
  <thead><tr>{['Name', 'Progress', 'Status', 'Down / Up', 'ETA', 'Peers'].map((name, index) => <th key={name} scope="col">
   {name}<span className="column-grip" role="separator" aria-label={`Resize ${name} column`} aria-orientation="vertical" aria-valuenow={Math.round(widths[index] ?? 100)} tabIndex={0}
    onKeyDown={e => { if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') { e.preventDefault(); const next = measure(); next[index] = Math.max(65, Math.min(600, next[index] + (e.key === 'ArrowRight' ? 15 : -15))); setWidths(next); } }}
    onPointerDown={e => { e.currentTarget.setPointerCapture(e.pointerId); drag.current = { index, x: e.clientX, widths: measure() }; }}
    onPointerMove={e => { const start = drag.current; if (!start || start.index !== index) return; const next = [...start.widths]; next[index] = Math.max(65, Math.min(600, start.widths[index] + e.clientX - start.x)); setWidths(next); }}
    onPointerUp={() => { drag.current = null; }} onPointerCancel={() => { drag.current = null; }} />
  </th>)}</tr></thead>
  <tbody>{jobs.map((job, index) => <tr key={job.id} id={`transfer-${job.id}`} className="transfer-row" tabIndex={job.id === selected ? 0 : -1}
   aria-selected={job.id === selected} aria-label={`${job.name}, ${labels[job.progress.state]}`} onClick={() => onSelect(job.id)}
   onFocus={() => onSelect(job.id)} onKeyDown={e => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') { e.preventDefault(); const next = jobs[index + (e.key === 'ArrowDown' ? 1 : -1)]; if (next) { onSelect(next.id); document.getElementById(`transfer-${next.id}`)?.focus(); } }
   }}>
   <td><div className="transfer-name"><span className="file-icon"><Icon name="torrent" size={23} /></span><div><strong title={job.name}>{job.name}</strong><small>{bytes(job.progress.completed)} of {bytes(job.progress.total)}</small></div></div></td>
   <td><div className="progress-cell"><span>{percent(job).toFixed(1)}%</span><progress value={percent(job)} max={100} aria-label={`${job.name} download progress`} /></div></td>
   <td><span className={`state state-${job.progress.state}`}>{labels[job.progress.state]}</span></td>
   <td className="speed-cell"><span>{bytes(job.progress.downloadSpeed)}/s</span><small>{bytes(job.progress.uploadSpeed)}/s</small></td>
   <td>{eta(job.progress)}</td><td>{job.progress.peers}</td>
  </tr>)}</tbody>
 </table></div>;
}
