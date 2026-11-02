/** Çizgi simgeler: 16 px, 24'lük ızgara, 1,5 çizgi (Coolify ölçüsü). */
const PATHS = {
  server: "M4 4h16v6H4zM4 14h16v6H4zM8 7h.01M8 17h.01",
  layers: "M12 3l9 5-9 5-9-5zM3 12l9 5 9-5M3 16l9 5 9-5",
  bell: "M6 17v-6a6 6 0 0 1 12 0v6l1.5 1.5h-15zM10 21a2 2 0 0 0 4 0",
  key: "M15 7a4 4 0 1 1-3 6.7L5 21H3v-3l7.3-7.3A4 4 0 0 1 15 7z",
  shield: "M12 3l8 3v6c0 4.5-3.2 7.5-8 9-4.8-1.5-8-4.5-8-9V6z",
  mail: "M4 6h16v12H4zM4 7l8 6 8-6",
  settings: "M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM12 3v3M12 18v3M3 12h3M18 12h3M5.6 5.6l2.1 2.1M16.3 16.3l2.1 2.1M5.6 18.4l2.1-2.1M16.3 7.7l2.1-2.1",
  search: "M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14zM20 20l-3.5-3.5",
  plus: "M12 5v14M5 12h14",
  up: "M12 19V5M6 11l6-6 6 6",
  chevron: "M7 9l5 5 5-5",
  database: "M12 3c4.4 0 8 1.3 8 3s-3.6 3-8 3-8-1.3-8-3 3.6-3 8-3zM4 6v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3",
  scissors: "M6 21a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM6 9a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM8.5 7.5 20 19M8.5 16.5 20 5",
  close: "M6 6l12 12M18 6 6 18",
  list: "M9 6h11M9 12h11M9 18h11M4 6h.01M4 12h.01M4 18h.01",
} as const;

export type LineIconName = keyof typeof PATHS;

export function LineIcon({ name, size = 16 }: { name: LineIconName; size?: number }) {
  return (
    <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" style={{ flex: "none" }}>
      <path d={PATHS[name]} />
    </svg>
  );
}
