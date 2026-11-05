# Filo 2. parça: ortak sunucu listesi

Üst tasarım: [`2026-10-30-filo-design.md`](2026-10-30-filo-design.md), "2. Ortak sunucu listesi".
Bu belge o bölümü uygulanabilir hâle getirir. Kullanıcı soruları atlayıp taslağı istedi (5 Kasım);
aşağıda **(spec)** üst tasarımdaki kararı, **(benim kararım)** bu belgede verdiğim kararı gösterir.

## Neden

Bugün sunucu listesi iki yerde, ikisi de yarım:

- CLI'da liste yok. Her komuta hedef elle yazılıyor (`kadran status kadran-client@1.2.3.4`).
  Yükseltmede root ya da sudo hedefi ve `-sudo` her seferinde hatırlanıyor.
- Masaüstü kendi listesini Electron'un veri dizininde (`%AppData%\kadran-desktop\profiles.json`)
  tutuyor. CLI onu görmüyor, masaüstü de CLI'ın bildiğini görmüyor.

4. parça ("+ Sunucu ekle") ve 5. parça ("Hepsini yükselt") tek bir listeye dayanıyor. Bu parça o
listeyi kurar. Sunucuya ve canlıya dokunmaz: değişiklik yalnız iş istasyonunda.

## Amaç

- Tek dosya: `servers.json`. CLI de masaüstü de yalnız onu kullanır.
- CLI'da `kadran server add|list|rm|rename` ve her komutta `-s <ad>` **(spec)**.
- Yükseltme ve anahtar işleri ad ile: `kadran bootstrap -s hetzner` kayıttaki yönetici hedefini ve
  `-sudo`'yu kullanır **(benim kararım; spec yönetici hedefini kayda koyuyor, kullanımını 5. parçaya
  bırakıyordu)**.
- Masaüstü kendi listesini bir kez bu dosyaya taşır **(spec)**.

## Kapsam dışı

- Anahtar dosyası yolu. Spec bunu kayda koyuyordu; bugün her bağlantı ssh-agent'ı ve varsayılan
  anahtarları kullanıyor, `-i` hiç yok. İki anahtarı 4. parça üretiyor; alan onunla gelir
  **(benim kararım)**. Dosya biçimi sürümlü olduğu için sonradan eklemek kolay.
- Kadran'ın kendi `known_hosts`'u, parmak izi onayı, bağlantı denemesi: 4. parça.
- Masaüstünde TOTP kodu penceresi: 3. parça.
- Sunucuya yeni çağrı. Protokol değişmez (3).

## Asla

- Dosyada anahtar, parola ya da jeton yok **(spec)**. TOTP jetonları yine `sessions/` altında.
- Ad, yazana göre başka bir sunucuya çözülmez: tam eşleşme, büyük/küçük harf duyarlı, önek yok.
  Yanlış sunucuya dağıtım bu parçanın en kötü hatası olur.
- Elle düzenlenmiş bir dosya `ssh`'a seçenek sokamaz (bkz. Güvenlik).

## Dosya

**Yer (spec):** `os.UserConfigDir()/kadran/servers.json`, TOTP oturumlarının (`sessions/`) yanı:

| Sistem | Yol |
|---|---|
| Windows | `%AppData%\kadran\servers.json` |
| Linux | `~/.config/kadran/servers.json` (`$XDG_CONFIG_HOME` varsa orası) |
| macOS | `~/Library/Application Support/kadran/servers.json` |

Spec macOS için `~/.config` diyordu; Go'nun verdiği yer bu ve oturumlar zaten orada.

**İzin:** dizin 0700, dosya 0600 (oturumlarla aynı). Windows'ta `%AppData%` zaten kullanıcıya özel.

**Biçim (benim kararım):**

```json
{
  "version": 1,
  "servers": [
    {
      "name": "hetzner",
      "provider": "Hetzner",
      "target": "kadran-client@203.0.113.10",
      "admin": "root@203.0.113.10"
    },
    {
      "name": "gcp",
      "provider": "Google Cloud",
      "target": "kadran-client@198.51.100.7",
      "admin": "ops@198.51.100.7",
      "sudo": true
    }
  ]
}
```

- `name` ve `target` zorunlu; `provider`, `admin`, `sudo` isteğe bağlı.
- Sıra kullanıcının ekleme sırası; `list` ve masaüstü bu sırayı gösterir.
- `version` 1'den büyükse dosya okunur ama yazılmaz: "bu dosyayı daha yeni bir Kadran yazmış"
  hatası. Eski bir CLI bilmediği alanları silerek dosyayı bozmasın **(benim kararım)**.
