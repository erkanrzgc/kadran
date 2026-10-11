import { useCallback, useEffect, useMemo, useState } from "react";

import { createRequestScope } from "../lib/management.ts";

/** Failed refreshes clear the current snapshot, so operations cannot use stale details. */
export function useManagementQuery<T>(load: () => Promise<T>) {
  const scope = useMemo(createRequestScope, [load]);
  const [state, setState] = useState<{ scope: typeof scope; data: T | null; error: string | null; loading: boolean }>({ scope, data: null, error: null, loading: true });
  const reload = useCallback(async () => {
    setState({ scope, data: null, error: null, loading: true });
    const result = await scope.run(load);
    if (result.status === "discarded") return;
    setState({ scope, data: result.status === "success" ? result.value : null, error: result.status === "error" ? result.error.message : null, loading: false });
  }, [load, scope]);
  useEffect(() => { void reload(); return () => scope.invalidate(); }, [reload, scope]);
  return { ...(state.scope === scope ? state : { data: null, error: null, loading: true }), reload };
}
