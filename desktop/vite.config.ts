import { fileURLToPath } from "node:url";

import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Arayüz paketlenmiş uygulamada file:// üzerinden loadFile ile açılıyor;
// bu yüzden yollar GÖRELİ (base: "./"). Geliştirmede de canlı sunucu yok:
// CSP'ye geliştirme istisnası açılmasın diye hep derlenmiş dosya yüklenir.
export default defineConfig({
  root: fileURLToPath(new URL("./src/renderer", import.meta.url)),
  base: "./",
  plugins: [react()],
  build: {
    outDir: fileURLToPath(new URL("./out/renderer", import.meta.url)),
    emptyOutDir: true,
    sourcemap: true,
    target: "esnext",
  },
});