- Bilinmeyen alan (aynı sürümde) okumada yok sayılır, yazmada düşer.
- Dosya yoksa liste boş; bu hata değil. Bozuk dosya hatadır ve yolu söylenir; Kadran onu
  sessizce boş listeyle EZMEZ (masaüstünün bugünkü "bozuksa boş başla" davranışı burada
  tehlikeli: ilk ekleme bütün listeyi silerdi).

### Kayıt kuralları

- **Ad (benim kararım):** 1–64 karakter. Harf, rakam, boşluk, `-`, `_`, `.` serbest; denetim
  karakteri yok; baş ve sondaki boşluk kırpılır; `-` ile başlayamaz (`-s -foo` bayrak gibi
  okunmasın). Benzersiz, ve benzersizlik büyük/küçük harfe bakmaz: `Hetzner` ile `hetzner` aynı
  anda olamaz. Arama ise tam eşleşme.
- **Hedef:** `client.ParseTarget`'tan geçmeli; aynı hedef iki kez olamaz (masaüstü bugün de
  hedefi anahtar sayıyor). Yerel soket (`unix://…`) kabul edilir, sunucunun kendisinde işe yarar.
- **Yönetici hedefi:** `admin` yalnız SSH hedefi olabilir; `bootstrap`'ın bugünkü root hedefi
  denetiminden geçer. `sudo: true` bugünkü `-sudo` bayrağının karşılığı.
- **Sağlayıcı:** serbest metin, en çok 64 karakter (masaüstündeki sınır).
- **Sayı:** en çok 100 sunucu (masaüstündeki sınır).

## CLI

### `kadran server`

```
kadran server add [-provider P] [-admin root@host] [-sudo] <ad> <hedef>
kadran server list
kadran server rm <ad>
kadran server rename <eski> <yeni>
```

- `add` ağa çıkmaz, yalnız kaydeder **(benim kararım)**. Bağlantıyı denemek 4. parçanın işi;
  bugün `kadran status -s <ad>` aynı denetimi yapıyor. Var olan ad ya da hedefle `add` hata verir.
