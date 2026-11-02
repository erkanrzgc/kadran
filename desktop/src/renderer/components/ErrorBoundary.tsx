import { Component, type ReactNode } from "react";

import { useT } from "../i18n/I18nProvider.tsx";
import { WindowButtons } from "./WindowButtons.tsx";

function Crashed({ message, onRetry }: { message: string; onRetry: () => void }) {
  const t = useT();
  return (
    <div className="app">
      <div className="shell" style={{ flexDirection: "column" }}>
        {/* Çerçevesiz pencere: düğmeler ve sürükleme burada da olmalı, yoksa kapatılamaz. */}
        <header className="topbar"><span style={{ flex: 1 }} /><WindowButtons /></header>
        <div className="empty-state" role="alert" data-testid="crashed">
          <h1 className="empty-title">{t("crash.title")}</h1>
          <p className="empty-body mono">{message}</p>
          <p><button type="button" className="btn btn-ghost" onClick={onRetry} data-testid="crash-retry">{t("action.retry")}</button></p>
        </div>
      </div>
    </div>
  );
}

/** Çizim hatası boş bir pencere bırakmasın: sebep ekranda görünür, pencere kapatılabilir. */
export class ErrorBoundary extends Component<{ children: ReactNode }, { message: string | null }> {
  override state = { message: null as string | null };

  static getDerivedStateFromError(err: unknown): { message: string } {
    return { message: err instanceof Error ? err.message : String(err) };
  }

  override render(): ReactNode {
    return this.state.message === null ? this.props.children : <Crashed message={this.state.message} onRetry={() => this.setState({ message: null })} />;
  }
}
