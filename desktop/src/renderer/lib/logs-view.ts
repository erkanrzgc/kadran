/** Günlük panelinin ne göstereceği: hata varken "günlük boş" iddiası yapılmaz. */
export type LogsBody =
  | { kind: "none" }
  | { kind: "empty" }
  | { kind: "text"; text: string };

/** Veri yoksa (yükleniyor ya da hata) hiçbir gövde çizilmez; "boş" yalnız veri gelip çıktı boşsa. */
export function logsBody(data: { output: string } | null | undefined): LogsBody {
  if (!data) return { kind: "none" };
  return data.output === "" ? { kind: "empty" } : { kind: "text", text: data.output };
}
