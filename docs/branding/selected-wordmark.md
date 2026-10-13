# KΛDRΛN — seçilen marka

12 Ekim 2026. Kullanıcı üç font arasından **02: Bahnschrift Light
SemiCondensed** seçti. Eksenler `wght=300`, `wdth=87.5`; harf aralığı
`0.07 em`. İki Λ harfi de fontun kendi glifidir.

Ana logo yalnız KΛDRΛN yazısıdır. Masaüstü başlığındaki altı harf SVG
path olarak saklanır ve `currentColor` ile tema rengini izler. Font
kurulumu gerekmez; font dosyası dağıtılmaz. Native küçük simge aynı
fontun oranları korunmuş K harfidir; saydam 256px tuval ve `#5B96BA`
rengi kullanılır. Eski torus aktif marka olarak kullanılmaz.

## Son dosyalar

- [Açık renk SVG](../assets/kadran-wordmark.svg) ve [PNG](../assets/kadran-wordmark.png)
- [Koyu renk SVG](../assets/kadran-wordmark-dark.svg) ve [PNG](../assets/kadran-wordmark-dark.png)
- [Native simge kaynağı](../../desktop/assets/kadran-icon.svg),
  [PNG](../../desktop/public/kadran-icon.png), [ICO](../../desktop/assets/kadran.ico)
- [Koyu uygulama](selected-desktop-dark.png) ve [açık uygulama](selected-desktop-light.png)

PNG'ler 2344 × 560 RGBA'dır. İki renk sürümünün alfa geometrisi aynıdır;
alfa aralığı 0–255, dört köşe tam saydamdır. SVG'lerde arka plan yoktur.
README `picture` öğesiyle sayfa temasına uygun SVG'yi seçer.

Tema görünürlüğü: açık temada logo tam siyah (`#000000`), koyu temada
açık renk (`#EDF1F5`) kullanır. Logo rengi vurgu, parlama ve arayüz fontu
ayarlarından bağımsızdır. Başlığın %70 opak cam zemini, Clear modunda pencere
renk tonu sıfırken de duvar kâğıdı üzerinde kontrastı korur; tam opak bir
zemin eklenmez. Ayrı PNG/SVG varlıkları arka plansız kalır.

Siyah SVG, GitHub README için de
[`7579a5b`](https://github.com/erkanrzgc/kadran/commit/7579a5b192aa139d4a709b9ee75aec7852fc958d)
ile güncellendi. Gerçek Electron CSS ölçümlerinden hesaplanan en kötü
kontrast koyu temada beyaz masaüstü üzerinde 6,30:1, açık temada siyah
masaüstü üzerinde 9,35:1. Smoke iki ton ve dört mevcut vurgu rengini
`tint=0` ile kontrol eder; test değişiklikleri kaydedilmez ve sonunda
geri alınır. Bu oranlar CSS bileşimi hesabıdır; harici duvar kâğıdı
ekran görüntüsü ölçümü değildir. Clear yolculuğu gerçek renderer ve
saydam piksel kontrolleriyle ayrıca geçti.

GitHub `main` dalında yalnız README başlığı ve iki SVG yayımlandı:
[logo değişikliği](https://github.com/erkanrzgc/kadran/compare/138b7ccccca66ce1250c349f88664c4818581d93...512884d284a4989c6f045090d23db26401da98f5).
Genel ürün kodu veya başka çalışma ağacı değişikliği GitHub'a aktarılmadı.

`python docs/branding/select-wordmark.py`, yerel Windows fontu ve fontTools
ile SVG varlıklarını ve React komponentini tekrar üretir. PNG çizimi
`render-wordmark.cjs`, ICO üretimi `desktop/scripts/make-windows-icon.py`
ile yapılır. Native simge SHA-256 kayıtları `desktop/assets/icon-provenance.json`
içindedir; PNG/ICO dönüştürme komutları `desktop/assets/README.md` içindedir.

## Doğrulama

- `npm run typecheck` ve `npm run build`: geçti.
- `node --test test/packaging-icon.test.ts test/i18n.test.ts`: 11/11 geçti.
- `npm run test:built`: 4/4 geçti.
- `KADRAN_SMOKE_MODE=clear npm run e2e`: gerçek Electron'da tüm yolculuk
  geçti. Tek logo, görünür SVG geometrisi, iki tema rengi, minimum pencere
  taşma kontrolleri ve saydam cam yüzeyler ölçüldü. Sahte sidecar kullanıldı;
  canlı sunucuya işlem yapılmadı.
- `npm run dist` ve paketlenmiş EXE smoke: geçti. Pakette yeni native PNG ve
  iki SVG mevcut, `node_modules` yok; EXE içinden çıkarılan K simgesi görsel
  olarak kontrol edildi. Paket smoke çıkışında Chromium iki GPU teardown
  tanısı yazdı; renderer, logo, preload ve Node izolasyonu kontrolleri geçti.
- Windows %150 ekran ölçeğinde saydam pencere 760 yerine 761 CSS piksel
  açılıyor. Test yalnız bu ölçümde ±1 piksel tolerans kullanır; gerçek
  yatay taşma kontrolleri korunur.

Genel `npm test` mevcut yönetim çalışma ağacında tamamlanamadı:
`management-core.test.ts` eşzamanlı işlem testinde bekliyor.
`management-ipc.ts` içindeki aynı uygulama için ikinci işlemi reddeden
guard'ın yerinde `void key` var; bu satır `scripts/mutate-desktop.sh` içindeki
gate-disabled mutasyonuyla eşleşiyor. Önceden mevcut bu dosyalara logo
çalışmasında dokunulmadı. Bu nedenle tüm yönetim testlerinin geçtiği veya
yerel geliştirme paketinin yayın adayı olduğu iddia edilmez.
