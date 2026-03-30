import { useState } from 'react';
import {
    FileUpload,
    SelectTorrentFile,
    SelectFolder
} from "../wailsjs/go/main/App";

function App() {
    const [torrentPath, setTorrentPath] = useState('');
    const [outputPath, setOutputPath] = useState('');
    const [resultText, setResultText] = useState('');
    const [status, setStatus] = useState('idle'); // idle | loading | success | error

    async function pickTorrent() {
        try {
            const path = await SelectTorrentFile();
            if (path) setTorrentPath(path);
        } catch (err) {
            console.error(err);
        }
    }

    async function pickOutput() {
        try {
            const path = await SelectFolder();
            if (path) setOutputPath(path);
        } catch (err) {
            console.error(err);
        }
    }

    async function startDownload() {
        if (!torrentPath || !outputPath) return;
        setStatus('loading');
        setResultText('');
        try {
            const result = await FileUpload(torrentPath, outputPath);
            setResultText(result);
            setStatus('success');
        } catch (err) {
            setResultText("" + err);
            setStatus('error');
        }
    }

    const canDownload = torrentPath && outputPath && status !== 'loading';

    const trimPath = (p) => {
        if (!p) return null;
        const parts = p.replace(/\\/g, '/').split('/');
        return parts[parts.length - 1] || p;
    };

    return (
        <>
            <style>{`
                @import url('https://fonts.googleapis.com/css2?family=DM+Mono:wght@400;500&family=DM+Sans:wght@300;400;500&display=swap');

                *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }

                body {
                    background: #0d0d0d;
                    min-height: 100vh;
                    display: flex;
                    align-items: center;
                    justify-content: center;
                    font-family: 'DM Sans', sans-serif;
                }

                .shell {
                    width: 420px;
                    background: #111;
                    border: 1px solid #222;
                    border-radius: 16px;
                    overflow: hidden;
                    box-shadow: 0 0 0 1px #1a1a1a, 0 32px 64px rgba(0,0,0,0.6);
                }

                .header {
                    padding: 24px 28px 20px;
                    border-bottom: 1px solid #1c1c1c;
                    display: flex;
                    align-items: center;
                    gap: 12px;
                }

                .logo {
                    width: 32px;
                    height: 32px;
                    background: #e8ff47;
                    border-radius: 8px;
                    display: flex;
                    align-items: center;
                    justify-content: center;
                    flex-shrink: 0;
                }

                .logo svg { display: block; }

                .header-text h1 {
                    font-size: 14px;
                    font-weight: 500;
                    color: #f0f0f0;
                    letter-spacing: 0.01em;
                }

                .header-text p {
                    font-size: 11px;
                    color: #444;
                    margin-top: 1px;
                    font-family: 'DM Mono', monospace;
                    letter-spacing: 0.02em;
                }

                .body {
                    padding: 24px 28px;
                    display: flex;
                    flex-direction: column;
                    gap: 12px;
                }

                .field {
                    display: flex;
                    flex-direction: column;
                    gap: 6px;
                }

                .label {
                    font-size: 10px;
                    font-weight: 500;
                    letter-spacing: 0.08em;
                    text-transform: uppercase;
                    color: #3a3a3a;
                    font-family: 'DM Mono', monospace;
                }

                .pick-row {
                    display: flex;
                    align-items: center;
                    gap: 8px;
                    background: #161616;
                    border: 1px solid #222;
                    border-radius: 10px;
                    padding: 10px 14px;
                    cursor: pointer;
                    transition: border-color 0.15s, background 0.15s;
                }

                .pick-row:hover {
                    border-color: #333;
                    background: #1a1a1a;
                }

                .pick-row.filled {
                    border-color: #2a2a2a;
                }

                .pick-icon {
                    color: #333;
                    flex-shrink: 0;
                    transition: color 0.15s;
                }

                .pick-row:hover .pick-icon,
                .pick-row.filled .pick-icon {
                    color: #e8ff47;
                }

                .pick-value {
                    flex: 1;
                    font-size: 12px;
                    font-family: 'DM Mono', monospace;
                    color: #555;
                    white-space: nowrap;
                    overflow: hidden;
                    text-overflow: ellipsis;
                }

                .pick-value.set {
                    color: #bbb;
                }

                .pick-badge {
                    font-size: 10px;
                    font-family: 'DM Mono', monospace;
                    color: #e8ff47;
                    background: rgba(232, 255, 71, 0.08);
                    border: 1px solid rgba(232, 255, 71, 0.15);
                    border-radius: 4px;
                    padding: 2px 6px;
                    flex-shrink: 0;
                }

                .divider {
                    height: 1px;
                    background: #1a1a1a;
                    margin: 4px 0;
                }

                .btn-download {
                    width: 100%;
                    padding: 13px;
                    background: #e8ff47;
                    color: #0d0d0d;
                    border: none;
                    border-radius: 10px;
                    font-size: 13px;
                    font-weight: 500;
                    font-family: 'DM Sans', sans-serif;
                    letter-spacing: 0.02em;
                    cursor: pointer;
                    transition: opacity 0.15s, transform 0.1s;
                    display: flex;
                    align-items: center;
                    justify-content: center;
                    gap: 8px;
                    margin-top: 4px;
                }

                .btn-download:hover:not(:disabled) {
                    opacity: 0.9;
                    transform: translateY(-1px);
                }

                .btn-download:active:not(:disabled) {
                    transform: translateY(0);
                }

                .btn-download:disabled {
                    opacity: 0.25;
                    cursor: not-allowed;
                }

                .btn-download.loading {
                    opacity: 0.7;
                    cursor: wait;
                }

                .spinner {
                    width: 14px;
                    height: 14px;
                    border: 2px solid rgba(0,0,0,0.2);
                    border-top-color: #0d0d0d;
                    border-radius: 50%;
                    animation: spin 0.7s linear infinite;
                }

                @keyframes spin { to { transform: rotate(360deg); } }

                .result {
                    font-size: 11px;
                    font-family: 'DM Mono', monospace;
                    padding: 10px 14px;
                    border-radius: 8px;
                    line-height: 1.5;
                    word-break: break-all;
                }

                .result.success {
                    background: rgba(232, 255, 71, 0.06);
                    border: 1px solid rgba(232, 255, 71, 0.12);
                    color: #c8e830;
                }

                .result.error {
                    background: rgba(255, 80, 80, 0.06);
                    border: 1px solid rgba(255, 80, 80, 0.15);
                    color: #ff6b6b;
                }

                .footer {
                    padding: 14px 28px;
                    border-top: 1px solid #1a1a1a;
                    display: flex;
                    align-items: center;
                    gap: 6px;
                }

                .dot {
                    width: 6px;
                    height: 6px;
                    border-radius: 50%;
                    background: #2a2a2a;
                    transition: background 0.3s;
                }

                .dot.active { background: #e8ff47; box-shadow: 0 0 6px rgba(232,255,71,0.4); }
                .dot.error-dot { background: #ff5050; }

                .footer-text {
                    font-size: 11px;
                    font-family: 'DM Mono', monospace;
                    color: #333;
                    margin-left: 4px;
                }

                .footer-text.active { color: #888; }
            `}</style>

            <div className="shell">
                {/* Header */}
                <div className="header">
                    <div className="logo">
                        <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
                            <path d="M8 2L8 10M8 10L5 7M8 10L11 7" stroke="#0d0d0d" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"/>
                            <path d="M3 13H13" stroke="#0d0d0d" strokeWidth="1.8" strokeLinecap="round"/>
                        </svg>
                    </div>
                    <div className="header-text">
                        <h1>Torrent Downloader</h1>
                        <p>v1.0.0 · wails/go</p>
                    </div>
                </div>

                {/* Body */}
                <div className="body">
                    {/* Torrent file */}
                    <div className="field">
                        <span className="label">Torrent File</span>
                        <div
                            className={`pick-row ${torrentPath ? 'filled' : ''}`}
                            onClick={pickTorrent}
                        >
                            <svg className="pick-icon" width="14" height="14" viewBox="0 0 14 14" fill="none">
                                <rect x="1" y="1" width="12" height="12" rx="2" stroke="currentColor" strokeWidth="1.3"/>
                                <path d="M4 7h6M7 4v6" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round"/>
                            </svg>
                            <span className={`pick-value ${torrentPath ? 'set' : ''}`}>
                                {torrentPath ? trimPath(torrentPath) : 'Choose .torrent file…'}
                            </span>
                            {torrentPath && <span className="pick-badge">.torrent</span>}
                        </div>
                    </div>

                    {/* Output folder */}
                    <div className="field">
                        <span className="label">Output Folder</span>
                        <div
                            className={`pick-row ${outputPath ? 'filled' : ''}`}
                            onClick={pickOutput}
                        >
                            <svg className="pick-icon" width="14" height="14" viewBox="0 0 14 14" fill="none">
                                <path d="M1 4a1 1 0 011-1h3l1.5 1.5H12a1 1 0 011 1V11a1 1 0 01-1 1H2a1 1 0 01-1-1V4z" stroke="currentColor" strokeWidth="1.3"/>
                            </svg>
                            <span className={`pick-value ${outputPath ? 'set' : ''}`}>
                                {outputPath ? trimPath(outputPath) : 'Choose destination folder…'}
                            </span>
                            {outputPath && <span className="pick-badge">dir</span>}
                        </div>
                    </div>

                    <div className="divider" />

                    {/* Download button */}
                    <button
                        className={`btn-download ${status === 'loading' ? 'loading' : ''}`}
                        onClick={startDownload}
                        disabled={!canDownload}
                    >
                        {status === 'loading' ? (
                            <>
                                <span className="spinner" />
                                Downloading…
                            </>
                        ) : (
                            <>
                                <svg width="14" height="14" viewBox="0 0 14 14" fill="none">
                                    <path d="M7 1.5v7M7 8.5L4.5 6M7 8.5L9.5 6" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"/>
                                    <path d="M2 11.5h10" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round"/>
                                </svg>
                                Start Download
                            </>
                        )}
                    </button>

                    {/* Result */}
                    {resultText && (
                        <div className={`result ${status}`}>
                            {resultText}
                        </div>
                    )}
                </div>

                {/* Footer status */}
                <div className="footer">
                    <div className={`dot ${status === 'loading' ? 'active' : ''} ${status === 'error' ? 'error-dot' : ''} ${status === 'success' ? 'active' : ''}`} />
                    <span className={`footer-text ${status !== 'idle' ? 'active' : ''}`}>
                        {status === 'idle' && 'ready'}
                        {status === 'loading' && 'downloading…'}
                        {status === 'success' && 'complete'}
                        {status === 'error' && 'failed'}
                    </span>
                </div>
            </div>
        </>
    );
}

export default App;