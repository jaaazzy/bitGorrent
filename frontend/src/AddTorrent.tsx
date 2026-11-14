import { useEffect, useRef, useState } from 'react';
import { backend } from './backend';
import { Icon } from './Icon';
import { bytes, errorText, type Preview } from './types';

export default function AddTorrent({ initialSource, magnet, onClose }: { initialSource: string; magnet: boolean; onClose: () => void }) {
 const [source, setSource] = useState(initialSource);
 const [preview, setPreview] = useState<Preview | null>(null);
 const [selected, setSelected] = useState<number[]>([]);
 const [folder, setFolder] = useState('');
 const [ratio, setRatio] = useState(1);
 const [download, setDownload] = useState(0);
 const [upload, setUpload] = useState(0);
 const [loading, setLoading] = useState(false);
 const [status, setStatus] = useState('');
 const [error, setError] = useState('');
 const dialog = useRef<HTMLDialogElement>(null);
 const sequence = useRef(0);
 useEffect(() => {
  dialog.current?.showModal(); const stop = backend.onMetadata(setStatus);
  return () => { sequence.current++; backend.cancelMetadata(); stop(); };
 }, []);
 async function inspect(value: string) {
  const request = ++sequence.current; setLoading(true); setError(''); setPreview(null); setStatus('Reading torrent metadata…');
  try { const p = await backend.inspect(value); if (request !== sequence.current) return; setPreview(p); setSelected(p.files.map((_, i) => i)); }
  catch (e) { if (request === sequence.current) setError(errorText(e)); }
  finally { if (request === sequence.current) setLoading(false); }
 }
 useEffect(() => { if (initialSource) void inspect(initialSource); }, [initialSource]);
 async function add() {
  if (!preview) return; setLoading(true); setError('');
  try { await backend.add(preview, folder, selected, ratio, download * 1024, upload * 1024); onClose(); }
  catch (e) { setError(errorText(e)); setLoading(false); }
 }
 return <dialog ref={dialog} className="add-dialog" onCancel={onClose} aria-labelledby="add-title">
  <header className="dialog-header"><h2 id="add-title">Add {magnet ? 'magnet link' : 'torrent'}</h2><button className="icon-button" onClick={onClose} aria-label="Close add torrent"><Icon name="close" /></button></header>
  <div className="dialog-content">
   {magnet && <form className="magnet-entry" onSubmit={e => { e.preventDefault(); void inspect(source); }}><label htmlFor="magnet">Magnet link</label><div className="input-row"><input id="magnet" value={source} autoFocus placeholder="magnet:?xt=urn:btih:…" disabled={loading} onChange={e => { setSource(e.target.value); setPreview(null); }} /><button disabled={loading || !source.trim()} type="submit">Read metadata</button></div></form>}
   {loading && <p className="working" role="status"><span className="busy-indicator" />{status || 'Adding torrent…'}</p>}
   {preview && <>
    <div className="torrent-summary"><Icon name="torrent" size={26} /><div><h3>{preview.name}</h3><p>{bytes(preview.length)} · {preview.files.length} {preview.files.length === 1 ? 'file' : 'files'}{preview.private ? ' · Private torrent' : ''}</p></div></div>
    <div className="file-select-heading"><strong>Files to download</strong><button className="text-button" onClick={() => setSelected(selected.length === preview.files.length ? [] : preview.files.map((_, i) => i))}>{selected.length === preview.files.length ? 'Deselect all' : 'Select all'}</button></div>
    <div className="file-selection">{preview.files.map((file, i) => <label key={file.path}><input type="checkbox" checked={selected.includes(i)} disabled={loading} onChange={e => setSelected(s => e.target.checked ? [...s, i] : s.filter(n => n !== i))} /><span title={file.path}>{file.path}</span><small>{bytes(file.length)}</small></label>)}</div>
    <div className="field"><label htmlFor="destination">Download to</label><div className="input-row"><input id="destination" readOnly value={folder} placeholder="Choose a destination folder" /><button disabled={loading} onClick={() => void backend.pickFolder().then(p => { if (p) setFolder(p); }).catch(e => setError(errorText(e)))}><Icon name="folder" /> Browse</button></div></div>
    <details><summary><Icon name="settings" size={16} /> Transfer options</summary><div className="option-fields"><label>Stop at seed ratio<input type="number" min="0" max="100" step="0.1" value={ratio} onChange={e => setRatio(Number(e.target.value))} /><small>0 stops after downloading.</small></label><label>Download limit (KiB/s)<input type="number" min="0" max="1073741824" value={download} onChange={e => setDownload(Number(e.target.value))} /><small>0 means unlimited.</small></label><label>Upload limit (KiB/s)<input type="number" min="0" max="1073741824" value={upload} onChange={e => setUpload(Number(e.target.value))} /><small>0 means unlimited.</small></label></div></details>
   </>}
   {error && <p className="error-message" role="alert">{error}</p>}
  </div>
  <footer className="dialog-footer"><span>{preview ? `${bytes(preview.files.reduce((n, f, i) => n + (selected.includes(i) ? f.length : 0), 0))} selected` : 'Metadata is verified before downloading.'}</span><button onClick={onClose}>Cancel</button><button className="primary" disabled={!preview || !folder || !selected.length || loading || !Number.isFinite(ratio) || ratio < 0 || ratio > 100 || download < 0 || upload < 0} onClick={() => void add()}><Icon name="download" size={16} /> Start download</button></footer>
 </dialog>;
}
