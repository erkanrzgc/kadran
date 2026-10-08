import { useCallback, useEffect, useState } from "react";

import { bridge, type Profile } from "./bridge.ts";
import { Rail, type View } from "./components/Rail.tsx";
import { Titlebar } from "./components/Titlebar.tsx";
import { TopBar } from "./components/TopBar.tsx";
import { useServer } from "./hooks/useServer.ts";
import { errorText } from "./lib/errors.ts";
import { healthOf, healthTone } from "./lib/health.ts";
import { createProfileStore } from "./lib/profile-store.ts";
import type { VersionPayload } from "./lib/types.ts";
import { AuditScreen } from "./screens/AuditScreen.tsx";
import { SettingsScreen } from "./screens/SettingsScreen.tsx";
import { StatusScreen } from "./screens/StatusScreen.tsx";
import { useToast } from "./toast/ToastProvider.tsx";

function initialView(): View {
  const v = new URLSearchParams(window.location.search).get("view");
  return v === "audit" || v === "settings" ? v : "status";
}

export function App() {
  const toast = useToast();
  const server = useServer();
  const [view, setView] = useState<View>(initialView);
  const [profiles, setProfiles] = useState<Profile[]>([]);
  // Okuma ve yazmalar sırada; diskten okunmadan yazılmaz (lib/profile-store.ts).
  const [store] = useState(() =>
    createProfileStore({
      load: () => bridge.loadProfiles(),
      save: (list) => bridge.saveProfiles(list),
      onChange: setProfiles,
    }),
  );
  const [version, setVersion] = useState<VersionPayload | null>(null);

  useEffect(() => {
    store.load().catch((err: unknown) => toast("toast.profiles_failed", { error: errorText(err) }));
    bridge.version().then(setVersion, (err: unknown) => toast("toast.sidecar_failed", { error: errorText(err) }));
  }, [toast, store]);

  const connect = useCallback(
    async (target: string) => {
      const ok = await server.connect(target);
      if (!ok) return;
      try {
        await store.add({ name: target, target });
      } catch (err) {
        toast("toast.save_failed", { error: errorText(err) });
      }
    },
    [server, store, toast],
  );

  const removeProfile = useCallback(
    async (target: string) => {
      try {
        await store.remove(target);
      } catch (err) {
        toast("toast.save_failed", { error: errorText(err) });
      }
    },
    [store, toast],
  );

  const health = healthOf(server.state.status, server.state.verify, server.state.stale);

  return (
    <div className="app" data-testid="app">
      <Titlebar />
      <div className="body">
        <Rail
          profiles={profiles}
          currentTarget={server.state.target}
          currentTone={healthTone(health)}
          view={view}
          connecting={server.state.connecting}
          version={version}
          onConnect={(target) => void connect(target)}
          onNavigate={setView}
        />
        <main className="stage">
          <TopBar
            target={server.state.target}
            profiles={profiles}
            health={health}
            rtt={server.state.rtt}
            verifying={server.state.verifying}
            onVerify={() => void server.verifyChains()}
            onSwitch={(target) => void connect(target)}
          />
          {view === "status" ? (
            <StatusScreen server={server} onSeeAll={() => setView("audit")} />
          ) : view === "audit" ? (
            <AuditScreen target={server.state.target} />
          ) : (
            <SettingsScreen profiles={profiles} version={version} onRemoveProfile={(target) => void removeProfile(target)} />
          )}
        </main>
      </div>
    </div>
  );
}
