import type { Language } from "../shared/appearance.ts";

export function managementCancellation(language: Language): string {
  return language === "tr" ? "İşlemden vazgeçildi" : "operation cancelled";
}

export function managementConfirmation(language: Language, method: "apps.delete" | "apps.rollback", target: string, appId: string) {
  const tr = language === "tr";
  const action = method === "apps.delete" ? (tr ? "Uygulamayı sil" : "Delete application") : (tr ? "Önceki sürüme dön" : "Rollback application");
  return {
    type: "warning" as const,
    title: "Kadran",
    message: `${action}: ${appId}`,
    detail: tr ? `Sunucu: ${target}\nBu işlem seçili uygulamayı değiştirir.` : `Server: ${target}\nThis changes the selected application.`,
    buttons: [tr ? "Vazgeç" : "Cancel", action],
    defaultId: 0,
    cancelId: 0,
    noLink: true,
  };
}
