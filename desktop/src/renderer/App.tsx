import { useCallback, useEffect, useState } from "react";

import { bridge, type Profile } from "./bridge.ts";
import { Backdrop } from "./components/Backdrop.tsx";
import { Rail, type FleetView } from "./components/Rail.tsx";
import { ServerDialog, type ServerDraft } from "./components/ServerDialog.tsx";
import { ServerSubnav, ServerSubnavRow, type ServerView } from "./components/ServerSubnav.tsx";
import { Tabs } from "./components/Tabs.tsx";
import { TopBar } from "./components/TopBar.tsx";
import { useT } from "./i18n/I18nProvider.tsx";
import { useFleet } from "./hooks/useFleet.ts";
import { useServer } from "./hooks/useServer.ts";
import { errorText } from "./lib/errors.ts";
import { alarmCount } from "./lib/fleet.ts";
import { healthOf } from "./lib/health.ts";
import { createProfileStore } from "./lib/profile-store.ts";
import type { VersionPayload } from "./lib/types.ts";
import { AlarmsScreen } from "./screens/AlarmsScreen.tsx";
import { AppsScreen } from "./screens/AppsScreen.tsx";
import { AuditScreen } from "./screens/AuditScreen.tsx";
import { BackupsScreen } from "./screens/BackupsScreen.tsx";
import { Dashboard, ServersSection } from "./screens/Dashboard.tsx";
import { FleetAlarms, FleetApps, FleetAudit } from "./screens/FleetLists.tsx";
import { SettingsScreen } from "./screens/SettingsScreen.tsx";
import { StatusScreen } from "./screens/StatusScreen.tsx";
import { useToast } from "./toast/ToastProvider.tsx";

/** Pencere yeniden kurulunca arayüz Settings'te açılır (main/window.ts). */
function initialView(): FleetView {
  return new URLSearchParams(window.location.search).get("view") === "settings" ? "settings" : "dashboard";
}

type Dialog = { kind: "add" } | { kind: "edit"; profile: Profile } | null;

