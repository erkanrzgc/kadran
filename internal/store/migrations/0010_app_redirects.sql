-- www eşi ve ek yönlendirme adları (proje B).
--
-- ── Mevcut uygulamalar KAPALI başlar ────────────────────────────────
--
-- Her iki sütun da varsayılanla eklenir: www = 0, redirects_json = '[]'.
-- Canlıdaki bir uygulamanın davranışı göçle DEĞİŞMEMELİ: eşi türetilip
-- rotalanırsa, o adı başka bir uygulama ya da dış bir kayıt kullanıyor
-- olabilir ve kullanıcının haberi olmadan yönlendirme başlardı. Açmak
-- bilinçli bir `app update -www` adımıdır.
--
-- www bir BAYRAK; eş ad domainden türetilir ve saklanmaz (domain taşınınca
-- eş de taşınsın diye). Açık adlar ise JSON dizisi: `env_json` ve
-- `volumes_json` ile aynı gerekçe, hiçbir zaman tek tek sorgulanmıyor.
ALTER TABLE apps ADD COLUMN www INTEGER NOT NULL DEFAULT 0 CHECK (www IN (0, 1));
ALTER TABLE apps ADD COLUMN redirects_json TEXT NOT NULL DEFAULT '[]';
