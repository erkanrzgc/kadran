import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { App } from "./App.tsx";
import { AppearanceProvider, useAppearance } from "./appearance/AppearanceProvider.tsx";
import { bridge } from "./bridge.ts";
import { I18nProvider } from "./i18n/I18nProvider.tsx";
import { Toast } from "./toast/Toast.tsx";
import { ToastProvider } from "./toast/ToastProvider.tsx";
import "./styles/tokens.css";
import "./styles/base.css";
import "./styles/fonts.css";
import "./styles/glass.css";
import "./styles/controls.css";
import "./styles/focus.css";
import "./styles/layout.css";
import "./styles/screens.css";

/** Dil görünüm ayarlarından geliyor; sağlayıcı onun içinde. */
function Localized() {
  const { appearance } = useAppearance();
  return (
    <I18nProvider language={appearance.language}>
      <App />
      <Toast />
    </I18nProvider>
  );
}

const root = document.getElementById("root");
if (!root) throw new Error("root element missing");

// Görünüm ilk çizimden ÖNCE okunur: yanlış renkte bir kare yanıp sönmesin.
const bundle = await bridge.loadAppearance();

createRoot(root).render(
  <StrictMode>
    <ToastProvider>
      <AppearanceProvider initial={bundle}>
        <Localized />
      </AppearanceProvider>
    </ToastProvider>
  </StrictMode>,
);