- `list` sütunlar: AD, SAĞLAYICI, HEDEF, YÖNETİCİ (sudo'luysa `ops@… (sudo)`). Liste boşsa nasıl
  ekleneceğini söyler. Çıktı İngilizce (CLI'ın geri kalanı gibi).
- `rm` yalnız kaydı siler: sunucuya ve kayıtlı oturuma dokunmaz. Oturum 8 saat sonra kendiliğinden
  geçer; hemen silmek için `kadran logout`.
- Alan değiştirmek için ayrı komut yok (spec'teki dört komut). Hedef ya da yönetici değişince
  `rm` + `add`; ad için `rename`. Masaüstünde düzenleme var (aşağıda).

### `-s <ad>`

- Bütün komutların ortak bayrak kümesine (`newFlagSet`) eklenir; her komutta seçeneklerle aynı
  yerde: `kadran status -s hetzner`, `kadran deploy -s gcp blog`.
- `-s` ile konumsal hedef birlikte verilirse kullanım hatası. Konumsal hedef bugünkü gibi çalışır
  **(spec: eski biçim hedef çalışmaya devam eder)**. Konumsal argüman ad olarak ARANMAZ:
  `hetzner` bir `~/.ssh/config` takma adı da olabilir, ikisi karışmasın **(benim kararım)**.
- Ad bulunamazsa hata ve kayıtlı adlar listelenir; benzer ad önerilmez, seçilmez.
- Oturum (TOTP) jetonu çözülmüş hedefe bağlı: `-s hetzner` ile `kadran-client@203.0.113.10`
  aynı jetonu kullanır. Oturum hatasındaki ipucu, `-s` ile çağrıldıysa `kadran login -s hetzner`
  der.
- Yönetici yolunu kullanan komutlar (`bootstrap`, `key …`, `totp reset`) `-s` ile `admin`'i ve
  `sudo`'yu alır. `admin` yoksa hata: kaydın yönetici hedefi olmadığını ve nasıl ekleneceğini söyler.
  `-s` ile ayrıca `-sudo` verilirse kayıttaki değer yerine bayrak geçerli olur.

## Masaüstü

Masaüstü dosyayı kendisi okuyup yazmaz; yan süreçteki (`kadran sidecar`) Go kodu okur ve yazar
**(benim kararım)**. Dosya biçimini tek bir paket (`internal/servers`) bilir. Electron'dan Wails'e
geçilirse aynı paket doğrudan çağrılır.

Yeni yan süreç yöntemleri:

| Yöntem | Ne yapar |
|---|---|
| `servers.list` | Listeyi döner. |
| `servers.add` | `{name, target, provider?}` ekler. |
| `servers.update` | Hedefle bulunan kaydın `name` ve `provider`'ını değiştirir (bugünkü düzenleme penceresiyle aynı: hedef sabit). |
| `servers.remove` | Hedefle bulunan kaydı siler. |
| `servers.import` | Göç için: verilen profilleri listede olmayanlar için ekler (aşağıda). |

- Her yöntem diske yazmadan önce dosyayı yeniden okur. Masaüstü bütün listeyi geri yazmaz; CLI'ın
  o arada eklediği kayıt böylece silinmez. Bugünkü `ProfileStore` arayüzü (`add`, `remove`,
  `update`) aynı kalır, altındaki IO bu yöntemlere bağlanır.
- Masaüstü listeyi pencere odağa gelince yeniden okur: CLI'da eklenen sunucu kart olarak görünür.
- Masaüstünün düzenleme penceresi `admin`'i göstermez ve silmez (yalnız ad ve sağlayıcı).

### Göç

İlk açılışta, Electron veri dizininde `servers-imported` işaret dosyası yoksa:

1. `profiles.json` okunur (bugünkü doğrulamayla).
2. `servers.import` her profili ekler. Hedef zaten listede varsa atlanır. Ad kurala uymuyorsa
   düzeltilir: boşluk kırpılır, 64 karaktere kısaltılır, baştaki `-` atılır; ad başka bir hedefte
   kullanılıyorsa `ad (2)`, `ad (3)` … olur **(benim kararım)**.
3. Başarılıysa işaret dosyası yazılır. `profiles.json` silinmez ve değişmez: v0.10.0'a geri dönen
   biri listesini yine görür (o andan sonraki eklemeler hariç).
4. Hata olursa işaret yazılmaz, bir sonraki açılışta yeniden denenir; kullanıcıya bildirim çıkar.

Göç bir kez olur; sonrasında `profiles.json`'a ne yazılırsa yazılsın okunmaz.

## Eşzamanlı yazma

- Yazma atomik: aynı dizinde geçici dosya, `fsync`, `rename` **(spec)**. Dosya hiçbir anda yarım
  olmaz.
- Okuma-değiştirme-yazma arasında başka bir süreç yazarsa son yazan kazanır **(spec)**. Pencere
  milisaniyeler; tek kullanıcılı iş istasyonunda kabul edildi.
- Windows'ta `rename` hedef dosya başka bir süreçte açıkken düşebilir. Kısa aralıklarla birkaç
  deneme, sonra açık hata **(benim kararım; ölçülecek)**.

## Güvenlik

- Dosya kullanıcının kendi dosyası, ama elle ya da başka bir araçla düzenlenebilir. Okunan her
  `target` ve `admin` yazmadakiyle aynı denetimden geçer (`ParseTarget`, `-` ile başlayan kullanıcı
  ya da sunucu reddi). `-oProxyCommand=…` gibi bir değer `ssh`'a ulaşmaz; kayıt reddedilir ve
  hangi kayıt olduğu söylenir (ssh argüman enjeksiyonu sınıfı).
- Yan süreç yöntemleri bugünkü gibi bilinen alanlar dışında her şeyi reddeder.
- Dosyada sır yok; izinler yine 0600.

## Test ve ölçüm

- `internal/servers`: okuma/yazma, kurallar, `version` > 1'de yazma reddi, bozuk dosyanın ezilmemesi,
  izinler (Linux'ta 0600/0700), seçenek gibi görünen hedefin reddi, iki sürecin aynı anda yazması
  (dosya hep geçerli JSON).
- CLI: dört komut, `-s` çözümü, `-s` + konumsal hedef hatası, yönetici yolunda `admin` yokken hata,
  oturum ipucu.
- Masaüstü: göç (çakışan ad, aynı hedef, bozuk `profiles.json`, işaretin yalnız başarıda yazılması),
  odakta yeniden okuma, sahte yan süreçte yeni yöntemler; uçtan uca yolculukta "CLI'da eklenen
  sunucu kartta görünür".
- **Mutasyon (yeni betik):** ad aramasının tam eşleşmesi (önek ve harf duyarsız eşleşme mutantları
  yakalanmalı), `-s` + hedef dışlaması, `admin`'in `bootstrap`'a geçmesi, okunan hedefin denetimi,
  yeni sürüm dosyasında yazma reddi.

## Belgeler

README'nin CLI bölümü (`server`, `-s`), masaüstü rehberi (liste artık CLI ile ortak, dosyanın
yeri), CHANGELOG. Karar kaydı: K-162.

## Uygulayıcıya kalan

- Tablo çıktısının biçimi, hata metinlerinin tam sözü.
- `internal/servers`'ın iç API'si (ör. `Load`, `Save`, `Add` …) ve masaüstü IPC adları.
- Windows `rename` yeniden deneme sayısı ve aralığı (ölçerek).
