import { useCallback, useEffect, useRef, useState } from "react";

import { createRequestScope } from "../lib/management.ts";

export function useManagementOperation() {
  const [scope] = useState(createRequestScope);
  const busy = useRef(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => () => scope.invalidate(), [scope]);
  const run = useCallback(async <T,>(work: () => Promise<T>): Promise<T | null> => {
    if (busy.current) return null;
    busy.current = true;
    setPending(true);
    setError(null);
    const result = await scope.run(work);
    if (result.status === "discarded") return null;
    busy.current = false;
    setPending(false);
    if (result.status === "error") { setError(result.error.message); return null; }
    return result.value;
  }, [scope]);
  return { pending, error, run, clearError: () => setError(null) };
}
