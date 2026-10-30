import { Component, type ReactNode } from "react";

import { useT } from "../i18n/I18nProvider.tsx";

function Crashed({ message }: { message: string }) {
  const t = useT();
  return (
    <div className="empty-state" role="alert" data-testid="crashed">
      <h1 className="empty-title">{t("crash.title")}</h1>
      <p className="empty-body mono">{message}</p>
    </div>
  );
}

/** Çizim hatası boş bir pencere bırakmasın: sebep ekranda görünür. */
export class ErrorBoundary extends Component<{ children: ReactNode }, { message: string | null }> {
  override state = { message: null as string | null };

  static getDerivedStateFromError(err: unknown): { message: string } {
    return { message: err instanceof Error ? err.message : String(err) };
  }

  override render(): ReactNode {
    return this.state.message === null ? this.props.children : <Crashed message={this.state.message} />;
  }
}
