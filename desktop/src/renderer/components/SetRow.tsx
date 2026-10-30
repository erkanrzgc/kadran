import type { ReactNode } from "react";

/** Ayar satırı: solda ad ve tek satırlık açıklama, sağda denetim. */
export function SetRow({ title, hint, children }: { title: string; hint: string; children: ReactNode }) {
  return (
    <div className="set-row">
      <div className="set-label">
        <strong>{title}</strong>
        <span>{hint}</span>
      </div>
      <div className="set-ctl">{children}</div>
    </div>
  );
}
