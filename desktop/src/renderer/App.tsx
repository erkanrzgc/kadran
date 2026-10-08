import { useCallback, useEffect, useRef, useState } from "react";

import { bridge, type Profile } from "./bridge.ts";
import { Rail, type View } from "./components/Rail.tsx";
import { Titlebar } from "./components/Titlebar.tsx";
import { TopBar } from "./components/TopBar.tsx";
import { useServer } from "./hooks/useServer.ts";
import { errorText } from "./lib/errors.ts";
import { healthOf, healthTone } from "./lib/health.ts";
import { createSerial } from "./lib/serial.ts";
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
  // Kaydedilmiş en son liste. Yazma işleri render anındaki `profiles`i değil
  // bunu okur; yoksa sıraya girmiş ikinci iş eski listeden başlar.
  const profilesRef = useRef<Profile[]>([]);
  // Profil yazmaları (ekleme, silme) tek sırada çalışır.
  const [serial] = useState(() => createSerial());
  const applyProfiles = useCallback((next: Profile[]) => {
    profilesRef.current = next;
    setProfiles(next);
  }, []);
  const [version, setVersion] = useState<VersionPayload | null>(null);

  useEffect(() => {
    bridge.loadProfiles().then(applyProfiles, (err: unknown) => toast("toast.profiles_failed", { error: errorText(err) }));
    bridge.version().then(setVersion, (err: unknown) => toast("toast.sidecar_failed", { error: errorText(err) }));
  }, [toast, applyProfiles]);

  const connect = useCallback(
    async (target: string) => {
      const ok = await server.connect(target);
      if (!ok) return;
      await serial(async () => {
        // Sıra bize geldiğinde liste değişmiş olabilir: koşulu burada yeniden bak.
        const current = profilesRef.current;
        if (current.some((p) => p.target === target)) return;
        try {
          applyProfiles(await bridge.saveProfiles([...current, { name: target, target }]));
        } catch (err) {
          toast("toast.save_failed", { error: errorText(err) });
        }
      });
    },
    [server, serial, applyProfiles, toast],
  );

  const removeProfile = useCallback(
    (target: string) =>
      serial(async () => {
        const current = profilesRef.current;
        if (!current.some((p) => p.target === target)) return;
        try {
          applyProfiles(await bridge.saveProfiles(current.filter((p) => p.target !== target)));
        } catch (err) {
          toast("toast.save_failed", { error: errorText(err) });
        }
      }),
    [serial, applyProfiles, toast],
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
