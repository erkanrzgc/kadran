import { useCallback, useEffect, useRef, useState } from "react";

import { isCancelled, type ManagementCancelled } from "../../shared/management.ts";
import { useT } from "../i18n/I18nProvider.tsx";
import { errorText } from "../lib/errors.ts";
import { createRequestScope } from "../lib/management.ts";

/**
 * Durum değiştiren yönetim işlemleri. Ana süreçteki yerel onay kutusunda
 * vazgeçilirse sonuç hata DEĞİL tipli iptaldir (`{ cancelled: true }`):
 * burada TEK yerde işlenir. `run` null döner, nötr bir durum iletisi
 * (`notice`) kalır; ekranlar iptali başarı gibi işlemez.
 */
export function useManagementOperation() {
  const t = useT();
  const [scope] = useState(createRequestScope);
  const busy = useRef(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  useEffect(() => () => scope.invalidate(), [scope]);
  const run = useCallback(async <T,>(work: () => Promise<T | ManagementCancelled>): Promise<T | null> => {
    if (busy.current) return null;
    busy.current = true;
    setPending(true);
    setError(null);
    setNotice(null);
    const result = await scope.run(work);
    if (result.status === "discarded") return null;
    busy.current = false;
    setPending(false);
    if (result.status === "error") { setError(errorText(result.error)); return null; }
    if (isCancelled(result.value)) { setNotice(t("operation.cancelled")); return null; }
    return result.value;
  }, [scope, t]);
  return { pending, error, notice, run, clearError: () => { setError(null); setNotice(null); } };
}
