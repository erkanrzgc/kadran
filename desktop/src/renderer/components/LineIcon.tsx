/** Çizgi simgeler: 16 px ızgara, taslağın kendi çizimleri (docs/specs/2026-10-30-filo-taslak.html). */
const PATHS = {
  dashboard: <path d="M2.5 7.2 8 2.8l5.5 4.4V13a.5.5 0 0 1-.5.5h-3.2V9.8H6.2v3.7H3a.5.5 0 0 1-.5-.5z" />,
  server: <><rect x="2.5" y="2.5" width="11" height="4.5" rx="1" /><rect x="2.5" y="9" width="11" height="4.5" rx="1" /><circle cx="5" cy="4.75" r=".7" fill="currentColor" stroke="none" /><circle cx="5" cy="11.25" r=".7" fill="currentColor" stroke="none" /></>,
  layers: <path d="M8 2.2 13.5 5 8 7.8 2.5 5z M2.5 8 8 10.8 13.5 8 M2.5 11 8 13.8 13.5 11" />,
  bell: <path d="M4 11.5V7.2a4 4 0 0 1 8 0v4.3l1 1H3z M6.6 13.8a1.5 1.5 0 0 0 2.8 0" />,
  key: <><circle cx="5.5" cy="10.5" r="2.8" /><path d="m7.5 8.5 5.5-5.5M11 5l1.6 1.6M9.6 6.4l1.2 1.2" strokeLinecap="round" /></>,
  link: <path d="M6.8 9.2a2.5 2.5 0 0 0 3.5 0l2.2-2.2a2.5 2.5 0 0 0-3.5-3.5l-.8.8 M9.2 6.8a2.5 2.5 0 0 0-3.5 0L3.5 9a2.5 2.5 0 0 0 3.5 3.5l.8-.8" strokeLinecap="round" />,
  mail: <><rect x="2.5" y="3.5" width="11" height="9" rx="1" /><path d="m3 4.5 5 4 5-4" /></>,
  settings: <><circle cx="8" cy="8" r="2" /><path d="M8 1.8v1.7M8 12.5v1.7M1.8 8h1.7M12.5 8h1.7M3.6 3.6l1.2 1.2M11.2 11.2l1.2 1.2M3.6 12.4l1.2-1.2M11.2 4.8l1.2-1.2" strokeLinecap="round" /></>,
  search: <><circle cx="7" cy="7" r="4.2" /><path d="m10.2 10.2 3.3 3.3" strokeLinecap="round" /></>,
  plus: <path d="M8 3v10M3 8h10" strokeLinecap="round" />,
  up: <path d="M8 13V3.5M4 7.5l4-4 4 4" strokeLinecap="round" />,
  refresh: <path d="M13 8a5 5 0 1 1-1.5-3.6M13 2.8v2.6h-2.6" strokeLinecap="round" />,
  chevron: <path d="m4.5 6.5 3.5 3.5 3.5-3.5" strokeLinecap="round" />,
  database: <><ellipse cx="8" cy="4" rx="5" ry="1.8" /><path d="M3 4v8c0 1 2.2 1.8 5 1.8s5-.8 5-1.8V4M3 8c0 1 2.2 1.8 5 1.8s5-.8 5-1.8" /></>,
  shield: <path d="M8 2 13 4v4c0 3-2.2 5-5 6-2.8-1-5-3-5-6V4z" />,
  scissors: <><circle cx="4.5" cy="11.5" r="2" /><circle cx="4.5" cy="4.5" r="2" /><path d="M6.2 5.6 13 12M6.2 10.4 13 4" strokeLinecap="round" /></>,
  close: <path d="M4.5 4.5l7 7M11.5 4.5l-7 7" strokeLinecap="round" />,
} as const;

export type LineIconName = keyof typeof PATHS;

export function LineIcon({ name, size = 15 }: { name: LineIconName; size?: number }) {
  return (
    <svg viewBox="0 0 16 16" width={size} height={size} fill="none" stroke="currentColor" strokeWidth="1.2" strokeLinejoin="round" aria-hidden="true" style={{ flex: "none", opacity: 0.9 }}>
      {PATHS[name]}
    </svg>
  );
}
