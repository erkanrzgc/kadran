import { useT } from "../i18n/I18nProvider.tsx";
import type { FleetMeter } from "../hooks/useFleetResources.ts";
import { useResourceText } from "../hooks/useResourceText.ts";

/** Bu yüzdenin üstü uyarı renginde: dolmak üzere. */
const WARN_PERCENT = 90;

/** Filo kartındaki üç ölçer (K-166): CPU, bellek, disk. */
export function ResourceMeters({ meter }: { meter: FleetMeter }) {
  const t = useT();
  const rows: ReadonlyArray<readonly [string, string, number | null]> = [
    ["cpu", t("res.cpu"), meter.cpu],
    ["memory", t("res.memory"), meter.memory],
    ["disk", t("res.disk"), meter.disk],
  ];
  return (
    <div className="meters" data-testid="meters">
      {rows.map(([key, label, value]) => (
        <Meter key={key} name={key} label={label} value={value} />
      ))}
    </div>
  );
}

function Meter({ name, label, value }: { name: string; label: string; value: number | null }) {
  const text = useResourceText().percent(value);
  const clamped = value === null ? 0 : Math.min(100, Math.max(0, value));
  // Değer yokken role="meter" olmaz: ekran okuyucuya sahte bir 0 söylenmesin.
  const aria = value === null ? {} : { role: "meter", "aria-label": label, "aria-valuemin": 0, "aria-valuemax": 100, "aria-valuenow": Math.round(clamped), "aria-valuetext": text };
  return (
    <div className={clamped >= WARN_PERCENT ? "meter is-high" : "meter"} data-meter={name} {...aria}>
      <span className="meter-label">{label}</span>
      <span className="meter-track" aria-hidden="true">
        <i className="meter-fill" style={{ transform: `scaleX(${clamped / 100})` }} />
      </span>
      <span className="meter-value mono">{text}</span>
    </div>
  );
}
