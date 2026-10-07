import { useLanguage, useT } from "../i18n/I18nProvider.tsx";
import { chainView } from "../lib/chain.ts";
import { formatCount } from "../lib/format.ts";

interface Props {
  name: string;
  status: string | undefined;
  checked: string | undefined;
  detail: string | undefined;
  testId: string;
}

/**
 * Zincir kartı. BROKEN (kurcalama şüphesi) ile UNVERIFIABLE (işletim
 * sorunu) farklı renkte; aynı görünürlerse operatör sahte alarma koşar.
 */
export function ChainCard({ name, status, checked, detail, testId }: Props) {
  const t = useT();
  const language = useLanguage();
  const view = chainView(status);
  const n = Number(checked) || 0;
  return (
    <article className={`chain glass is-${view.tone}`} data-testid={testId}>
      <div className="chain-top">
        <h4 className="chain-name mono">{name}</h4>
        <span className={`badge is-${view.tone}`}>{view.label}</span>
      </div>
      <p className="chain-count">
        {status ? t("chains.records", { count: n, n: formatCount(n, language) }) : t("chains.not_verified")}
      </p>
      {detail ? <p className="chain-detail mono">{detail}</p> : null}
    </article>
  );
}
