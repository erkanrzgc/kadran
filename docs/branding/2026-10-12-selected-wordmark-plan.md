# Seçilen KΛDRΛN yazı logosunu uygulama planı

**Goal:** Kullanıcının seçtiği 02, Bahnschrift Light SemiCondensed yazı logosunu
masaüstü ve README'de ana marka olarak kullanmak.

**Architecture:** Font dosyası dağıtılmaz; önceden çıkarılmış SVG path'leri
tema rengiyle görüntülenir. Native küçük simge aynı fontun K harfidir.
Kullanıcının mevcut yönetim/IPC değişiklikleri korunur; bu çalışma onları
commit etmez veya yeniden düzenlemez.

**Tech Stack:** React, CSS, SVG, Electron, fontTools ve ICO için Pillow.

- [x] Font 02 path'lerini daraltılmış viewBox ile public/docs varlıklarına kaydet;
  primary marka dosyası ve koyu sürümü true-alpha PNG olarak üret.
- [x] Titlebar eski torus ve tekrar eden plain text yerine tek renk uyumlu
  wordmark gösterir; 40px bar içinde yaklaşık 18px glyph yüksekliği kullanılır.
- [x] Aynı font K harfinden saydam native PNG/ICO üret; provenance ve mevcut
  paket simgesi kontrollerini yeni kaynağa bağla.
- [x] README `picture` ile koyu/açık tema sürümünü gösterir. Torus ana logo olmaz.
- [x] Electron smoke gerçek görüntünün yüklendiğini, görünür ölçüsünü ve iki
  tema rengini kontrol eder. Typecheck, build ve ilgili testler çalıştırılır.
- [x] Windows paketini üret ve pencere/ikon açılışını doğrula. GitHub'a yalnız
  yetkilendirilmiş logo/README değişikliği aktarılır; genel kod paketi push edilmez.