export function App() {
  const t = useT();
  const toast = useToast();
  const server = useServer();
  const [view, setView] = useState<FleetView>(initialView);
  const [serverView, setServerView] = useState<ServerView>("status");
  const [tabs, setTabs] = useState<readonly string[]>([]);
  const [active, setActive] = useState<string | null>(null);
  const [newAppFor, setNewAppFor] = useState<string | null>(null);
  const [dialog, setDialog] = useState<Dialog>(null);
  const [dialogPending, setDialogPending] = useState(false);
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
  const fleet = useFleet(profiles, active === null);

  useEffect(() => {
    store.load().catch((err: unknown) => toast("toast.profiles_failed", { error: errorText(err) }));
    bridge.version().then(setVersion, (err: unknown) => toast("toast.sidecar_failed", { error: errorText(err) }));
  }, [toast, store]);

  const openServer = useCallback(
    async (target: string, next?: ServerView) => {
      setTabs((list) => (list.includes(target) ? list : [...list, target]));
      setActive(target);
      if (next) setServerView(next);
      if (server.state.target === target) return;
      // Bağlanamazsa sebep bildirimde; sekme kapanır, kart zaten durumu gösterir.
      if ((await server.connect(target)) === "failed") {
        setTabs((list) => list.filter((t) => t !== target));
        setActive((current) => (current === target ? null : current));
      }
    },
    [server],
  );

  const closeTab = useCallback((target: string) => {
    setTabs((list) => list.filter((t) => t !== target));
    setActive((current) => (current === target ? null : current));
  }, []);

  const navigate = useCallback((next: FleetView) => {
    setView(next);
    setActive(null);
  }, []);

  const newApp = useCallback(
    (target: string) => {
      setNewAppFor(target);
      void openServer(target, "apps");
    },
    [openServer],
  );

  const removeProfile = useCallback(
    async (target: string) => {
      try {
        await store.remove(target);
        closeTab(target);
      } catch (err) {
        toast("toast.save_failed", { error: errorText(err) });
      }
    },
    [store, closeTab, toast],
  );

  const submitDialog = useCallback(
    async (draft: ServerDraft) => {
      setDialogPending(true);
      try {
        if (dialog?.kind === "edit") {
          await store.update(draft.target, { name: draft.name, provider: draft.provider });
          setDialog(null);
          return;
        }
        // Önce bağlanılır: bağlanılamayan hedef kaydedilmez, sebep bildirimde görünür.
        if ((await server.connect(draft.target)) !== "connected") return;
        await store.add({ name: draft.name, target: draft.target, ...(draft.provider ? { provider: draft.provider } : {}) });
        setTabs((list) => (list.includes(draft.target) ? list : [...list, draft.target]));
        setActive(draft.target);
        setDialog(null);
      } catch (err) {
        toast("toast.save_failed", { error: errorText(err) });
      } finally {
        setDialogPending(false);
      }
    },
    [dialog, store, server, toast],
  );

  const health = healthOf(server.state.status, server.state.verify, server.state.stale || server.state.verifyStale);
  const currentProfile = profiles.find((p) => p.target === active) ?? null;
  const editServer = (target: string) => {
    const profile = profiles.find((p) => p.target === target);
    if (profile) setDialog({ kind: "edit", profile });
  };
  const fleetActions = { onOpen: (target: string) => void openServer(target), onNewApp: newApp, onEdit: editServer, onAdd: () => setDialog({ kind: "add" }) };
  const listProps = { profiles, cards: fleet.cards, onOpen: (target: string, next?: ServerView) => void openServer(target, next) };
  const clientVersion = version?.version ?? null;

  return (
    <div className="app" data-testid="app">
      <Backdrop />
      <div className="shell">
        <Rail view={active === null ? view : null} serverOpen={active !== null} alarmCount={alarmCount(fleet.cards)} version={version} onNavigate={navigate} />
        <div className="main">
          <Tabs tabs={tabs} active={active} profiles={profiles} cards={fleet.cards} onSelectFleet={() => navigate("dashboard")} onSelect={(target) => void openServer(target)} onClose={closeTab} />
          {active === null ? (
            <>
              <TopBar mode="fleet" view={view} profiles={profiles} onOpenServer={fleetActions.onOpen} onNavigate={navigate} />
              <main className="stage">
                {view === "dashboard" ? (
                  <Dashboard profiles={profiles} cards={fleet.cards} clientVersion={clientVersion} onSeeAll={() => navigate("audit")} {...fleetActions} />
                ) : view === "servers" ? (
                  <ServersSection profiles={profiles} cards={fleet.cards} clientVersion={clientVersion} {...fleetActions} />
                ) : view === "apps" ? (
                  <FleetApps {...listProps} />
                ) : view === "alarms" ? (
                  <FleetAlarms {...listProps} />
                ) : view === "audit" ? (
                  <FleetAudit {...listProps} />
                ) : (
                  <SettingsScreen profiles={profiles} version={version} onRemoveProfile={(target) => void removeProfile(target)} />
                )}
              </main>
            </>
          ) : (
            <>
              <TopBar mode="server" target={active} health={health} rtt={server.state.rtt} verifying={server.state.verifying} profiles={profiles} onOpenServer={fleetActions.onOpen} onNavigate={navigate} onVerify={() => void server.verifyChains()} />
              <ServerSubnavRow view={serverView} onNavigate={setServerView} />
              <div className="page-host">
                <ServerSubnav view={serverView} onNavigate={setServerView} />
                <main className="stage">
                  {/* Bağlantı kurulmadan ekran veri istemez: bağlanamayan sunucuda tek bildirim çıksın. */}
                  {server.state.target !== active || !server.state.status ? (
                    <p className="meta" role="status" data-testid="server-loading">{t("resource.loading")}</p>
                  ) : serverView === "status" ? (
                    <StatusScreen server={server} profile={currentProfile} onEdit={editServer} onSeeAll={() => setServerView("audit")} />
                  ) : serverView === "audit" ? (
                    <AuditScreen target={server.state.target} />
                  ) : serverView === "apps" ? (
                    <AppsScreen key={server.state.target} target={server.state.target} startCreating={newAppFor === active} onCreateConsumed={() => setNewAppFor(null)} />
                  ) : serverView === "alarms" ? (
                    <AlarmsScreen key={server.state.target} target={server.state.target} />
                  ) : (
                    <BackupsScreen key={server.state.target} target={server.state.target} />
                  )}
                </main>
              </div>
            </>
          )}
        </div>
      </div>
      {dialog ? <ServerDialog profile={dialog.kind === "edit" ? dialog.profile : null} pending={dialogPending} onSubmit={(draft) => void submitDialog(draft)} onClose={() => setDialog(null)} /> : null}
    </div>
  );
}
