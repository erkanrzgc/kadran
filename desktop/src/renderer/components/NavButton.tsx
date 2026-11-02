import { LineIcon, type LineIconName } from "./LineIcon.tsx";

interface NavButtonProps {
  icon: LineIconName;
  label: string;
  active?: boolean;
  onClick?: () => void;
  testId?: string;
  count?: number;
  /** Henüz işlevi yok: görünür ama basılamaz, ipucu nedenini söyler. */
  soon?: string;
}

export function NavButton({ icon, label, active = false, onClick, testId, count, soon }: NavButtonProps) {
  return (
    <button
      type="button"
      className={active ? "nav-item is-active" : "nav-item"}
      aria-current={active ? "page" : undefined}
      data-testid={testId}
      disabled={soon !== undefined}
      title={soon ?? label}
      onClick={onClick}
    >
      <LineIcon name={icon} />
      <span className="nav-label">{label}</span>
      {count ? <span className="nav-count">{count}</span> : null}
    </button>
  );
}
