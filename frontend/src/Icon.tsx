import type { CSSProperties } from 'react';
const paths = {
 add: 'M12 5v14M5 12h14', download: 'M12 3v12m-5-5 5 5 5-5M4 17v4h16v-4',
 upload: 'M12 17V5m-5 5 5-5 5 5M4 17v4h16v-4', pause: 'M8 5v14M16 5v14',
 play: 'm8 4 12 8-12 8Z', folder: 'M3 7V5h6l2 3h10v12H3Z',
 check: 'm5 12 4 4L19 6', close: 'm6 6 12 12M6 18 18 6', search: 'M20 20l-5-5M17 10a7 7 0 1 1-14 0 7 7 0 0 1 14 0',
 all: 'M4 5h16M4 12h16M4 19h16', error: 'M12 8v5m0 4h.01M12 3 2 21h20Z',
 link: 'm9 15 6-6M8 17l-1 1a4 4 0 0 1-6-6l5-5a4 4 0 0 1 6 0m0 10a4 4 0 0 0 6 0l5-5a4 4 0 0 0-6-6l-1 1',
 refresh: 'M20 7v5h-5M4 17v-5h5M5 8a8 8 0 0 1 13-3l2 3M4 16l2 3a8 8 0 0 0 13-3',
 settings: 'M4 7h16M4 17h16M8 4v6m8 4v6', torrent: 'M5 3h10l4 4v14H5ZM14 3v5h5M8 13h8m-8 4h5',
 info: 'M12 11v6m0-10h.01M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0',
} as const;
export type IconName = keyof typeof paths;
export function Icon({ name, size = 18, style }: { name: IconName; size?: number; style?: CSSProperties }) {
 return <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" style={style}><path d={paths[name]} /></svg>;
}
