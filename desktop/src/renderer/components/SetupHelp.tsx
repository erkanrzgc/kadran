import { useT } from "../i18n/I18nProvider.tsx";

export function SetupHelp() {
  const t = useT();
  return <div className="screen setup-screen" data-testid="setup-help"><section className="panel glass"><div className="panel-head"><div><h2 className="section-title">{t("setup.title")}</h2><p className="panel-sub">{t("setup.body")}</p></div></div><ol className="setup-steps"><li><strong>{t("setup.server_title")}</strong><p>{t("setup.server_body")}</p></li><li><strong>{t("setup.ssh_title")}</strong><p>{t("setup.ssh_body")}</p></li><li><strong>{t("setup.key_title")}</strong><p>{t("setup.key_body")}</p></li><li><strong>{t("setup.trust_title")}</strong><p>{t("setup.trust_body")}</p></li></ol><p className="scope-note">{t("setup.error_help")}</p></section></div>;
}
