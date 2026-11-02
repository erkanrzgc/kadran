import { useEffect, useRef, useState } from "react";

import { LineIcon } from "./LineIcon.tsx";

export interface CrumbItem {
  key: string;
  label: string;
  current?: boolean;
  onSelect: () => void;
}

interface Props {
  label: string;
  items: readonly CrumbItem[];
  testId: string;
}

/** Yoldaki seçici (Coolify düzeni): `Sunucular / sunucu ⌄`. */
export function CrumbMenu({ label, items, testId }: Props) {
  const [open, setOpen] = useState(false);
  const box = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) setOpen(false);
    };
    const esc = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setOpen(false);
        trigger.current?.focus();
      }
    };
    document.addEventListener("mousedown", close);
    document.addEventListener("keydown", esc);
    return () => {
      document.removeEventListener("mousedown", close);
      document.removeEventListener("keydown", esc);
    };
  }, [open]);

  return (
    <div className="crumb-box" ref={box}>
      <button ref={trigger} type="button" className="crumb" title={label} aria-expanded={open} data-testid={testId} onClick={() => setOpen((o) => !o)}>
        <span>{label}</span>
        <LineIcon name="chevron" size={12} />
      </button>
      {open ? (
        <ul className="crumb-menu">
          {items.map((item) => (
            <li key={item.key}>
              <button type="button" className="menu-item" aria-current={item.current ? "true" : undefined} onClick={() => { setOpen(false); item.onSelect(); }}>
                <span>{item.label}</span>
                {item.current ? <LineIcon name="chevron" size={12} /> : null}
              </button>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
