# Masaüstü görünümü: taslağa birebir

**Goal:** Masaüstünü onaylı taslağın ([`2026-10-30-filo-taslak.html`](../specs/2026-10-30-filo-taslak.html)) birebir görünümüne geçirmek. Kullanıcı 31 Ekim'de taslağın ekran görüntüsünü gönderip "birebir" dedi; kapsam ilk plandan büyüdü, spec'in 3. parçasının arayüzü de buraya girdi.

**Architecture:** Yalnız `desktop/` değişir; sunucu ve sidecar aynı kalır. Çağrılar zaten hedef başına olduğu için pano mevcut `status`, `apps.list`, `alarms.list` ve `audit.list` ile çalışır. Arayüz React'te kaldığı için Wails geçişine taşınır.

**Spec:** [`2026-10-30-filo-design.md`](../specs/2026-10-30-filo-design.md) "Görsel dil", "3. Masaüstünde filo ekranı".

## Ölçümler (31 Ekim, Windows 11 yapı 26300)

- **Yazı tipi:** taslak bu makinede Inter ile değil Segoe UI ile çiziliyor (ölçülen genişlik 418,5 = Segoe, Inter yüklü değil); mono değerler Consolas. Beğenilen görünüm ve KΛDRΛN yazısı budur. Inter indirilmez; yığın `"Segoe UI", -apple-system, system-ui`; kod yığını `Consolas, "JetBrains Mono Bundled"`.
- **Pus:** Acrylic bu makinede çalışıyor; Mica neredeyse opak. Clear'da pus imkânsız: `backdrop-filter` masaüstünü bulanıklaştıramaz.
- **Renk lekeleri:** ekran görüntüsündeki kırmızı, mavi, turuncu lekeler taslağın kendi zemini, kullanıcının duvar kâğıdı değil. Birebir için bu zemin uygulamaya girer.

## Kararlar

| Konu | Karar |
|---|---|
| Pencere modları | Yeni varsayılan **Renkli cam** (`glass`): taslağın zemini uygulamanın içinde, her sistemde aynı. Acrylic, Mica, Vibrancy, Solid seçenek kalır. Clear kalkar |
| Logo | Segoe UI kalın, `.22em` aralıklı **metin**. Segoe'nun dış hatları yeniden dağıtılamaz, bu yüzden SVG'ye çevrilmez; macOS ve Linux'ta sistem yazı tipiyle çizilir |
| Pencere düğmeleri | Taslaktaki gibi solda üç renkli nokta, Windows ve Linux'ta çalışan düğmeler; macOS'ta sistemin kendi ışıkları |
| Kısayol etiketi | Windows ve Linux'ta `Ctrl B`, macOS'ta `⌘B` |
| Sahte veri | Yok. Bölge bilgisi sunucudan gelmediği için kartta yok; işletim sistemi, disk, uygulama sayısı, sürüm gerçek |
| Henüz işlevi olmayanlar | Yerinde ve soluk, "Yakında" ipucuyla: Ara, Anahtarlar, Bildirimler, Hepsini yükselt, TOTP ve oturum, Yükseltme, Budama |
| TOTP girişi gerekli kart | Taslaktaki "Kodu gir" yerine gerçek yol: terminalde `kadran login` |
| Sağlayıcı | Profilde elle seçilen etiket (spec 4. parça); dosya dışında sır yok |
| Yoklama | Filo ekranındayken 15 sn; pencere gizliyken yok |
| Yükseltmeler "Son işlemler"de | Yok: sunucuda kayıt bırakmıyorlar. Tablo dağıtım ve geri almaları gösterir |

## Bu planda OLMAYANLAR

Toplu yükseltme, boş VPS kurulumu, masaüstünde TOTP penceresi, ortak `servers.json`, Wails geçişi.

---

### Görev 1: Pencere modları
`appearance.ts`, `window-options.ts`, `window.ts`, `smoke.ts`, `mutate-desktop.sh`. Testler önce: `appearance-schema`, `window-options`. Varsayılan: `glass`, `tint 22`, `edge 16`.

### Görev 2: Saf filo mantığı
`lib/fleet.ts`, `lib/providers.ts`, `format.ts` `relativeTime`, `main/profiles.ts` `provider` alanı, `profile-store` `update`. Testler önce: `fleet.test.ts`, `format.test.ts`, `profiles.test.ts`, `profile-store.test.ts`.

### Görev 3: Stil
`tokens`, `base`, `glass`, `controls`, `focus`, `layout`, `screens`, `management`. Değerler taslaktan. Test önce: `style-rules.test.ts` (köşeler yalnız belirteçten, küçük öğede sabit beyaz yok, cam 30 px, kontrast sınırı, `prefers-reduced-transparency`).

### Görev 4: Kabuk ve ekranlar
`App.tsx`, `Rail`, `Tabs`, `TopBar`, `CrumbMenu`, `WindowLights`, `Backdrop`, `ServerDialog`, `KeyValues`, `LineIcon`; `useFleet`, `useRecentOps`; `screens/Dashboard`, `FleetLists`, mevcut ekranların düzeni. Bütün sunucu işlemleri, onaylar ve `data-testid`'ler aynı kalır. i18n iki dilde. Duman yolculuğu yeni gezintiye uyarlanır, pano iddiaları eklenir.

### Görev 5: Gerçek pencerede kabul
Sahte sidecar'la 1180×780 ve 760×520, koyu ve açık, Renkli cam ve Acrylic ekran görüntüleri; taslakla yan yana. Kullanıcı onayı. Bağımsız inceleme. K-161, spec ve marka notu. Push ve PR kullanıcı onayıyla.
