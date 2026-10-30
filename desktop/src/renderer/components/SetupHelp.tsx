import { useT } from "../i18n/I18nProvider.tsx";

export function SetupHelp() {
  const t = useT();
  return <div data-testid="setup-help"><ol className="setup-steps"><li><strong>{t("setup.server_title")}</strong><p>{t("setup.server_body")}</p></li><li><strong>{t("setup.ssh_title")}</strong><p>{t("setup.ssh_body")}</p></li><li><strong>{t("setup.key_title")}</strong><p>{t("setup.key_body")}</p></li><li><strong>{t("setup.trust_title")}</strong><p>{t("setup.trust_body")}</p></li></ol><p className="scope-note">{t("setup.error_help")}</p></div>;
}
