# Mimari Kararlar

Panely'nin şekline yön veren kararlar ve — mümkün olduğunda — onları
destekleyen **ölçülmüş kanıt**. "Böyle olması lazım" ile "denedim, böyle
oluyor" arasındaki fark burada kayıt altına alınır.

---

## K-001 — Kontrol düzlemi masaüstü + CLI, web paneli yok

**Karar.** Panely bir web uygulaması değil; sunucuda `panelyd` daemon'ı,
iş istasyonunda Electron GUI ve `panely` CLI çalışır. Bağlantı SSH üzerinden.

**Gerekçe.** Web paneli tüm bir güvenlik sınıfını beraberinde getiriyordu:
çerez, CSRF, SameSite, XSS, oturum jetonu, CORS. Masaüstü istemcide bunların
hiçbiri yok. Kimlik doğrulama SSH anahtarına iner ki bu zaten sunucudaki en
güçlü kimlik katmanıdır.

**Sonuç.** Şartname §9.3'teki "frontend Vercel'de dursun ki sunucu çökünce
panel ayakta kalsın" maddesi gereksizleşti — masaüstü uygulaması zaten
kullanıcının makinesinde.

**Açık varsayım.** Sunucu ölünce uygulama ayakta kalır ama yönetecek bir API
kalmaz. Gerçek felaket *yönetimi* Faz 5'te yedek düğümün de `panelyd`
çalıştırmasını gerektirir. O zamana kadar §9 "gözlem + geri yükleme"
seviyesindedir, "canlı devralma" değil.

---

## K-002 — Üç binary, üç yetki seviyesi

**Karar.** `panelyd` yetkisiz (kullanıcı `panely`), `panely-exec` ayrıcalıklı
(root) ve aralarındaki sözleşme tipli bir protobuf şeması.

**Gerekçe.** Docker soketine erişim pratikte root yetkisidir. "Panel root
çalışmasın" şartını gerçekten karşılamanın tek yolu, ayrıcalıklı yüzeyi
denetlenebilir küçük bir binary'ye hapsetmektir. Coolify/Dokploy dahil
mevcut panellerin hiçbirinde bu ayrım yok.

**Değişmez.** `internal/exec` 2000 satırı geçerse ne eklendiği sorgulanır.
Ayrıcalıklı yüzey büyüdükçe "en az yetki" iddiası anlamını yitirir.

---

## K-003 — SSH erişimi zorlanmış komutla, soket yönlendirmesiyle değil

**Karar.** İstemcinin `authorized_keys` girdisi:

```
command="/usr/local/lib/panely/panely-connect",restrict ssh-ed25519 AAAA...
```

**Reddedilen alternatif.** İlk taslakta `direct-streamlocal@openssh.com` ile
unix soketi yönlendirmesine izin verilmesi düşünülmüştü.

**Neden reddedildi.** OpenSSH'ta unix soketi yönlendirmesini açmak
`port-forwarding` iznini gerektirir; bu da istemciye sunucudaki **her TCP
portuna** tünel açma yetkisi verir — örneğin `localhost:5432`'deki
veritabanına. `restrict` + zorlanmış komut ile bu sınıf tamamen kapanır:
anahtar yalnızca `panely-connect` binary'sini çalıştırabilir, o da sokete
bayt taşımaktan başka bir şey yapmaz.

**Yan fayda.** OpenSSH'ın ince yönlendirme semantiğine hiç bağımlı değiliz.
Faz 7'deki veritabanı tünelleri de SSH yönlendirmesi yerine denetim
günlüğüne yazılan bir RPC üzerinden geçecek.

---

## K-004 — İki ayrı unix grubu: `panely` ve `panely-client`

**Karar.**

| Yol | Sahiplik | Mod | Erişebilen |
|---|---|---|---|
| `/run/panely-exec/` | `root:panely` | 0750 | yalnızca panelyd |
| `/run/panely-exec/exec.sock` | `root:panely` | 0660 | yalnızca panelyd |
| `/run/panely/` | `panely:panely-client` | 0750 | panelyd + istemci |
| `/run/panely/api.sock` | `panely:panely-client` | 0660 | panelyd + istemci |

**Yakalanan kaçak.** Tek grup kullanılsaydı, istemci SSH kullanıcısı
`api.sock`'a erişebilmek için `panely` grubunda olmak zorunda kalırdı — ve
aynı grup `exec.sock`'u da açtığı için istemci **panelyd'yi atlayıp doğrudan
executor'a** bağlanabilirdi. İki grup bu yolu kapatır.

**Bootstrap değişmezi.** `SO_PEERCRED` yalnızca sürecin **birincil** gid'ini
bildirir, ek grup üyeliklerini değil. Bu yüzden istemci kullanıcısı
`useradd -g panely-client` ile oluşturulmalıdır, `-G` ile değil. Yanlış
yapılırsa hata mesajı çıkmaz; her bağlantı sessizce reddedilir.

---

## K-005 — Kontrol veritabanı SQLite, Postgres değil

**Karar.** `modernc.org/sqlite` (saf Go, CGO'suz), WAL modu,
`_txlock=immediate`.

**Gerekçe.** Kontrol düzleminde yüksek eşzamanlı yazma yok. Postgres ayrı bir
servis, ayrı bir yedekleme yolu ve bir tavuk-yumurta problemi getirirdi:
"Panely'nin veritabanını Panely mi yönetsin?" SQLite gömülüdür, yedeği tek
dosyadır ve Faz 5'te Litestream ile R2'ye sürekli replike edilebilir.

**CGO'suz olması şart.** Windows iş istasyonundan `linux/amd64` ve
`linux/arm64` hedeflerine çapraz derleme buna bağlı; `panely bootstrap`'in
"tek komut kurulum" vaadi de öyle.

**Ölçülmüş kanıt.** `_txlock=immediate`'in sürücü tarafından gerçekten
ayrıştırıldığı doğrulandı: `_txlock=zirvana` verildiğinde sürücü
`unknown _txlock "zirvana"` hatası döndürüyor, yani değeri yok saymıyor.
(Bilinmeyen *diğer* parametreler sessizce yok sayılıyor, bu yüzden bu
kontrol anlamlıydı.)

---

## K-006 — Denetim zinciri kanonik JSON değil, uzunluk-önekli ikili kodlama

**Karar.** `audit.ComputeHash`, alanları uzunluk öneki ile SHA-256'ya besler.

**Gerekçe.** JSON kanonikleştirmesi ince tuzaklarla dolu: anahtar sıralaması,
unicode kaçışları, ondalık gösterim, boşluk. Farklı bir kütüphane sürümü aynı
kayıt için farklı bayt üretip zinciri sebepsiz kırabilir.

Uzunluk öneki ayrıca klasik zincir hatasını engeller: önek olmadan
`("ab","c")` ile `("a","bc")` aynı baytlara serileşir ve iki farklı kayıt aynı
hash'i üretir. Bu, bir saldırganın alan sınırlarını kaydırarak eylemin
anlamını değiştirmesine izin verirdi.

**Test.** `TestLengthPrefixPreventsFieldBoundaryCollision`.

---

## K-007 — Executor'ın günlüğü düz dosya, SQLite değil

**Karar.** `/var/lib/panely/exec-audit.log`, satır başına bir JSON kaydı,
0640 `root:panely` (panelyd okur, **yazamaz**).

**Gerekçe.** SQLite'ı executor'a bağlamak ~200 bin satırlık bir SQL motorunu
ayrıcalıklı sürecin içine sokardı. Dosya tabanlı günlük ~200 satır. K-002'nin
değişmezi burada test edildi ve kolaylık için taviz verilmedi.

**Neden ayrı bir günlük?** panelyd'nin ele geçirilmesi tehdit modelinin
merkezinde. Kayıtlar yalnızca panelyd'de tutulsaydı, ele geçirilmiş bir
panelyd kendi yaptığı ayrıcalıklı çağrıları hiç kaydetmeyebilirdi.

**Okuma anında yeniden doğrulama.** `Journal.Read`, açılıştaki doğrulamaya
güvenmez; her çağrıda zinciri seq 1'den yeniden doğrular. Aksi hâlde
executor çalışırken dosyaya eklenen sahte bir satır "gerçek" gibi teslim
edilir ve panelyd'nin çapraz doğrulaması iki doğrulanmamış zinciri
karşılaştırmış olurdu — yani hiçbir şey kanıtlamazdı.
Test: `TestReadRejectsLineForgedWhileOpen`.

---

## K-008 — §1.1'deki seccomp ifadesinin yerine konan gerçek kontroller

**Şartnamede yazan.** "Çekirdek seviyesinde süreçlerin yetkisiz bellek veya
doğrudan donanım alanlarına erişimini engelleyen kısıtlama kuralları."

**Sorun.** seccomp *sistem çağrılarını* filtreler; bellek veya donanım
erişimini değil. Madde olduğu gibi uygulanabilir değil.

**Yerine konan.** İş yükü konteynerlerinde Docker varsayılan seccomp profili
+ `no-new-privileges` + düşürülmüş yetenekler. Daemon'ın kendisinde systemd
sıkılaştırması: `NoNewPrivileges`, `ProtectSystem=strict`,
`CapabilityBoundingSet=`, `SystemCallFilter=@system-service`,
`MemoryDenyWriteExecute`, `PrivateDevices`, `RestrictAddressFamilies`.

Özel bir seccomp katmanı **yazılmayacak**.

---

## K-009 — gRPC unix soketi sertleştirme altında çalışıyor (ölçüldü)

**Endişe.** `RestrictAddressFamilies=AF_UNIX` panelyd'nin `exec.sock`'a
bağlanmasını kırabilir mi? Go'nun çözümleyicisi bazı yollarda `AF_NETLINK`
kullanır.

**Ölçüm.** WSL Ubuntu (systemd çalışır durumda) üzerinde bir gRPC sunucu +
istemci sondası, `panelyd.service`'in kısıtlarıyla çalıştırıldı:
`RestrictAddressFamilies=AF_UNIX`, `NoNewPrivileges`,
`SystemCallArchitectures=native`, `SystemCallFilter=@system-service`,
`SystemCallFilter=~@privileged @resources @obsolete @mount @debug
@cpu-emulation @swap @raw-io`, `MemoryDenyWriteExecute`, `LockPersonality`.

**Sonuç.** Her iki bağlanma yöntemi de başarılı:

| Yöntem | Kısıtsız | Kısıtlı |
|---|---|---|
| `unix://` hedefi, gRPC çözümleyicisi | bağlandı | bağlandı |
| `passthrough:///` + özel dialer | bağlandı | bağlandı |

**Karar.** Endişe gerçekleşmedi, ancak kodda yine de **passthrough + özel
dialer** kullanılacak. Bedeli sıfır ve çözümleyiciyi tamamen devreden
çıkararak bir bilinmeyeni kalıcı olarak siliyor.

---

## K-011 — Uygulanmamış RPC derlemeyi kırar (ölçüldü)

**Karar.** `buf.gen.yaml`'da `require_unimplemented_servers=false` — gRPC
varsayılanının **tersi**. Sunucular `UnimplementedXxxServer` yapısını gömmez.

**Gerekçe.** Varsayılan (`true`) ileriye uyumluluk sağlar: şemaya yeni bir RPC
eklendiğinde mevcut sunucular derlenmeye devam eder ve yeni metot sessizce
`codes.Unimplemented` döner. `exec.proto` bir güvenlik sınırıdır; oraya
ayrıcalıklı bir yetenek eklenip uygulamasının unutulması sessiz bir boşluktur.

**Ölçüm.** Şemaya `TehlikeliYeniYetenek` adında bir RPC eklendi, uygulaması
yazılmadı, `buf generate` çalıştırıldı:

```
cmd/panely-exec/main.go:128:49: cannot use service (variable of type
*exec.Server) as panelyv1.ExecutorServiceServer value: *exec.Server does not
implement panelyv1.ExecutorServiceServer (missing method TehlikeliYeniYetenek)
```

Derleme kırıldı, değişiklik geri alındı.

**Bedeli.** İleriye uyumluluk kaybı. Panely'de üç binary birlikte sürümlenip
`panely bootstrap` ile birlikte kurulduğu için bu bedel yok sayılabilir; sürüm
uyumu ayrıca `version.Protocol` sabitiyle çalışma anında denetlenir.

---

## K-010 — `buf` uzak eklentileri değil, yerel eklentiler

**Karar.** `buf.gen.yaml` yalnızca `local:` eklentiler kullanır.

**Gerekçe.** buf'ın `remote:` eklentileri `.proto` tanımlarını kod üretimi
için buf.build sunucularına yükler. `exec.proto` projenin güvenlik sınırıdır
ve yetki modelinin tamamını tarif eder. Üçüncü taraf bir servise
gönderilmesi için hiçbir gerekçe yok.

---

## K-012 — Yerel istemci kimlik önsözünü kendisi yazar (hata düzeltmesi)

**Karar.** `panely` yerel unix soketine bağlanırken kimlik önsözünü
(`connproto.Identity{Origin: "local"}`) kendisi yazar. SSH yolunda YAZMAZ —
orada önsözü sunucudaki `panely-connect` yazıyor.

**Bulunan hata.** `internal/client` yerel yolda hiç önsöz yazmıyordu, oysa
`api.callerCreds.ServerHandshake` önsözü koşulsuz okuyor. Sunucuda argümansız
`panely status` — yani birincil kullanım — gRPC'nin HTTP/2 önsözünü uzunluk
sanıp ölürdü: `PRI ` dizisi big-endian okunduğunda 1.347.703.584 eder ve
`ErrPreambleTooLarge` döner. Operatörün gördüğü mesaj `broken pipe` idi;
gerçek nedenle hiçbir ilgisi yok.

**Ölçüm.** Önsöz yazımı geçici olarak devre dışı bırakıldı ve testler gerçek
Linux'ta koşturuldu:

```
--- FAIL: TestLocalDialWritesIdentityPreamble    önsöz hiç gelmedi
--- FAIL: TestGRPCWorksOverRealSocketAfterPreamble
    rpc error: code = Unavailable desc = write unix ...: broken pipe
```

Yazım geri konunca ikisi de geçti.

**Önsöz neden dialer'ın İÇİNDE yazılıyor?** gRPC bağlantı kurucusunu bağlantı
başına çağırır: ilk bağlantıda ve kopma sonrası her yeniden bağlanmada
(GOAWAY, geçici hata). `Dial()` içinde bir kez yazmak ilk bağlantıda çalışır,
sonrakilerin hepsinde sessizce bozulurdu. `TestReconnectWritesPreambleAgain`
sunucuyu kasten düşürüp bunu doğruluyor.

**Bu, önsözü uydurulabilir yapmıyor mu?** Hayır. Önsözün bütünlüğü "api.sock'a
yalnızca panely-connect yazabilir" varsayımına dayanmıyor; "`SSH_AUTH_INFO_0`'ı
yalnızca sshd ayarlayabilir" varsayımına dayanıyor. İstemci kullanıcısı olarak
rastgele kod çalıştırabilen biri, `panely-connect`'i düzmece bir ortamla
çağırarak istediği kimliği zaten yazdırabilirdi — yerel yol yeni bir saldırı
yüzeyi açmıyor. Bu yüzden `Origin` sabit tutuldu ve geçersiz kılacak bir
bayrak konmadı: kimlik uydurmak bir sömürü adımı olarak kalmalı, hazır bir kod
yolu haline gelmemeli.

**Asimetri korunmalı.** SSH yolunda istemci de önsöz yazsaydı panelyd iki
önsöz görürdü: ilkini okur, ardından HTTP/2 beklediği yerde dört baytlık bir
uzunluk artı JSON bulurdu. `TestSSHTransportWritesNoPreamble` sahte bir `ssh`
alt süreciyle boruya yazılan ilk baytları yakalayıp HTTP/2 önsözü olduğunu
doğruluyor.

---

## K-013 — Zincir doğrulaması üç durumlu, iki bool değil

**Karar.** `VerifyAuditChainResponse`'taki `valid` ve `executor_chain_valid`
bool alanları kaldırıldı (1 ve 5 `reserved`), yerlerine `ChainStatus` enum'u
geldi: `VALID`, `INVALID`, `UNREACHABLE`. `version.Protocol` 1 → 2.

**Gerekçe.** "Doğrulanamadı" ile "geçersiz" AYRI durumlardır ve operatörün
tepkisi tamamen farklıdır:

- `UNREACHABLE` bir işletim sorunudur (executor kapalı, veritabanı okunamıyor)
  ve zincir hakkında hiçbir şey söylemez.
- `INVALID` kurcalama şüphesidir ve araştırılmalıdır.

Eski kod erişilemeyen executor'ı `executor_chain_valid = false` diye
raporluyordu. Cron'a konulan bir `panely audit verify`, executor'ın kapalı
olduğu her an sahte bir güvenlik alarmı üretirdi — ve tekrarlayan sahte
alarmların sonu, gerçek olanın da yok sayılmasıdır.

Daemon tarafında ayrım `errors.Is(err, audit.ErrChainBroken)` ile yapılıyor:
zincir bütünlüğü hatası `INVALID`, veritabanı G/Ç hatası `UNREACHABLE`.

**Çıkış kodlarına yansıması.** `panely audit verify`:

| Durum | Kod |
|---|---|
| iki zincir de `VALID` | 0 |
| herhangi biri `INVALID` | 3 |
| kurcalama yok ama biri doğrulanamadı | 1 |

Kurcalama, erişilememeyi bastırır: gerçek bulgu öncelikli.

**Alan silmek neden protokol sürümünü artırdı?** `version.Protocol` kuralı
"alan eklemek artırmaz, silmek veya anlamını değiştirmek artırır" diyor. Henüz
dağıtılmış bir sunucu olmasa da kuralın kendisine uyulmasının nedeni budur:
istisna tanınan bir kural, kural değildir.

---

## K-014 — CLI uçtan uca gerçek daemon'a karşı doğrulanıyor

**Karar.** `scripts/e2e-cli.sh`, gerçek `panelyd`'yi WSL'de başlatıp gerçek
`panely` ile konuşur. Root gerektirmez.

**Gerekçe.** Birim testleri istemciyi sahte bir sunucuya karşı sınıyor. Sahte
sunucu, gerçeğinin yaptığı iki şeyi yapmıyor: `SO_PEERCRED` ile çağıranı
doğrulamak ve kimlik önsözünü okumak. İkisi de yalnızca Linux'ta çalışır ve
K-012'deki hata tam olarak bu boşlukta yaşıyordu.

Betik istemci grubu olarak kullanıcının kendi birincil grubunu kullanıyor;
üretimde bu `panely-client` olur. Ayrıcalık izolasyonunun root gerektiren
doğrulaması ayrı kalıyor (`scripts/e2e-executor.sh`).

**Doğrulanan halkalar.** unix soketi → SO_PEERCRED → kimlik önsözü → gRPC →
SQLite denetim zinciri → CLI çıktısı ve çıkış kodu. Ayrıca sidecar'ın stdio
JSON-RPC yolu gerçek sunucudan yanıt alıyor.

---

## K-015 — CI güvenlik kontrolleri, kendileri sınanmadan sayılmaz

**Karar.** Ayrıcalıklı yüzey denetimleri `scripts/check-exec-surface.sh`'a
taşındı ve o betiğin KENDİSİ `scripts/check-exec-surface-test.sh` ile
sınanıyor. CI önce testi, sonra kontrolü çalıştırır.

**Neden.** İlk sürümde kontroller CI YAML'ının içine gömülüydü ve biri
sessizce yanlış çalışıyordu: yorum satırlarını da tarıyordu. `exec.proto`
gelecekteki alan şekillerini yorum içinde örnekliyor —

```
//     repeated string command = 4;  // argv — konteyner İÇİNDE, kabuk yok
```

— ve tarama bunu gerçek bir alan sanıp temiz ağaçta hata veriyordu. Yanlış
alarm veren bir kontrol kapatılmaya mahkûmdur; kapatıldığında da geriye
yeşil bir rozet ve hiçbir koruma kalmaz.

Ters yön daha sinsi: hiçbir zaman ATEŞLENMEYEN bir kontrol de aynı yeşil
rozeti verir ve kimse fark etmez. Bu yüzden test betiği kasten bozulmuş
şemalar üretiyor ve kontrolün onları yakaladığını doğruluyor:

```
✓ gerçek exec.proto temiz
✓ yorumdaki örnekler yanlış alarm üretmiyor
✓ privileged alanı yakalandı
✓ cap_add alanı yakalandı
✓ serbest argv yakalandı
✓ serbest kabuk alanı yakalandı
✓ satır sınırı uygulanıyor
```

**Kontrol ettiği değişmezler.** Ayrıcalıklı kodun 2000 satırı geçmemesi
(K-002); `privileged`, `cap_add`, `host_network` gibi seçeneklerin şemada
TEMSİL EDİLEMEMESİ; serbest argv/kabuk alanı bulunmaması. Temsil edilemeyen
bir seçenek, doğrulanan bir seçenekten güçlüdür — kazara kabul edilemez.

---

## K-016 — Üretilen kod depoda tutulmuyor, CI her işte üretiyor

**Karar.** `*.pb.go` `.gitignore`'da. CI'daki her iş, ortak bir bileşik
eylemle (`.github/actions/setup`) önce `buf generate` çalıştırır ve
üretimin gerçekten dosya yazdığını doğrular.

**Gerekçe.** Üretilen kodu depoda tutmak, şema ile kodun sessizce ayrışması
demektir: biri `.proto`'yu değiştirip `buf generate` çalıştırmayı unutursa
derleme geçer ve fark edilmez. Üretmeden derlenemeyen bir ağaçta bu sınıf
hata imkânsız.

Bedeli: her CI işine bir üretim adımı. Bu bedel, eklentilerin YEREL
kurulmasıyla birlikte ödeniyor (K-010) ve üretim boş çıkarsa adım hemen
hata veriyor — yoksa sonraki adımlar anlaşılmaz derleme hatalarıyla patlardı.

**Yan bulgu.** `internal/exec/dockerprobe.go` linux'a etiketlenirken
`DefaultDockerSocket` sabiti de içeride kalmıştı ve Windows derlemesi
kırıldı. Ayrım kuralı `internal/exec/docker.go`'ya yazıldı: platforma özgü
olan DAVRANIŞTIR, yapılandırma değil.

---

## K-017 — Profiller anahtarlıkta değil düz JSON'da

**Karar.** Masaüstü uygulaması sunucu profillerini `app.getPath('userData')`
altında düz JSON olarak saklar. Plandaki "OS anahtarlığı" maddesi düşürüldü.

**Gerekçe.** Bu tasarımda bir profil `panely-client@1.2.3.4` gibi bir hedef
dizesinden ibaret ve İÇİNDE SIR YOK. Kimlik doğrulamayı `ssh` yapıyor;
anahtar ssh-agent'ta ya da `~/.ssh` altında duruyor ve uygulama onu hiç
görmüyor. Anahtarlık, sıfır kazanç karşılığında bir arıza kipi eklerdi:
anahtarlık sıfırlandığında çözülemeyen bir bloba dönüşen profiller.

---

## K-018 — `bootstrap` tek tar akışı gönderir, betiği uzakta çalıştırır

**Karar.** Kurulum için gereken her şey (üç binary, systemd birimleri,
tmpfiles, istemci açık anahtarı, kurulum betiği) bellekte bir tar'a
konur ve TEK bir SSH bağlantısında gönderilir.

**Gerekçe.** Dosya başına `scp` hem yavaş hem de yarım kalmaya açık:
üçüncü dosyada kopan bir bağlantı, yarı kurulmuş bir sunucu bırakır.
Betik paketin içinde olduğu için uzak kabuk komutu dört satırda kalıyor
ve betik değiştiğinde güncellenmesi gerekmiyor.

**Mimari önce sorulur.** `uname -m` ile sunucunun mimarisi öğrenilip
eşleşen binary gönderiliyor. Yanlış mimari "exec format error" ile ölür
ve neden günlükte kolayca gözden kaçar; sormak bu sınıfı tamamen siler.

**Özel anahtar kontrolü.** `--client-key`'e kazara özel anahtar verilmesi
felaket olurdu: sunucuya yüklenir ve `authorized_keys`'e yazılırdı.
`validatePublicKey` bunu yakalıyor ve `TestRejectsPrivateKey` sınıyor.

---

## K-019 — İstemci kullanıcısının kabuğu `nologin` OLAMAZ

**Bulgu.** İlk tasarımda `panely-client` kullanıcısına — daemon kullanıcısı
gibi — `nologin` verilecekti. Bu, taşımayı tamamen kırardı.

sshd zorlanmış komutu (`command="..."`) kullanıcının GİRİŞ KABUĞU üzerinden
çalıştırır: `$SHELL -c "<komut>"`. `nologin` ise ne verilirse verilsin
reddeder. Sonuç: her bağlantı, sebebi belirsiz biçimde kapanırdı.

**Karar.** İstemci kabuğu `/bin/sh`. Hesabı kısıtlayan şey kabuk değil,
`authorized_keys`'teki `command=...,restrict` ikilisi: istemci ne isterse
istesin yalnızca `panely-connect` çalışır, pty/port yönlendirme/ajan
yönlendirme kapalıdır.

`TestInstallScriptDoesNotUseNologinForClient` bu tuzağı bekliyor.

---

## K-020 — Kurulum kendi değişmezlerini kurulum sonunda ÖLÇER

**Karar.** `install.sh` bitmeden önce beş kontrol yapar ve biri bile
başarısızsa kurulumu başarısız sayar:

1. panelyd `panely` kullanıcısı olarak mı çalışıyor (root DEĞİL)
2. `panely` kullanıcısı Docker'a erişebiliyor mu (ERİŞEMEMELİ)
3. `api.sock` = 660 panely:panely-client
4. `panely-client` `exec.sock`'a erişebiliyor mu (ERİŞEMEMELİ)
5. `authorized_keys` zorlanmış komut içeriyor mu

Ayrıca kullanıcılar oluşturulduktan hemen sonra iki değişmez doğrulanır:
`panely-client`'ın BİRİNCİL grubu `panely-client` mi, ve `panely`
grubunda DEĞİL mi.

**Gerekçe.** "Kurulum başarılı" mesajı, kurulan şeyin iddia edilen
özellikleri taşıdığı ölçülmeden verilemez. Bu kontroller olmadan yanlış
bir `useradd -G` sessizce geçer ve sorun ancak ilk bağlantı denemesinde,
hiçbir açıklama olmadan görünür.

---

## K-021 — Ayrıcalık izolasyonu ÖLÇÜLDÜ: SO_PEERCRED gerçekten reddediyor

**Durum: doğrulandı** (2026-08-03, WSL Ubuntu, gerçek Linux çekirdeği).

Ürünün merkezî iddiası buydu ve şimdiye kadar yalnızca birim testleriyle
destekleniyordu. Kök E2E testi artık koştu: **7 doğrulamanın 7'si geçti.**

**Kritik olan doğrulama.** Soket dizini `0755`, soket `0666` yapıldıktan
sonra — yani dosya izinleri savunma olmaktan çıktıktan sonra — yetkisiz
kullanıcı hâlâ reddediliyor:

| Kullanıcı | İzinler | `connect()` | Yanıt |
|---|---|---|---|
| yetkisiz | 0666 | **başarılı** | `ConnectionResetError` |
| izinli | 0666 | başarılı | **15 bayt** (gRPC SETTINGS) |

İki kullanıcı arasındaki TEK fark uid/gid. İzinler birebir aynı. Farklı
sonuç, ayrımın gerçekten çekirdekten gelen kimliğe dayandığını kanıtlıyor.

**Testin naif hâli boş yere geçiyordu.** Önceki sürüm "bağlanamadıysa
geçti" diyordu. Ama `connect()` işletim sistemi izinleri yüzünden
başarısız olsaydı SO_PEERCRED hiç devreye girmemiş olurdu — test,
kanıtladığını sandığı şeyi kanıtlamazdı. Şimdi `connect()`'in BAŞARILI
olması zorunlu; engellendiyse test "geçersiz" sayılıyor.

**Testin kendisi de sınanıyor.** `PANELY_E2E_ALLOW_INTRUDER=1` ile
executor davetsiz kullanıcıyı kabul edecek şekilde başlatılıyor ve
doğrulamanın gerçekten KALDIĞI görülüyor:

```
[KALDI]  İZİNLER GEVŞEKKEN YETKİSİZ ÇAĞIRAN KABUL EDİLDİ — SO_PEERCRED çalışmıyor
[KALDI]  izinli kullanıcı da reddedildi — executor herkesi reddediyor olabilir
==> Sonuç: 5 geçti, 2 kaldı
```

CI her iki geçişi de koşuyor: önce testin bozuk politikayı yakaladığı,
sonra gerçek doğrulama.

---

## K-022 — WSL'de root için `sudo` gerekmiyor: `wsl.exe -u root`

**Bulgu.** Kök E2E testi "sunucu gelene kadar bekliyor" sanılıyordu,
çünkü WSL'de parolasız `sudo` yok. Bu yanlış bir çıkarımdı:
`wsl.exe -d Ubuntu -u root` doğrudan root veriyor ve `sudo`'ya hiç
uğramıyor. PID 1 de systemd.

Yani test ilk günden çalıştırılabilirmiş. Engel teknik değil, varsayımdı.

**Ders.** "X yok" ile "X'e giden tek yol kapalı" aynı şey değil. Bir
doğrulama engellenmiş görünüyorsa, engelin kendisi de ölçülmeli.

**Testler `/mnt/c` üzerinde DEĞİL `/tmp` altında koşuyor:** `/mnt/c`
Windows dosya semantiği taşır ve doğrulanan şey tam olarak unix
izinleri.

---

## K-023 — CI'ın ilk koşusu gerçek bir güvenlik açığı buldu: grpc 1.70 → 1.82.1

**Bağlam.** Boru hattı yazılmıştı ama hiç çalışmamıştı: uzak depo yoktu.
İlk gerçek koşuda `govulncheck` iki açığı çağrı iziyle raporladı.

```
GO-2026-4762  Authorization bypass in gRPC-Go via missing leading slash in :path
              Found in: google.golang.org/grpc@v1.70.0   Fixed in: v1.79.3
              internal/grpcserve/serve.go:35: grpcserve.Run calls grpc.Server.Serve

GO-2026-6061  xDS RBAC + HTTP/2 transport server
              Found in: google.golang.org/grpc@v1.70.0   Fixed in: v1.82.1
```

`GO-2026-4762` bu projede sıradan bir bağımlılık uyarısı değil.
Panely'nin tüm modeli executor'ın YALNIZCA beyaz listedeki RPC'leri kabul
etmesine dayanıyor; `:path` üzerinden yetkilendirme atlatma o beyaz
listenin altını oyan sınıf. Şema-beyaz-liste tasarımı, altındaki gRPC
yönlendirmesi doğru çalıştığı sürece anlamlı.

**Karar.** grpc v1.82.1'e yükseltildi (ikisini birden kapatan alt sınır).

**Doğrulama.** `govulncheck ./...` → "No vulnerabilities found", çıkış 0.
12 paketin tamamı `-race` ile geçiyor. Bu ikincisi önemsiz değil:
1.70 → 1.82 büyük bir sıçrama ve `peercred`'in dayandığı
`credentials.TransportCredentials` el sıkışma yolu tam orada yaşıyor.

**Ders.** Hiç çalıştırılmamış bir CI, yeşil rozet bile üretmez — hiçbir
şey üretmez. Boru hattının değeri yazıldığı an değil, ilk koştuğu an
başlıyor.

---

## K-024 — `ssh` argüman enjeksiyonu: `-` ile başlayan hedef reddediliyor

**Bulgu.** golangci-lint v2 yükseltmesi G204 bayrağı kaldırdı. İnceleme
gerçek bir açığa çıktı.

`ssh` kabuk üzerinden çağrılmıyor; argümanlar exec'e dizi olarak
veriliyor. Bu kabuk enjeksiyonunu TAMAMEN kapatır — ve tam da bu yüzden
insanı rahatlatıp ikinci sınıfı gözden kaçırtıyor: `-` ile başlayan bir
konumsal argümanı `ssh` SEÇENEK olarak okur, `-oProxyCommand=<komut>`
iş istasyonunda keyfî yerel komut çalıştırır.

`ParseTarget` ilk `@`'te böldüğü için kullanıcı adı saldırganın
denetimindeydi. Kırmızı test bunu doğrudan yazdırdı:

```
seçenek benzeri hedef kabul edildi: -oProxyCommand=touch /tmp/pwned@sunucu
(ssh'a geçecek argüman: "-oProxyCommand=touch /tmp/pwned@sunucu")
```

**"Kullanıcı kendi ayağına sıkar" savunması geçerli değil.** Hedef dizesi
yalnızca komut satırından gelmiyor: sidecar hedefleri GUI profillerinden
alıyor. Kaynağı operatörün kendi yazdığı komut olmayan bir yol var.

**Karar.** İki katmanda reddediliyor: `ParseTarget` (erken, anlaşılır
hata) ve `dialSSH` (çözümlemeyi atlayan, doğrudan kurulan `Target` için).
Bootstrap'ta hedef tek parça argüman olduğu için kontrol de tek satır.

**Neden `--` ile ayırmak değil?** `--` desteği OpenSSH sürümüne göre
değişir. Taşınabilir ve kesin olan, girdiyi kaynağında reddetmek. Meşru
hiçbir kullanıcı veya sunucu adı `-` ile başlamaz.

**Ölçüm düzeneğinin kendisi de sınandı.** Sahte `ssh` artık aldığı argv'yi
kaydediyor. Negatif test "argv dosyası yok ⇒ exec edilmedi" diyor; bu
BOŞ YERE geçebilirdi, çünkü alt süreç dosyayı eşzamansız yazıyor ve
"henüz yazmadı" da dosyasız görünür. Pozitif kontrol AYNI bekleme
süresiyle dosyayı görüyor — yani yokluğu artık bir şey ifade ediyor.
(Aynı hata sınıfı K-021'de yakalanmıştı.)

---

## K-025 — Linter sabitlenirken sürüm yetmez, derleme araç zinciri de sabitlenmeli

**Bulgu.** `.golangci.yml` v1.62.2 ile yerelde doğrulanmıştı. CI ilk
koştuğunda aynı sürüm, aynı yapılandırma, farklı sonuç:

```
can't load config: the Go language version (go1.23) used to build
golangci-lint is lower than the targeted Go version (1.25.0)
```

golangci-lint KENDİSİNİN derlendiği Go sürümünü `go.mod`'un hedefiyle
karşılaştırıyor. Yereldeki kurulum `go install` ile, yani yerel araç
zinciriyle (go1.26.5) derlenmişti ve geçiyordu. CI ise yayınlanmış
binary'yi indiriyor — o go1.23 ile derlenmiş.

**Ders.** Bir aracın *sürümü* ile *nasıl elde edildiği* ayrı iki
değişken. "Yerelde çalışıyor" burada bilgi taşımıyordu.

**Karar.** golangci-lint v2.12.2 + action v9. Üçü tek küme olarak hareket
ediyor: action v6 yalnızca v1'i, v7+ yalnızca v2'yi destekliyor ve
`.golangci.yml` şeması da sürüme bağlı.

**Doğrulama varsayımla değil ölçümle:** CI'ın indireceği linux-amd64
artefaktı indirilip `--version` çalıştırıldı → `built with go1.26.2`.

**Yükseltmenin bulduğu 5 gerçek sorun** (v1.62 hiçbirini görmüyordu):
argüman enjeksiyonu (K-024), sarmalanmış hatada `==` karşılaştırması, ve
`panely-connect`'te zaman aşımsız `Dial` — ayrıcalıklı vekil yolunda
askıda kalan bir bağlantı SSH oturumunu süresiz açık tutuyordu.

**Bilerek ertelendi.** v2 göçü, v1'de ÖRTÜK olan varsayılan hariç
tutmaları (`legacy`, `common-false-positives`) açık hâle getirdi. Bunlar
bir kısım gosec bulgusunu susturuyor ve güvenlik sınırı olan bir projede
ayrıca gözden geçirilmeli. Göçle sıkılaştırmayı aynı değişikliğe koymak,
CI kızardığında sebebi ayırt edilemez kılardı.

---

## K-026 — Gerçek sunucuda ilk koşu dört hata buldu

`panely bootstrap` haftalarca yazılı ve testli durdu, ama hiç gerçek
sunucuda çalıştırılmadı. İlk koşuda **dört ayrı hata** çıktı; hiçbiri
birim testleri, WSL veya CI ile yakalanamazdı.

| # | Belirti | Kök neden |
|---|---|---|
| 1 | `226/NAMESPACE`, birim hiç başlamıyor | `BindReadOnlyPaths=/run/docker.sock` — taze sunucuda Docker yok, `-` öneki eksikti |
| 2 | `2/INVALIDARGUMENT`, sonsuz yeniden başlatma | Birim `--config /etc/panely/panelyd.toml` geçiyordu; ne bayrak ne dosya vardı |
| 3 | `31/SYS` (SIGSYS), çekirdek öldürüyor | `~@privileged` chown ailesini kapatıyordu; daemon api.sock'un grubunu ayarlayamıyordu |
| 4 | `error reading server preface: EOF` | ssh alt süreci gRPC'nin deneme bağlamına bağlıydı ve el sıkışmadan sonra öldürülüyordu |

**Neden hiçbiri yakalanamamıştı.** Birim testleri binary'leri sınıyordu;
birim DOSYALARINI kimse sınamıyordu (1, 2). Seccomp ve systemd ad alanı
yalıtımı yalnızca gerçek çekirdekte kurulur (1, 3). SSH taşıması ilk kez
gerçekten kullanıldı; testler `dialSSH`'ı iptal edilmeyen bağlamla
çağırıyordu (4).

**Kabul ölçütü servisin ayakta olması DEĞİL.** 3. hatadan sonra bootstrap
"Kurulum tamamlandı" dedi ve kurulum sonrası altı kontrolün hepsi geçti —
ama istemci hâlâ bağlanamıyordu (4. hata). "Servis active" ile "istemci
konuşabiliyor" farklı iddialar; doğrulama ikincisini ölçmeli.

**Sonuç (46.225.95.35, Ubuntu 24.04, cx23/nbg1):**

```
panely status panely-client@<ip>        → tam çıktı, çıkış 0
panely audit verify panely-client@<ip>  → daemon GEÇERLİ (2 kayıt),
                                          executor GEÇERLİ (0 kayıt)
panely bootstrap root@<ip>  (2. kez)    → idempotan, servis kesintisiz
```

CAX11 (ARM) seçilmişti ama Hetzner'da üç EU lokasyonunda da kapasite
yoktu; cx23 aynı özellikleri (2 vCPU / 4 GB / 40 GB) daha ucuza veriyor
(8,05 vs 8,67 EUR/ay). Tek fark x86; ARM kapasitesi dönünce geçilebilir.

---

## K-027 — systemd birim dosyaları artık test ediliyor

1. ve 2. hatalar aynı boşluktan geldi: **binary sınanıyordu, birim
dosyası sınanmıyordu.** İkisi de çevrimdışı doğrulanabilir gerçekler.

`internal/bootstrap/units_test.go`:

- `TestUnitsDoNotHardRequireForeignPaths` — Panely'nin oluşturmadığı bir
  yola zorunlu bind kurulmuş mu (kural: `-` öneki şart).
- `TestDockerSocketBindIsOptional` — somut gerilemenin nöbetçisi.
- `TestUnitExecStartFlagsExist` — `ExecStart`'taki her `--bayrak`
  binary'de gerçekten tanımlı mı.

Üçü de düzeltme geri alınarak sınandı; hepsi hatayı yakalıyor. Her biri
"hiç bulgu yoksa test bir şey ölçmüyor" koruması taşıyor, çünkü dosya
taşınırsa sessizce geçmeleri mümkündü.

**Seccomp için karşılık gelen test YOK ve yazılmayacak.** `@privileged`
çıkarmasının chown'u kapattığı yalnızca gerçek çekirdek + gerçek systemd
ile görülebilir. Zayıf bir vekil test, olmayandan kötü olurdu.

---

## K-028 — Alt sürecin ömrü bağlantıya bağlanır, denemeye değil

`dialSSH`, ssh'ı `exec.CommandContext(ctx, ...)` ile başlatıyordu.
Buradaki ctx gRPC'nin **bağlantı denemesi** bağlamı; gRPC onu el sıkışma
biter bitmez iptal eder ve `CommandContext` iptalde süreci **öldürür**.
Yani ssh, bağlantı kurulur kurulmaz ölüyordu.

Belirti yanıltıcıydı: `error reading server preface: EOF` sunucuyu
suçluyor. Teşhisi ayıran ölçüm şuydu — aynı sunucuda `panely-client`
kullanıcısı YEREL sokete bağlanınca tam çıktı alıyordu. Daemon
sağlamdı, kopan taşımaydı.

Yerel yol neden etkilenmiyordu: unix soketine bağlandıktan sonra bağlamın
iptali bağlantıyı etkilemez. Bu asimetri hatayı SSH'a özel kıldı ve
gerçek sunucuya kadar gizledi.

**Karar.** `exec.Command` kullanılıyor; süreç `Close()` içinde toplanıyor,
süre sınırı dolarsa öldürülüyor. İki test bunu iki yönden kilitliyor:

- `TestSSHProcessSurvivesDialContextCancel` — iptalden sonra taşıma yaşar
- `TestSSHProcessDiesWhenConnectionCloses` — ama bağlantı kapanınca ölür

İkincisi olmadan birincisi "süreci hiç öldürme" diyerek de geçerdi ve her
bağlantı arkada asılı bir ssh bırakırdı.

---

## K-029 — Faz 1'in ilk dilimi: şema, sürücü değil

Faz 1'in sekiz maddesi tek pushta yapılsaydı CI kızardığında sebebi ayırt
etmek imkânsız olurdu. İlk dilim **yalnızca sözleşmedir**: konteyner yaşam
döngüsü RPC'leri, doğrulayıcılar, kaçış testleri ve doğrulayıp
`Unimplemented` dönen handler'lar. Docker'a dokunan tek satır yok.

Gerekçe sıralamada: şema güvenlik sınırının kendisi. Önce sabitlenirse
sürücü onu kazara genişletemez. Ters sırada yazılsaydı sürücünün ihtiyaç
duyduğu her alan şemaya "geçici olarak" eklenir ve beyaz liste erirdi.

### Şemadaki üç karar

**1. `container_id` kabul edilmez.** Serbest konteyner tutamağı, hostta
çalışan HERHANGİ bir konteynere root seviyesinde işaretçidir. Adresleme
`(app_id, release_id[, replica])` üçlüsüyle; executor bunu kendi
`panely.app_id=` etiketine çevirip yalnızca kendi konteynerlerine dokunur.

Bunun doğurduğu yükümlülük: **etiketle adreslenen her konteyner sayılabilir
ve silinebilir olmalı.** Aksi hâlde dağıtım ortasında çöken panelyd, bir
daha adresleyemeyeceği öksüz konteyner bırakırdı. Bu yüzden
`ContainerRemove` varsayılan olarak SÜRÜM düzeyindedir (replika daraltma),
ve `ContainerList` yalnızca `app_id` alır — hatta boş `app_id` ile
Panely'nin yönettiği tüm konteynerleri döner. Uygulama kaydı tamamen
kaybolsa bile temizlik yolu kapanmıyor.

**2. `image` alanı yok, etiket KURULUR.** `panely/<app_id>:<commit_sha>`.
Serbest imaj alanı, beyaz listenin tamamını anlamsız kılan tek alandı.
`commit_sha` yalnızca hex; `:` veya `/` girseydi etiket başka bir imaja
kayabilirdi.

⚠ `release_id ↔ commit_sha` bağı bir **daemon** değişmezidir. Executor'ın
veritabanı yok; R sürümünün X commit'ine karşılık geldiğini doğrulayamaz.
Ele geçirilmiş panelyd konteyneri `release=R` etiketleyip başka bir imaj
çalıştırabilir ve geri almayı sessizce bozar. Executor ikisini de günlüğe
yazıyor; sapma sonradan yakalanabilir. Bu, proto yorumunda açıkça yazılı —
okuyanın executor'ın zorladığını sanmaması için.

**3. Host yolu hiç temsil edilmez.** Yalnızca hacim adı. `mount_path`
konteyner içi ve `path.Clean(p) == p` şartına tabi: bu tek kontrol `..`,
`//`, `/a/./b` ve sondaki `/` durumlarını birlikte eler. Elenmeselerdi aynı
yeri gösteren iki farklı yazım, çakışma kontrolünü atlatırdı.

### Yüzey denetçisinde iki gerçek boşluk kapandı

Yasak alan deseni `[A-Za-z0-9_.]+` ile tip eşleştiriyordu ve **iki şekli
kaçırıyordu**:

| Alan tanımı | Eski desen | Yeni desen |
|---|---|---|
| `bool privileged = 1;` | YAKALAR | YAKALAR |
| `map<string,string> sysctls = 2;` | **KAÇIRIR** | YAKALAR |
| `optional bool privileged = 3;` | **KAÇIRIR** | YAKALAR |

İkisi de teorik değildi: bu dilim şemaya `map<string,string> env` ve
`optional uint32 replica` soktu, yani her iki şekil de artık gerçekten
kullanılıyor. Boşluk grep ile doğrudan ölçüldü (yukarıdaki tablo o ölçümün
çıktısı), sonra kapatıldı.

Yasak liste artık `--list-forbidden` ile dışa veriliyor ve test betiği her
öğe için ayrı kanıt üretiyor. **Kanıtsız desen eklemek yapısal olarak
imkânsız.** 17 desen, 17 kanıt, artı dört şekil kanıtı.

### Testlerin ateşlediği ölçüldü

Üç mutasyon uygulandı, üçü de yakalandı:

| Mutasyon | Sonuç |
|---|---|
| `path.Clean` kontrolü kaldırıldı | `/var/./lib` ve `/var/lib/app/..` kabul edildi → FAIL |
| Çakışma kontrolü kaldırıldı | iç içe bağlamalar kabul edildi → FAIL |
| `ContainerCreate` doğrulamayı atladı | kötü istek `Unimplemented` aldı → FAIL |

Üçüncüsü en önemlisi: diğer testlerin hepsi doğrulayıcıları DOĞRUDAN
çağırıyor, yani handler onları hiç çağırmasa da geçerlerdi.
`TestHandlersValidateBeforeAnythingElse` tam olarak o boşluğu kapatıyor —
kötü istek `InvalidArgument`, iyi istek `Unimplemented` almalı.

### Sürücü dilimine devredilen yükümlülükler

Doğrulayıcının sağlayamayacağı, çalışma anında verilen kararlar
`internal/exec/container.go` başında yazılı:

1. **İmaj asla çekilmez.** `panely/<app>:<sha>` kayıtsız bir addır; Docker
   çözemezse `docker.io/panely/<app>` olarak yorumlar. Engine API'nin
   `POST /containers/create` ucu kendiliğinden çekmiyor (404 dönüyor) —
   sürücü buna yaslanmalı, hiçbir yola pull koymamalı.
2. Ağ adı `panely-<app_id>` olarak kurulur, istekten alınmaz.
3. Hacimler `nodev,nosuid` ile bağlanmalı.
4. Etiket eşleşmesi tam olmalı.

`ImageBuild` kasten bu dilimde YOK: derleme bağlamını kimin çektiği
kararlaştırılmadı. panelyd'nin birimi `RestrictAddressFamilies=AF_UNIX`
taşıdığı için git çekemez; executor çekerse git URL'i root koduna ulaşır ve
`ext::sh -c` / `--upload-pack=` K-024'le aynı sınıftır.

Ayrıcalıklı kod: **1242 / 2000 satır.**

---

## K-030 — `buf generate` geçmesi `buf lint` hakkında hiçbir şey söylemiyor

K-029 push edildiğinde CI kırmızı döndü, yerelde her şey temizken:

```
proto/panely/v1/exec.proto:86:59: RPC response type "ContainerLogChunk"
should be named "ContainerLogsResponse" or
"ExecutorServiceContainerLogsResponse".
```

Yerelde `buf generate` koşturulmuştu, `buf lint` koşturulmamıştı. İkisi
farklı iş yapıyor: biri Go kodu üretiyor, diğeri API/stil kurallarını
zorluyor (`RPC_RESPONSE_STANDARD_NAME`). Generate'in geçmesi lint hakkında
bilgi taşımıyor.

Boşluk sekiz commit boyunca görünmedi çünkü Faz 0'dan sonra `exec.proto`'ya
yeni RPC eklenmemişti. İlk yeni RPC kümesi onu anında ortaya çıkardı.

**Bu, K-025'in aynısı.** Orada da "yerelde çalışıyor" bilgi taşımıyordu,
çünkü yerelde koşturulan şey CI'ın koşturduğu şey değildi. Kural:

> Yerel doğrulama listesi CI iş akışından TÜRETİLİR, hafızadan değil.

`.github/workflows/ci.yml`'deki "Biçim ve statik analiz" işi ne koşuyorsa
yerelde de o koşulmalı. Liste `CONTRIBUTING.md` ve PR şablonuna eklendi;
`buf lint` ve `buf format --diff --exit-code` artık açıkça yazılı.

Adlandırma değişikliği davranışı etkilemiyor — `ContainerLogChunk` →
`ContainerLogsResponse`, tek kullanıcısı `internal/exec/container.go`'daki
akış imzası.

---

## K-031 — `restrict`, ortam değişkeni geçirmeyi KAPATMAZ

`internal/sshenv` paketinin doküman yorumu şunu iddia ediyordu:

> `restrict` seçeneği user-rc, X11 ve ortam geçirmeyi
> (`PermitUserEnvironment`) kapatır

İkinci yarısı yanlış. sshd(8), restrict'i şöyle tanımlıyor:

> Enable all restrictions, i.e. disable port, agent and X11 forwarding, as
> well as disabling PTY allocation and execution of ~/.ssh/rc.

Ortam işleme bu listede yok. `environment="AD=deger"` seçeneğini kapatan
şey ayrı bir sshd_config yönergesidir: `PermitUserEnvironment`.

**Neden önemli?** panely-connect, denetim kaydının aktör kimliğini
`SSH_AUTH_INFO_0`'dan okuyor. sshd(8) `environment=` için "override other
default environment values" diyor — yani o seçenek açık olsaydı,
authorized_keys'e yazılan bir satır sshd'nin KENDİ yazdığı parmak izini
ezebilirdi. Denetim izi "kim yaptı" sorusuna yalan söylerdi.

Gerçek koruma iki yerden geliyordu ve ikisi de sshd VARSAYILANIYDI:

| Yol | Kapatan | Panely bunu pinliyor muydu? |
|---|---|---|
| `authorized_keys`'te `environment=` | `PermitUserEnvironment no` | Hayır — varsayılana güveniliyordu |
| İstemcinin `SendEnv`'i | `AcceptEnv` (varsayılan: hiçbiri) | Hayır — varsayılana güveniliyordu |

Yani model doğruydu ama **yanlış mekanizmaya atfediliyordu** ve hiçbir
yerde zorlanmıyordu. Bir dağıtımın genel yapılandırması
`PermitUserEnvironment yes` deseydi, `restrict` bunu geri almazdı.

**Düzeltme.** `PermitUserEnvironment no` bootstrap'ın sshd drop-in'ine
açıkça yazıldı. `AcceptEnv` BİLEREK yazılmadı: varsayılanı zaten
"hiçbirini kabul etme" ve yönerge eklemeli çalıştığı için boş bir değerle
sıfırlanamaz — yazılacak her isim yüzeyi yalnızca genişletirdi.

**Düzeltme sırasında ikinci bir hata yakalandı.** `PermitUserEnvironment`
ilk denemede `Match User panely-client` bloğunun İÇİNE konmuştu. O anahtar
kelime sshd_config(5)'in Match içinde izin verdiği listede DEĞİL; sshd
yapılandırmanın tamamını reddeder. install.sh yeniden yüklemeden önce
`sshd -t` çalıştırdığı için sunucu kilitlenmezdi — ama bootstrap taze bir
sunucuda ölürdü ve bunu ancak gerçek kurulumda görürdük.

Bu yüzden `TestSSHDDropInKeywordsAreValidInTheirScope` yazıldı: Match
sonrası satırların yalnızca Match'in izin verdiği alt kümeden olduğunu
denetliyor. Mutasyonla doğrulandı — yönerge Match'in içine taşındığında
test kırmızıya döndü.

**Sınıf.** Bu, K-025 ve K-030 ile aynı aile: "yerelde çalışıyor" veya
"varsayılan zaten doğru" ifadeleri, ölçülmediği sürece bilgi taşımıyor.
Buradaki ek ders şu: bir yorumun güvenlik özelliğini DOĞRU mekanizmaya
atfetmesi, özelliğin kendisi kadar önemli. Yanlış atıf, sonradan gelen
birinin gerçek korumayı kaldırmasına zemin hazırlar.

---

## K-032 — Redaksiyon iddia ediliyordu, uygulanmıyordu

`audit.Record.ParamsJSON` alanı şu yorumu taşıyordu:

```go
ParamsJSON string // sırlar redakte edilmiş hâlde
```

Kod tabanında redaksiyon yapan **hiçbir fonksiyon yoktu**. `grep -ri
'redact'` üç sonuç veriyordu ve üçü de test dosyalarındaki `[REDACTED]`
DİZGİ SABİTİYDİ — yani testler, içinde "[REDACTED]" geçen bir dizginin
tur attığını doğruluyordu. Hiçbiri redaksiyonun gerçekleştiğini
ölçmüyordu.

**Neden henüz zarar vermemişti?** Tek gerçek yazma yeri
`cmd/panelyd/main.go`'daki `daemon.start` kaydı ve o yalnızca sürüm
numarası taşıyor. İddia, taşıması gereken yükle hiç karşılaşmamıştı.

**Neden şimdi kritik hâle geldi?** Faz 1 dilim 1, şemaya
`ContainerCreateRequest.env` alanını `map<string,string>` olarak ekledi.
Sürücü dilimi bu haritayı denetime yazacak. Denetim zinciri
EKLE-SADECE'dir ve kayıtlar hash'lenir: oraya bir kez düz metin parola
yazılırsa **geri alınamaz** — silmek zinciri koparır.

**Uygulama.** `internal/audit/redact.go`:

| Fonksiyon | Politika | Kullanım |
|---|---|---|
| `RedactEnv` | **Varsayılan REDDET** — her değer gider, anahtar adları kalır | Konteyner ortam değişkenleri |
| `RedactSensitive` | Seçici — yalnızca adı sır ima eden anahtarlar | Karışık parametreler |
| `MarshalParams` | Belirlenimci JSON | Her ikisinin çıktısı |

Ortam değişkenlerinde neden sezgisel kullanılmıyor: adlandırmayı kullanıcı
seçiyor. `CONFIG` veya `SMTP_URL` masum görünür ve altında parola durabilir.
Sezgisel yalnızca anahtar adlarını bizim ürettiğimiz parametrelerde
güvenli.

İşaret değerin UZUNLUĞUNU da gizler: `"a"` ve 4096 baytlık bir anahtar aynı
`[REDACTED]`'a dönüşür. Uzunluk tek başına bilgidir.

**Ölçüm.** İki yönde de mutasyon uygulandı:

| Mutasyon | Sonuç |
|---|---|
| `RedactEnv` değerleri olduğu gibi bırakıyor | `TestRedactEnvRemovesEveryValue` KIRMIZI — "hunter2" sızdı |
| `IsSensitiveKey` her zaman `true` | `TestIsSensitiveKeyLeavesOrdinaryNames` KIRMIZI — PORT, HOST, LANG sır sayıldı |

İkinci mutasyon önemli: pozitif kontrol olmadan `return true` yazan bir
uygulama "hiçbir sır sızmıyor" testini geçerdi ve denetim kaydını tamamen
okunmaz hâle getirirdi. Redaksiyonda her iki yön de yanlış olabilir.

**Kural.** Bir alanın yorumu bir güvenlik özelliği iddia ediyorsa, o
özelliği zorlayan bir fonksiyon ADIYLA anılmalı. "Çağıran dikkat etsin"
demek yeterli değil — ama en azından dürüst; sessizce doğru varsaymak
değil.

---

## K-033 — Debug kipi: yetenek evet, varsayılan hayır

`panely-exec` günlük seviyesini `slog.LevelInfo` olarak SABİT tutuyordu;
`panelyd`'de `-debug` bayrağı vardı. Ayrıntılı günlük sunucuda hiç
açılamıyordu — çünkü systemd'nin başlattığı bir binary'ye bayrak eklemek
unit dosyasını düzenleyip `daemon-reload` yapmayı gerektiriyor.

`internal/logutil` üç binary için tek kural koydu: `-debug` bayrağı VEYA
`PANELY_DEBUG=1` ortam değişkeni. Ortam değişkeni, `systemctl
set-environment` veya bir drop-in ile bayraktan çok daha kısa bir yol.

**Varsayılan KAPALI ve öyle kalacak.** Gerekçe üslup değil güvenlik:
panelyd ve executor konteyner ortam değişkenlerini, istek parametrelerini
ve çağıran kimliklerini işliyor. Debug varsayılan açık olsaydı bunlar
systemd journal'ına düşerdi ve `journalctl` okuyabilen herkes görürdü —
SECURITY.md'de çizilen sınırın dışına taşardı.

**Debug, denetim kaydına yazılanı DEĞİŞTİRMEZ.** İki kanal ayrı tutuldu:
denetim zinciri ne olursa olsun aynı kaydı tutar, bayrak yalnızca stderr'e
giden ayrıntıyı belirler. Bağlamak, tanılama için açılan bir anahtarın
kalıcı ve hash'li kayda sır yazmasına yol açardı.

Tanınmayan değerler KAPALI sayılır: `PANELY_DEBUG=hayir` yazan biri kapalı
bekler. "Boş değilse aç" mantığı bunu sessizce açardı.

---

## K-034 — Ayrıcalıklı yüzey bütçesi, yüzeyin yarısını ölçüyordu

`check-exec-surface.sh` sabit bir yol listesi sayıyordu:

```bash
find "$REPO_ROOT/internal/exec" "$REPO_ROOT/cmd/panely-exec" -name '*.go' ...
```

Bu, K-002'nin değişmezini ("ayrıcalıklı kod 2000 satırı geçerse ne
eklendiği sorgulanır") **zorlanamaz** hâle getiriyordu: root süreç bu iki
dizinden fazlasını çalıştırıyor, ama sayaç geri kalanını görmüyordu.

Sonuç: yeni bir paket yazıp `cmd/panely-exec`'ten içe aktarmak, kodu
ayrıcalıklı sürecin içine sokuyor ama bütçeye hiç dokunmuyordu.

**Bu teorik değildi — aynı oturumda gerçekleşti.** Debug kipi için
`internal/logutil` eklendi ve `panely-exec` onu içe aktardı. Sayaç
1267'de kaldı. 64 satır root sürece girdi, bütçe kıpırdamadı.

**Düzeltme.** Sayılan küme artık binary'nin GERÇEK içe aktarma
grafiğinden türetiliyor:

```bash
go list -deps ./cmd/panely-exec | grep "^${module_path}/"
```

Gerçek rakam **2395** çıktı — eski ölçümün neredeyse iki katı:

| Paket | Satır |
|---|---|
| internal/exec | 1093 |
| internal/audit | 432 |
| internal/peercred | 215 |
| cmd/panely-exec | 174 |
| internal/pbconv | 144 |
| internal/sockets | 128 |
| internal/logutil | 64 |
| internal/grpcserve | 61 |
| internal/sdnotify | 53 |
| internal/version | 31 |

**Üretilen protobuf kodu (4057 satır) hariç tutuldu.** Gerekçe:
`internal/pb/**` exec.proto'dan mekanik türetiliyor, elle yazılmıyor ve
elle denetlenmiyor; bütçenin sorusu "root süreçte kaç satır İNSAN YAZIMI
kod var". Dışlama körü körüne değil — o dizindeki her dosyanın
`// Code generated ... DO NOT EDIT.` başlığı taşıdığı doğrulanıyor.
Aksi hâlde dizin, denetimden kaçmak için elle kod saklanacak bir yer
olurdu.

**Sınır 2000 -> 2600.** Denetlenen kod DEĞİŞMEDİ; değişen, ne kadarını
gördüğümüz. Bu bir bütçe gevşetmesi değil, yanlış bir taban çizgisinin
düzeltilmesi. Kalan pay (~205 satır) bilerek dar: Docker sürücüsü bu
sınıra çarpacak ve "ne ekliyoruz" tartışması tam orada yapılmalı.

**Ölçüm.** `internal/smuggled` adında 304 satırlık sahte bir paket
yazılıp `panely-exec`'ten içe aktarıldı:

| Mantık | Sonuç |
|---|---|
| Eski (sabit yol listesi) | 1269 satır — **GEÇERDİ** |
| Yeni (içe aktarma grafiği) | 2701 satır — **YAKALANDI** |

Yazımın ilk hâlinde ikinci bir hata vardı: `go list -deps` hedef paketi
zaten listelediği için `cmd/panely-exec` elle de eklenince iki kez
sayıldı (174 + 174). Paket dökümü basılmasaydı fark edilmezdi — sayaç
sadece "biraz yüksek" görünürdü.

**Sınıf.** [[surface-check-regex-gaps]] ile aynı: kontrolün KENDİSİ
hatalıydı ve yeşil rozet veriyordu. Kural genelleşiyor: bir güvenlik
kontrolünün kapsamı elle bakımı yapılan bir listeyse, o liste er ya da
geç gerçeklikten kopar. Kapsam mümkün olduğunca **türetilmeli** —
yasak alan listesi `--list-forbidden` ile testten, bütçe kapsamı
`go list -deps` ile derleyiciden.

---

## K-035 — Uzak git bağlamını kimse çekmiyor: BuildKit çekiyor

`ImageBuild` bir dilim boyunca ertelenmişti ve ertelenme gerekçesi bir
ikilem olarak yazılmıştı: "panelyd mi çeksin, executor mü?" panelyd'nin
birimi `RestrictAddressFamilies=AF_UNIX` taşıdığı için klonlayamıyordu;
executor çekerse git URL'i root koduna ulaşacaktı.

**İkilem yanlıştı.** İki gözden kaçan nokta:

1. `panely-exec.service` de `RestrictAddressFamilies=AF_UNIX` taşıyor.
   Yani "executor çeksin" seçeneği notların varsaydığından daha kötüydü:
   gevşetilecek olan ROOT sürecin ağ erişimiydi.
2. Uzak bağlamı BuildKit kendi çözüyor. Executor yalnızca unix soketine
   bayt yazıyor. **Hiçbir birimin ağa açılması gerekmiyor.**

### Ölçüm

Varsayılmadı. `unshare -n` ile bomboş bir ağ ad alanına konmuş bir
istemciden derleme başlatıldı — bu düzenek Panely'nin kısıtını doğru
taklit ediyor, çünkü unix soketi bir dosya sistemi nesnesi olduğu için
ad alanından etkilenmiyor.

**Pozitif kontrol önce koşturuldu.** O olmadan `unshare -n` sessizce
etkisiz kalsaydı "daemon çekti" derdik:

| Kontrol | Sonuç |
|---|---|
| Ağsız ad alanından `getent hosts github.com` | BAŞARISIZ (beklenen) |
| Ağsız ad alanından `git ls-remote` | BAŞARISIZ (beklenen) |
| Ağsız ad alanından `docker build <git-url>` | **Depo ÇEKİLDİ** |

Çekme kanıtı daemon çıktısından:

```
#1 [internal] load git source https://github.com/octocat/Hello-World.git
#1 0.039 Initialized empty Git repository in
         /var/lib/desktop-containerd/.../snapshots/635/fs/
#1 1.281 From https://github.com/octocat/Hello-World
#1 DONE 4.1s
ERROR: failed to solve: failed to read dockerfile: open Dockerfile: ...
```

Depo kasıtlı olarak Dockerfile'sız seçildi: hatanın TÜRÜ ayırt edici.
Ağ hatası gelseydi çekme istemci tarafında olurdu; "Dockerfile yok"
gelmesi çekmenin daemon tarafında BAŞARIYLA gerçekleştiğini kanıtlıyor.

Çıktı ayrıca git'in kendi mesajlarını taşıyor — yani BuildKit daemon
tarafında ROOT olarak `git` çalıştırıyor. Aşağıdaki tasarımı gerekli
kılan da bu.

### Tasarım: URL alınmaz, kurulur

Serbest bir URL dizgisi kabul edilseydi, o dizgi root bağlamındaki bir
git ayrıştırıcısına giden girdi olurdu. Bilinen kötüye kullanımlar:

| Girdi | Sonuç |
|---|---|
| `ext::sh -c '...'` | rastgele komut (git-remote-ext) |
| `ssh://...` | executor'ın SSH anahtarlarıyla kimlik |
| `git://...` | kimlik doğrulamasız, şifresiz taşıma |
| `https://user:token@...` | kimlik bilgisi provenance'a sızar (GHSA-gc89-7gcr-jxqc) |
| `<url>#<ref>:<subdir>` | depo kökü DIŞINA erişim (CVE-2026-33748) |

BuildKit bunların çoğunu kendi şema beyaz listesiyle (`http, https, ssh,
git`) reddediyor. **Ama bu bizim kontrolümüzde olmayan bir kodda, çalışma
zamanında yapılan bir savunma ve o kodun bu yıl çıkmış bir CVE'si var.**
Panely'nin duruşu "BuildKit doğruluyor, biz güvendeyiz" değil.

URL hiç alınmıyor; parçalardan executor kuruyor:

```
https://<host>/<owner>/<repo>.git#<commit_sha>
```

- Şema sabit `https` → `ext::`, `ssh://`, `git://` temsil edilemez
- Kullanıcı bilgisi alanı yok → URL'e gömülü kimlik bilgisi temsil edilemez
- `commit_sha` TAM 40 hane hex → iki nokta üst üste yok → subdir bileşeni
  temsil edilemez (CVE-2026-33748 sınıfı)
- Host beyaz listesi ŞEMADA DEĞİL, `-allow-git-host` bayrağında: ele
  geçirilmiş bir panelyd listeye ekleme yapamaz

`VolumeMount`'ta host yolu alınmamasıyla aynı karar: girdiyi doğrulamak
yerine hiç almamak, sınıfın tamamını siler.

**Dal adı da reddediliyor.** İki sebep: dal hareket eder ve geri alma
(§2.1) tekrarlanabilirliğe dayanır; ayrıca dal adları iki nokta
taşıyabilir ve fragment'in subdir bileşenini açan tam olarak budur.
Ek fayda: sha'ya sabitlemek, dilim 1'de belgelenen
`release_id ↔ commit_sha` boşluğunu daraltıyor — executor artık en
azından kendi içinde tutarlı (aynı doğrulanmış sha'dan hem derliyor hem
etiketliyor).

**Özel depolar desteklenmiyor** ve bu gizlenmiyor. Kimlik doğrulama alanı
bilerek yok: token alanı eklemek, sırrı bu mesajın kapatmak için var
olduğu yollara koymak demekti. Doğru yer Faz 2'nin kasası ve alan bir
token değil opak bir kasa referansı olacak. Faz 1 kabul ölçütü ("basit
bir depo → deploy") genel depolarla karşılanıyor.

### Ölçüm: mutasyonlar

| Mutasyon | Sonuç |
|---|---|
| Beyaz liste kontrolü kaldırıldı | `TestGitHostWhitelistIsEnforced` + `TestEmptyWhitelistDoesNotMeanAllowAll` KIRMIZI |
| `commit_sha` deseni dal adı kabul ediyor | `TestCommitSHAMustBeFullHex` KIRMIZI (`main`, `HEAD`, `<sha>:etc/passwd`) |
| Handler doğrulamayı atlıyor | `TestImageBuildHandlerValidatesFirst` KIRMIZI |

Üçüncüsü yine kritik: diğer testler doğrulayıcıları DOĞRUDAN çağırıyor,
handler onları hiç çağırmasa da geçerlerdi.

K-002 tripwire'ı da beklendiği gibi ateşledi: `ImageBuild` şemaya
eklenince `cmd/panely-exec` DERLENMEDİ (`missing method ImageBuild`).

### Sürücü diliminin devraldığı yükümlülükler

1. Uzak bağlam URL'i YALNIZCA `BuildContextURL` ile kurulur
2. Etiket YALNIZCA `ImageTag` ile kurulur
3. Derleme argümanları denetime yazılırken `audit.RedactEnv` uygulanır
   (derleme argümanları imaj geçmişinde görünür)
4. Derleme çıktısı istemciye akar ama denetime GİRMEZ — çıktı
   kullanıcının kodundan gelir ve sır basabilir

### Açık kalan

Ölçüm Docker Desktop'ın Linux motorunda yapıldı. `AF_UNIX`-only bir
istemcinin GERÇEK systemd altında aynı şekilde çalıştığı hâlâ
sınanmadı — `unshare -n` daha sert bir kısıt olduğu için sonucun
değişmesi beklenmiyor, ama "beklenmiyor" ölçüm değildir. Sürücü
diliminin doğrulama listesine yazıldı.

---

## K-036 — Bütçe metriği açıklamayı cezalandırıyordu

K-034'ten hemen sonra `ImageBuild` eklendi ve bütçe ısırdı: 2631 > 2600.
Bu, bütçenin işlevini görmesiydi — ama sayıya bakınca metrikte ikinci bir
sorun çıktı.

Ayrıcalıklı yüzeyin **%38'i yorum**: 2631 ham satır, 1608 kod satırı.

Ham satır saymak perves bir teşvik yaratıyor: bütçede kalmanın en kolay
yolu **yorum silmek** olurdu. Oysa K-002'nin amacı yüzeyi DENETLENEBİLİR
tutmak ve bu projede ağır yorumlama bilinçli bir karar — açıklama
denetlenebilirliği azaltmıyor, artırıyor. Yani metrik, var olma sebebinin
tersine çalışıyordu.

Planın kendi ifadesi de zaten koddan bahsediyordu:

> ayrıcalıklı yüzeyi denetlenebilir ~1500 satıra indirmektir

Yorum hariç ölçüm **1608** veriyor — tam o mertebe. Yani orijinal 2000
sınırı büyük olasılıkla en baştan kod satırı için düşünülmüştü ve ham
satır sayması bir uygulama hatasıydı.

**Sınır 2600'den ORİJİNAL 2000'e geri alındı.** Bu bir gevşetme değil;
K-034'ün 2000→2600 hamlesi ham-satır ölçümüne yapılmış geçici bir
düzeltmeydi ve artık gereksiz.

Şeffaflık için çıktı **her zaman iki sayıyı da** basıyor:

```
ham satır: 2631 · yorum/boş hariç: 1608
✓ ayrıcalıklı kod 1608 satır (sınır 2000)
```

Yorum ayıklama sezgiseldir (`^\s*(//|/\*|\*|$)`); hata payı kodu FAZLA
saymaya doğrudur, yani bütçe lehine değil aleyhine yanılır.

**Dürüst not:** ölçüm aynı oturumda ikinci kez değişti ve ikisi de baskıyı
azalttı. İkisinin de gerekçesi metriğin yanlış şeyi saymasıydı, ama bu
desen kendi başına bir uyarı işareti. Üçüncü bir ölçüm değişikliği
gerekirse önce "gerçekten metrik mi yanlış, yoksa kod mu fazla büyüdü"
sorusu açıkça yanıtlanmalı.

---

## K-037 — Gerçek sunucuda kapatılan dört boşluk (biri notu yanlışladı)

Kullanıcı "işini garanti yap, masraf korkma" dedi. Ölçülmemiş dört iddia
gerçek Hetzner sunucusunda (Ubuntu 24.04, cx23) sınandı.

### 1. Bugünkü sshd değişikliği gerçek sshd'den geçti

`PermitUserEnvironment no` (K-031) yalnızca betik METNİ üzerinden test
ediliyordu. Bootstrap gerçek sunucuda koşturuldu:

```
sshd -t OK
sshd -T -C user=panely-client → permituserenvironment no
                                 acceptenv LANG / LC_*
                                 exposeauthinfo yes
```

`sshd -T` ETKİN değeri gösteriyor, dosyadaki metni değil. `acceptenv`
yalnızca `LANG`/`LC_*` — yani `SSH_AUTH_INFO_0` istemciden gelemiyor.
Değişiklik gerçekten koruyor.

Bootstrap idempotanlığı da doğrulandı: kurulu bir sunucuda yeniden
koştu, altı kurulum-sonrası kontrolün hepsi geçti.

### 2. NOT YANLIŞTI: Docker'ı sonradan kurmak soketi VERİYOR

Hafızadaki ve dilim-1 notlarındaki iddia şuydu:

> Docker'ı executor çalışırken kurmak soketi VERMEZ.
> `BindReadOnlyPaths=-/run/docker.sock` eksik kaynağı yalnızca
> BAŞLANGIÇTA atlar. Docker'ı kuran ne ise ardından
> `systemctl restart panely-exec` yapmalı — bu sıralama bootstrap'a
> girmeli.

**Ölçüldü, yanlış.** Temiz düzenek: docker durduruldu ve soket silindi →
executor soket YOKKEN yeniden başlatıldı → docker başlatıldı → executor
YENİDEN BAŞLATILMADAN sokete bağlanabildi.

Sebep: `-` öneki bind mount'u atlıyor ama `/run` zaten paylaşımlı bir
tmpfs ve `ProtectSystem=strict` onu kapsamıyor (unit dosyasının kendi
yorumu da bunu söylüyor). Sonradan oluşan soket ad alanında görünüyor.

**Bootstrap'a sıralama kısıtı EKLENMEDİ**, çünkü gerek yok.

### 3. Ters yön de sınandı — asıl risk buradaydı

Not yanlışlanınca daha tehlikeli bir soru çıktı: soket başlangıçta VARSA
bind mount **salt-okunur** uygulanır. `connect()` bir sokete yazma izni
ister — read-only mount bunu engeller mi? Engelleseydi hata NORMAL
durumda (Docker zaten kurulu) çıkardı, yani test edilen durumda değil.

```
/run/docker.sock tmpfs[/docker.sock] ro,nosuid,nodev,noexec
→ curl --unix-socket ... /version
  {"Version":"29.1.3","ApiVersion":"1.52",...}
```

Salt-okunur bind mount `connect()`'i ENGELLEMİYOR. Her iki sıra da
çalışıyor.

### 4. K-035'in açık maddesi kapandı: GERÇEK systemd altında

WSL ölçümü `unshare -n` kullanıyordu. Gerçek soruda kısıt
`RestrictAddressFamilies=AF_UNIX`'ti. `panely-exec.service` ile aynı
sertleştirmeyi taşıyan geçici bir `systemd-run` birimiyle sınandı:

| Adım | Sonuç |
|---|---|
| Pozitif kontrol: birimden `https://github.com` | BAŞARISIZ (ağ gerçekten kapalı) |
| Aynı birimden `POST /build?remote=<git-url>` | `{"message":"Cannot locate specified Dockerfile: Dockerfile"}` |

Hata Dockerfile hakkında, ağ hakkında değil → **daemon depoyu çekti.**
K-035'in tasarımı gerçek systemd altında doğrulandı.

### 5. Yetki izolasyonu artık BOŞ YERE geçmiyor

Docker kurulmadan önce "panely kullanıcısı Docker'a erişemiyor" kontrolü
boş yere geçiyordu — erişilecek bir Docker yoktu. Docker ÇALIŞIRKEN
tekrarlandı:

- `sudo -u panely docker ps` → başarısız ✓
- `sudo -u panely-client docker ps` → başarısız ✓
- `panely` kullanıcısı `docker` grubunda değil ✓

Uçtan uca: `panely status panely-client@<sunucu>` artık `Docker 29.1.3`
gösteriyor — SSH → zorlanmış komut → panelyd → executor → Docker soketi
zincirinin tamamı çalışıyor. Denetim zinciri geçerli.

### Ölçüm düzeneğinin kendisi iki kez bozuldu

Kayda değer, çünkü ikisi de sessizce yanlış sonuç verirdi:

1. Birinci turda geçici birim `PrivateTmp=yes` taşıyordu ve `curl -o
   /tmp/out` çıktıyı birimin ÖZEL /tmp'sine yazdı; dış kabuk boş dosya
   okudu ve "belirsiz" dedi. Çıktı `--pipe` ile stdout'a alındı.
2. ARM kapasitesi taranırken sonuç çıkış kodu yerine METİNDE arandı;
   "unsupported location" mesajı `error` kelimesi içermediği için
   "OLUŞTU" sanıldı. Çıkış koduna geçildi.

İkincisi bu oturumda üçüncü kez tekrarlanan sınıf (bkz. `| tail`
çıkış kodunu maskeleme). Kural: **ölçüm sonucu metinden değil çıkış
kodundan okunur.**

### arm64: kapasite yoktu, çözüm CI'da

Gerçek ARM donanımı için Hetzner denendi — `cax11` nbg1, fsn1 ve
hel1'in ÜÇÜNDE de `resource_unavailable` (ARM yalnızca EU
konumlarında). Boşluk parayla değil kapasiteyle kapalıydı.

Depo public olduğu için `ubuntu-24.04-arm` runner'ı ücretsiz. Test
matrisine eklendi ve **ilk koşuda geçti**: `Testler (ubuntu-24.04-arm)
success`. Tek seferlik bir sunucudan daha iyi — arm64 artık her
commit'te gerçek donanımda sınanıyor, yalnızca çapraz derlenmiyor.

---

## K-038 — Hacim sertleştirmesi sürücüde değil, mount biriminde

Faz 1 dilim 2'nin devraldığı yükümlülüklerden biri şuydu: *"Hacimler
`nodev,nosuid`."* Tasarlanan yol, hacim başına Docker `local` sürücüsüyle
`--opt o=bind,nodev,nosuid` vermekti.

**Ölçüldü, çalışmıyor.** Kontrol hacmi ile "sertleştirilmiş" hacmin
konteyner içindeki etkin seçenekleri bayt bayt aynı çıktı:

| hacim | konteyner içindeki etkin seçenekler |
|---|---|
| `o=bind` (kontrol) | `rw,relatime` |
| `o=bind,nodev,nosuid` | `rw,relatime` ← **fark yok** |

Docker hata vermiyor, hacmi oluşturuyor, konteyner çalışıyor. Yalnızca
koruma yok.

### Sebep

Bind mount **tek bir `mount(2)` çağrısında bayrak değiştiremez**;
bayraklar kaynak mount'tan miras alınır. Değiştirmek AYRI bir
`remount,bind` çağrısı ister. Docker'ın local sürücüsü `mount(2)`'yi
doğrudan çağırdığı için ikinci adımı atmaz. `mount(8)` ve systemd
atar — bu yüzden **aynı seçenek dizgisi** systemd'de çalışır, Docker'da
çalışmaz. Ölçmeden bunu ayırt etmenin yolu yoktu.

### Yerine ne yapıldı

Sertleştirme tek bir systemd `.mount` birimine taşındı:
`/var/lib/panely/volumes` kendi üzerine `bind,nodev,nosuid` ile bağlanır.

Ölçülen iki özellik bunu yeterli kılıyor:

1. **Alt dizinler bayrakları miras alıyor.** Sertleştirilmiş bir mount'un
   alt dizini konteynere bind edildiğinde `nodev,nosuid` taşıyor. Yani tek
   birim, o kökün altındaki HER hacmi kapsar — sürücünün ileride
   ekleyeceği, kimsenin aklına gelmeyen yollar dahil. Hacim başına
   sertleştirmede unutulan tek yol sessizce korumasız kalırdı.
2. **Düz `Binds` yetiyor.** Engine API ile oluşturulan konteynerde
   `/var/lib/panely/volumes/...:/data` bağlaması `rw,nosuid,nodev` geliyor.
   Sürücünün hacim oluşturma koduna hiç ihtiyacı yok.

İkincisi ayrıcalıklı yüzey bütçesinde de kazanç: sürücü hacim tarafında
kod TAŞIMIYOR.

### Bayrakların etkili olduğu nasıl bilindi

Metinden değil davranıştan. setuid-root bir ikili:

```
/tmp üzerinden           -> euid=0     (setuid onurlandırıldı)
hacim kökü üzerinden     -> euid=1000  (yok sayıldı)
```

Aygıt düğümü okuma denemesi de bu mount üzerinde reddedildi.

⚠ İlk denemede `ls -l` ile bakılmıştı ve setuid biti görünüyordu. **O test
hiçbir şey ölçmez:** `ls -l` inode'un mod bitini gösterir ve mount
bayrağından etkilenmez. Doğru ölçüm çalıştırıp euid'e bakmaktır.

### Zorlanması

`systemctl is-active` yetmez — birim "active" görünürken bayraklar yok
sayılmış olabilir (yukarıdaki Docker durumunun tam olarak sessiz hâli).
Bu yüzden `install.sh` bayrakları `/proc/self/mountinfo`'dan, yani
çekirdekten okur ve eksikse `die` ile durur.

---

## K-039 — Birim kurulumu geçti, YENİDEN BAŞLATMAYI geçmedi

K-038'in mount birimi kurulumda kusursuz göründü: `systemctl is-active`
-> active, çekirdekten okunan bayraklar -> `rw,nosuid,nodev`, davranış
testi -> euid=1000. Üç bağımsız kontrol de yeşildi.

**Yeniden başlatmadan sonra koruma yoktu.** Birim `inactive`, bayrak yok,
hacim kökünde setuid-root ikili euid=**0** ile çalışıyordu.

systemd'nin kendi teşhisi:

```
local-fs.target: Found ordering cycle on var-lib-panely-volumes.mount/start
  Found dependency on systemd-tmpfiles-setup.service/start
  Found dependency on local-fs.target/start
Job var-lib-panely-volumes.mount/start deleted to break ordering cycle
```

### İki turda çözüldü — birincisi düzelmiş gibi göründü

**Tur 1.** Birim `WantedBy=local-fs.target` + tmpfiles'a `Requires=`
taşıyordu. `systemd-tmpfiles-setup.service`'in KENDİSİ
`After=local-fs.target` olduğu için halka kapandı.

Düzeltme: `Requires=` atıldı, `WantedBy=multi-user.target` yapıldı,
`After=systemd-tmpfiles-setup.service` bırakıldı. Yeniden başlatıldı ->
birim **active**, bayraklar doğru, davranış testi geçti.

**Ama journal'da hâlâ 2 döngü mesajı vardı.** Sonuç doğru göründüğü için
buna bakılmasaydı iş burada "bitmiş" sayılacaktı.

**Tur 2.** Döngü duruyordu, çünkü systemd HER `.mount` birimine örtük
`Before=local-fs.target` ekler (DefaultDependencies). Halka örtük kenardan
kapanıyordu:

```
local-fs.target <- bu mount <- tmpfiles-setup <- local-fs.target
```

O turda systemd döngüyü **bizim mount'umuzu değil** `local-fs.target/start`
işini silerek kırmıştı. Yani mount tesadüfen ayağa kalkmıştı: doğru
sonuç, yanlış sebep. Aynı yapılandırma başka bir makinede ters seçimle
kırılabilirdi.

Gerçek çözüm kenarı hiç kurmamak: `.mount` birimi kendi mount noktasını
zaten yaratır, dolayısıyla tmpfiles'a sıralama **hiç gerekmiyordu**.
`After=` de kaldırıldı -> üçüncü yeniden başlatmada **0 döngü**, birim
active, euid=1000, dizin modu `751 root:panely`.

### Çıkarılan kural

**Bir systemd biriminin kabul ölçütü "kurulumdan sonra active" değil,
"yeniden başlatmadan sonra active VE journal'da 0 döngü"dür.**

Bu, K-030'un (`-` öneki, `226/NAMESPACE`) aynı sınıfı: birim kurulu
makinede çalışır, açılışta çalışmaz. Birim testleri, WSL ve CI'ın
hiçbiri yakalayamaz — hiçbiri gerçek bir önyükleme yapmıyor.

⚠ Ayrıca: **"sonuç doğru" ile "sebep doğru" aynı şey değildir.** Tur 1'de
üç davranış kontrolü de yeşildi ve yapılandırma yine hatalıydı. Yakalatan
tek şey, beklenmeyen bir sayacın (journal'daki döngü sayısı) sıfır
olmamasıydı.

### Kalıcı savunma

Sertleştirmenin varlığı systemd'ye bırakılmıyor: executor hacim
bağlamadan önce etkin bayrakları çalışma anında kendisi doğrular. Bir
sıralama inceliği güvenlik özelliğini sessizce düşürememeli.

---

## K-040 — Docker sürücüsü ve bütçe: tahmin yanlıştı, kural gevşetilmedi

Faz 1 dilim 2 (Docker sürücüsü) yazılınca ayrıcalıklı yüzey **2199** kod
satırına çıktı ve `check-exec-surface.sh` ateşledi. Kontrol tasarlandığı
gibi çalıştı.

Kural şuydu (planın açık risk maddesi): *"internal/exec 2000 satırı
geçerse **ne eklendiği sorgulanacak**."* Yani sınır bir yasak değil, bir
DURDURMA NOKTASI. Sorgulandı.

### Ne eklendi?

`internal/dockerdrv` — 444 kod satırı. Docker Engine API'sinin Panely'nin
kullandığı kadarı: konteyner oluştur/başlat/durdur/sil/listele, ağ kur.

Bu, ayrıcalıklı binary'nin **var olma sebebi**. Docker'a konuşamayan bir
executor'ın hiçbir işlevi yoktur; ayrıcalık zaten yalnızca bunun için
alınıyor. "Bunu çıkaralım" seçeneği, projeyi çıkarmak demek.

### Neden 2000'di?

Plan "denetlenebilir ~1500 satırlık bir executor" diyordu ve 2000 o
TAHMİNE bir pay eklenerek konmuştu. Tahmin, Engine API sarmalayıcısının
gerçek maliyeti bilinmeden yapılmıştı.

Ölçüm tahmini yanlışladı. **Düzeltilen şey kural değil, kuralın dayandığı
sayıdır.** Sınır ölçülen ihtiyaca göre 2500'e çekildi (Faz 1'in kalan
derleme+günlük dilimi ~150-200 satır daha getirecek).

### Önce ucuz olan yapıldı

Sınıra dokunmadan önce gerçek indirim arandı ve bulundu:

- `internal/exec/dockerprobe.go` **silindi** (37 satır). Sürücünün `Ping`
  metodu aynı işi yapıyordu; iki ayrı unix-soketi HTTP istemcisi tutmanın
  gerekçesi kalmamıştı.
- Hacim sertleştirmesi sürücüden ÇIKTI (K-038). Hacim oluşturma kodu hiç
  yazılmadı; tek bir systemd mount birimi işi görüyor. Bu tek başına
  tahminen ~60-80 satır tasarruf.

Kalan 199 satır için kesilecek gerçek yağ yoktu. Yorumlar zaten
sayılmıyor (sürücünün %42'si yorum).

### ⚠ Dürüstlük notu: metrik kısa sürede üç kez değişti

1. **Kapsam**: sabit yol listesi → `go list -deps` içe aktarma grafiği
   (K-034). Eski kapsam yüzeyin YARISINI sayıyordu.
2. **Birim**: ham satır → yorum/boş hariç kod (K-036). Ham satır saymak
   bütçede kalmak için yorum silmeyi ödüllendiriyordu.
3. **Sınır**: 2000 → 2500 (bu kayıt).

Üçünün de gerekçesi var ve ikisi metriği DAHA sıkı yaptı. Ama desen kendi
başına bir uyarıdır: **ölçtüğü şeye uyacak biçimde sürekli ayarlanan bir
metrik ölçmeyi bırakır.**

Bu yüzden bir fren konuyor: **bundan sonraki her sınır yükseltmesi,
yüzeyi KÜÇÜLTME seçeneğinin neden tercih edilmediğinin yazılı gerekçesini
gerektirir.** Bu kayıt o gerekçenin ilk örneği.

### Sınır zaten tek savunma değil

Bütçe bir vekil ölçüdür. Asıl değişmezleri zorlayan şey aynı betikteki
YAPISAL kontrollerdir ve onlar **gevşetilmedi**:

- şemada yasak alan taraması (17 desen: `privileged`, `cap_add`,
  `devices`, `pid_mode`, …)
- serbest argv/kabuk alanı taraması
- `require_unimplemented_servers=false` tel tuzağı (K-002)

Sürücü katmanı bu duruşu sürdürüyor: tehlikeli alanlar `createBody`
yapısında **hiç tanımlı değil**, yani "false gönderiliyor" değil TEMSİL
EDİLEMEZ. Bir test bunu telde doğruluyor.

### Bu arada bulunan ayrı bir hata

`check-exec-surface.sh`'ın "yasak alanlar" bölümü sonucu GENEL `fail`
sayacına bakarak raporluyordu. Bütçe gibi ALAKASIZ bir başarısızlık, bu
bölümün ✓ satırını yutuyordu: tarama geçmesine rağmen çıktıda hiçbir şey
görünmüyordu. Geçtiğini söylemeyen bir güvenlik kontrolü, okuyana "koştu
mu, kaldı mı?" sorusu bırakır. Bölüm-yerel sayaca çevrildi (hemen
altındaki argv bölümü zaten doğru deseni kullanıyordu).

---

## K-041 — Docker API sürümü sabitlenemez; pencere kayıyor

Sürücü `apiVersion = "v1.51"` ile yazıldı — geliştirme sunucusundaki
Docker 29.1.3'e bakılarak. CI ilk koşuda reddetti:

```
HTTP 400: client version 1.51 is too new.
          Maximum supported API version is 1.48
```

Daemon, kendinden YENİ sürüm isteyen istemciyi tümden reddediyor. Yani o
pin yalnızca CI'ı değil, **Docker'ı biraz eski olan her sunucuda Panely'yi
tamamen çalışmaz** kılardı — ve bu ancak o sunucuda ortaya çıkardı.

Uyum için `v1.41`'e düşürüldü (Docker 20.10, 2020). Bu sefer **sunucu**
reddetti:

```
HTTP 400: client version 1.41 is too old.
          Minimum supported API version is 1.44
```

### İki host, zıt kısıt

| | min | maks |
|---|---|---|
| CI runner'ı | 1.24 | **1.48** |
| Docker 29.1.3 | **1.44** | 1.52 |

Desteklenen aralık bir **pencere** ve o pencere **kayıyor**: Docker tabanı
bir kez 1.24'ten 1.44'e çekti. Sabit bir pin bugün çalışsa da ileride bir
hostta kırılır ve arıza kısmi değil **toplam** olur — her istek reddedilir.

### Çözüm: uzlaşma, sabitleme değil

Sürüm ilk kullanımda uzlaşılıyor: daemon'ın bildirdiği
`[MinAPIVersion, ApiVersion]` ile bizim sınandığımız `[1.44, 1.48]`
kesiştirilip **ortak en yüksek** sürüm seçiliyor.

Sabitlemenin ASIL gerekçesi korunuyor — sürümsüz istek daemon'ın en
yenisine düşer ve alan anlamları sürümler arasında değişebilir. Uzlaşma
bunu bozmuyor: sürüm yine bağlantı başına **sabit**, yalnızca hangisi
olduğu ölçülerek belirleniyor ve **bizim üst sınırımızı aşamıyor**
(sınanmamış sürüme çıkılmaz). Örtüşme yoksa **hata** — sessizce sürümsüz
isteğe düşülmüyor.

Sürüm karşılaştırması sayısal; sözlüksel olsaydı `"1.9" > "1.48"` derdi ve
uzlaşma sessizce yanlış sürüm seçerdi. Ayrıca test edildi.

### Asıl ders: E2E adımı hiçbir şey sınamadan YEŞİL geçti

Bundan daha önemlisi bu. Testler Ping başarısız olunca `t.Skip` çağırıyordu;
sürüm uyuşmazlığı yüzünden **hepsi atlandı ve adım başarılı raporlandı.**

Sorunu yakalayan şey asıl test değil, tesadüfen eklenmiş ayrı bir **tuzak
adımıydı** (sertleştirme kaldırılınca testin ateşlediğini doğrulayan adım).
O adım olmasaydı sürücü "gerçek Docker'da doğrulandı" sanılarak
yayınlanacaktı.

`PANELY_E2E_REQUIRE_DOCKER=1` verildiğinde atlama hakkı kalkıyor ve atlama
**başarısızlık** sayılıyor. CI bunu veriyor: orada Docker var, dolayısıyla
atlamak bir arızadır.

**Kural:** bağımlılığın var olduğu GARANTİ edilen ortamda `t.Skip`
yasaktır. Atlanan test ile geçen testi ayırt edemeyen bir kontrol, yeşil
rozetten başka bir şey üretmez — bu projede aynı sınıfın kaçıncı tekrarı
olduğu artık sayılmıyor.

---

## K-042 — Derlemede başarı ölçütü POZİTİF olmalı: `aux` karesi

Derleme dilimine başlamadan önce tek bir soru bütün tasarımı belirledi:
`POST /build`'e hangi derleyici cevap veriyor?

İki ihtimal vardı ve aralarındaki fark bu dilimin yapılabilir olup
olmamasıydı:

| | çıktı | maliyet |
|---|---|---|
| klasik derleyici (`version=1`) | satır ayrılmış JSON | ~100 satır |
| BuildKit (`version=2`) | ele geçirilmiş bağlantı üzerinden gRPC oturumu | bütçeye SIĞMAZ |

Klasik derleyici Docker 23'te kullanımdan kaldırılmış (deprecated) ilan
edildi. Kalan bütün ölçümlerimiz onun çıktı biçimine dayanıyordu; hâlâ
cevap vermiyorsa dilim boşlukta duruyordu demekti.

**Ölçüldü (Docker 29.1.3):** klasik derleyici cevap veriyor, kaldırılmamış.

### Üç başarısızlık biçimi, yalnızca biri HTTP hatası

Yerel bir `git daemon` ile kurulan üç depoya karşı ölçüldü:

| durum | HTTP | akış |
|---|---|---|
| çekme aşamasında hata | **500** | `{"message":"error fetching: ..."}` |
| derleme ORTASINDA hata | **200** | son karede `{"error":...}`, **aux YOK** |
| başarı | **200** | `{"aux":{"ID":"sha256:..."}}`, **error YOK** |

Yani durum koduna bakan bir sürücü, derleme ortasında ölen HER derlemeyi
başarılı sayardı: bozuk imaj etiketlenir, dağıtıma girer ve arıza ancak
üretimde görünürdü.

### "Hata karesi yok" YETERLİ DEĞİL

İlk tasarım kabul ölçütü olarak hata karesinin YOKLUĞUNU alıyordu. Bu bir
OLUMSUZUN YOKLUĞUDUR ve üç şekilde sessizce yanılır: ayrıştırıcıda bir
hata olursa, akış sessizce kesilirse, ya da Docker dördüncü bir
başarısızlık biçimi eklerse.

Ölçüm daha iyisini verdi: `aux` karesi başarıda **daima** geliyor, ortada
ölen derlemede **hiç** gelmiyor. Ölçüt bu yüzden POZİTİF:

> aux karesi görülmediyse derleme başarısızdır — hata karesi hiç gelmemiş
> olsa bile.

Bu, "imaj var mı?" diye sormaktan da güçlü. Etiket `panely/<app>:<sha>`
ve aynı commit daha önce derlenmiş olabilir; o durumda başarısız bir
derlemeden sonra **eski** imaj bulunur ve kontrol yanılırdı. aux karesi
**bu** derlemenin ürettiği kimliktir ve denetim kaydına o yazılıyor —
kaydı sonradan yanlışlanabilir kılan alan da bu.

### Yanında doğrulanan iki şey

- **`build_args` değerleri imaj geçmişinde düz metin görünüyor.**
  `docker history` çıktısında `GIZLI_ARG=deger-42` okundu. exec.proto bunu
  zaten yazıyordu; artık iddia değil ölçüm. Denetim zincirine
  `audit.RedactEnv` ile yalnızca ADLAR giriyor: aynı sızıntıyı
  ekle-sadece bir zincire kopyalamanın anlamı yok.
- **GitHub tam SHA ile fetch'e izin veriyor.** `BuildContextURL` daima
  `#<40-hex>` üretiyor; GitHub `allowReachableSHA1InWant` desteklemeseydi
  üretimdeki her derleme kırılırdı. Gerçek GitHub'a karşı ölçüldü.

### Günlük çerçevelemesi

`ContainerLogs` için de baytlar okundu, belge okunmadı:

```
01 00 00 00  00 00 00 0d  "cikti-satiri\n"
02 00 00 00  00 00 00 0c  "hata-satiri\n"
```

bayt 0 akış türü, 1-3 dolgu, 4-7 **büyük-uçlu** uzunluk. Uzunluk telden
geldiği için kare tek seferde tamponlanmıyor: sabit 32 KiB'lik tampondan
parçalanarak akıtılıyor. Aksi hâlde bozuk bir daemon AYRICALIKLI süreçte
4 GiB ayırtabilirdi.

---

## K-043 — Test, kurduğu bağımlılığın DOĞRU İÇERİĞİ sunduğunu doğrulamalı

Derleme E2E testi kendi git sunucusunu kuruyor. İlk hâli iki kestirme
yapıyordu: git'in **varsayılan portunu** (9418) kullanmak ve `cmd.Start()`
başarılı dönünce daemon'ı çalışıyor saymak.

İkisi birlikte şunu üretti: makinede önceki ölçümlerden kalmış bir git
daemon aynı portu tutuyordu. Yeni daemon başladı, "address already in
use" ile öldü — ama `Start()` bunu görmez, çünkü süreç gerçekten
başlamıştı. Test sessizce **eski** daemon'a bağlandı ve kendi yazdığı
Dockerfile yerine **bambaşka bir deponun** içeriğini derledi.

Test kırmızı oldu, ama doğru sebeple değil: yalnızca çıktı beklenen izi
taşımadığı için. İçerik tesadüfen uyuşsaydı **yeşil geçecek ve hiçbir şey
kanıtlamayacaktı.**

**Düzeltme:** port işletim sisteminden isteniyor (`127.0.0.1:0`) ve
daemon'ın **bizim depomuzu** yayınladığı `git ls-remote` ile doğrulanana
kadar teste başlanmıyor.

**Kural:** bir test kendi kurduğu bağımlılığı kullanacaksa, "başlattım"
kabul ölçütü değildir. Ölçüt, o bağımlılığın **beklenen içeriği
sunduğunun** gözlenmesidir. Aynı sınıf K-039'da systemd birimi için
("kurulumdan sonra active" yetmedi, yeniden başlatma gerekti) ve K-041'de
atlanan testler için görülmüştü.

### Mutasyon sınaması bir boşluk daha buldu

`ContainerLogs` tekil eşleşme kontrolü (`len(matches) != 1`) `< 1` yapıldı
ve test YİNE geçti. Sebep: iki eşleşmeli durumda çağrı gerçekten hata
döndürüyordu — ama çokluk kontrolünden değil, sahte daemon'ın günlük
biçiminde olmayan gövdesinden. Test **doğru sonucu yanlış sebeple**
geçiyordu.

`if err == nil { t.Error }` zayıf bir iddiadır: herhangi bir hata onu
tatmin eder. Ayırt edici ölçüt davranışsal olmalıydı — istek **tele hiç
çıkmamalı**. Kontrol o hâle getirildi ve mutasyon yakalandı (11/11).

### Aynı sınıfın üçüncü örneği: ARTIK KALMIŞ duruma yaslanmak

Günlük E2E testi konteyneri oluşturup başlatıyor ama ağı kurmuyordu.
Geliştirme sunucusunda **geçti** — `panely-e2etest` ağı önceki
koşulardan kalmıştı. CI'da taze bir runner'da düştü:

```
ContainerStart: HTTP 404: failed to set up container networking:
                network panely-e2etest not found
```

Dosya adı sırası (`build_e2e_test.go` < `e2e_test.go`) bu testi, ağı
kuran yaşam döngüsü testinden **önce** koşturuyor. Yani test hiçbir zaman
kendi başına çalışmıyordu; yalnızca sıra ve artık durum sayesinde yeşildi.

Bu, "gerçek sunucuda koştu" ifadesinin **CI'da koşacak** anlamına
gelmediğinin doğrudan kanıtı: geliştirme sunucusu aylardır birikmiş
durum taşıyor, taze runner taşımıyor.

**Kural:** E2E testi kendi ön koşullarını **kendisi** kurmalı ve tam
temizlenmiş bir daemon'da koşturularak doğrulanmalı. Doğrulama böyle
yapıldı: tüm `panely/*` imajları ve ağları silindikten sonra 33 testin
tamamı yeşil.

---

## K-044 — `http.Client.Timeout` gövde okumasını da kapsıyor; akış uçları onunla ölür

Sürücü istemcisi şöyle kuruluyordu:

```go
http: &http.Client{
    // Akış uçları (logs, build) kendi bağlamlarıyla yönetilir; bu
    // zaman aşımı yalnızca istek/yanıt turları içindir.
    Timeout: 60 * time.Second,
    ...
}
```

Yorum **yanlıştı**. `http.Client.Timeout` bağlantı kurmayı, yönlendirmeleri
**ve gövdenin okunmasını** kapsar — akıp akmadığına bakmaz.

**Ölçüldü** (300 ms sınır, kare kare akan bir yanıt):

```
okunan bayt: 15, hata: context deadline exceeded
             (Client.Timeout or context cancellation while reading body)
```

Üç kare okundu, sonra akış öldü. 60 sn'lik sınır iki şeyi sessizce
bozardı:

- `panely logs -f` **her dakika kopardı**
- 60 sn'den uzun süren **hiçbir derleme başarılı olamazdı** — ki Faz 1'in
  var olma sebebi derleme yapmak. Arıza da kısmi görünmezdi: derleme
  yarıda kesileceği için `aux` karesi hiç gelmez ve K-042'nin pozitif
  ölçütü onu "başarısız" sayardı. Yani kullanıcı, düzgün bir derlemenin
  neden başarısız olduğunu gösteren hiçbir ipucu görmezdi.

### Çözüm

Sınır istemciden kaldırıldı, **akış olmayan** yollara bağlam üzerinden
kondu (`doJSON`, `negotiate`). Akış uçları (`ImageBuild`, `ContainerLogs`)
`do()`'yu doğrudan çağırıyor ve çağıranın bağlamıyla yönetiliyor —
istemci koparsa ctx zaten iptal olur.

Sınırın tamamen silinmesi **yanlış** olurdu: asılı kalan bir daemon
ayrıcalıklı süreci sonsuza kadar bekletirdi. İki taraf da teste bağlandı:
akış uzun sürebilmeli, akış olmayan çağrı sürememeli.

### Bu, yorumun andığı mekanizmanın gerçek olmaması sınıfının tekrarı

Aynı sınıf daha önce iki kez görüldü: `restrict` niteleyicisi ve
"redakte edilmiş" iddiası. Kural yine aynı: bir yorum bir mekanizmaya
dayanıyorsa, o mekanizmayı **yanlışlayacak** bir deney yazılmalı.

### Mutasyon sınaması testin KÖRLÜĞÜNÜ gösterdi

İlk regresyon testi `httptest`'in istemcisiyle kurulmuş bir `Client`
kullanıyordu. Blanket `Timeout` **geri konduğunda test yine geçti** —
çünkü hatanın yaşadığı yer `New()`'di ve test `New()`'i hiç çağırmıyordu.

Testin doğru şeyi ölçmesi için ayırt edici kontrol doğrudan oraya
bakmak zorundaydı:

```go
if to := New("/yok.sock", "/yok").http.Timeout; to != 0 { ... }
```

**Ders:** bir regresyon testi, hatanın YAŞADIĞI kod yolunu çalıştırdığını
kanıtlamalı. Aynı davranışı taklit eden bir kurulum üzerinden geçen test,
o kurulum hatalı olan yeri içermiyorsa hiçbir şey korumaz.

---

## K-045 — Bütçe kapsamı ÖLÇÜLDÜ: üretilen kod dışarıda, `pbconv` içeride

Faz 1 dilim 4a, panelyd tarafına dört yeni RPC ve iki tablo ekliyor. Kod
yazmadan önce yanıtlanması gereken soru şuydu: **api.proto'yu büyütmek
ayrıcalıklı yüzey bütçesini harcar mı?**

Soru boş değil. `internal/pb/panely/v1` **tek bir Go paketidir** ve hem
`api.pb.go` hem `exec.pb.go` oradadır; paket `cmd/panely-exec`'in içe
aktarma grafiğindedir. Cevap "evet" olsaydı, dört RPC ve beş mesaj
eklemek altı satırlık kalan bütçeyi ilk adımda bitirirdi ve çözüm yapısal
olurdu (`go_package` ayırmak) — yani RPC'ler yazıldıktan SONRA değil,
ÖNCE bilinmesi gereken bir şey.

### Ölçüm

`scripts/check-exec-surface.sh` satır 192-199, `internal/pb/*` ile
başlayan her dizinde `continue` ediyor. Doğrulandı:

```
go list -deps ./cmd/panely-exec | grep panely/  →  12 modül-içi paket
scripts/check-exec-surface.sh                   →  "11 paket"
```

Fark tam olarak `internal/pb/panely/v1`. Ampirik olarak da doğrulandı:
api.proto'ya mesajlar ve RPC'ler eklendi, `buf generate` koşuldu, sayaç
**2494'te kaldı**.

### Asıl tuzak proto değil, `internal/pbconv`

`pbconv` (116 kod satırı) `internal/pb/` ALTINDA DEĞİL, yani **bütçeye
yazılıyor** — ve üç tarafın hepsi onu içe aktarıyor:

| dosya | taraf |
|---|---|
| `internal/exec/server.go` | ayrıcalıklı |
| `internal/execclient/client.go` | daemon |
| `internal/api/server.go` | daemon |

Ortak adı ("dönüşümler burada") oraya yeni dönüştürücü koymayı doğal
gösteriyor. App/release dönüştürücüleri oraya konsaydı, root süreçle
hiçbir ilgisi olmayan kod root bütçesinden harcanırdı.

**Kural:** yalnızca panelyd'nin kullandığı dönüşümler `internal/api`
içinde kalır. Bu dilimin dönüştürücüleri `internal/api/apps.go`'da.

### Bu dilimin bütçeye maliyeti: 3 satır (2494 → 2497)

Tek kalem: `internal/exec/container.go`, imaj kimliğini panelyd'ye
gönderen üç satır (bkz. K-046). Sınıra DOKUNULMADI; K-040'ın freni bu
yüzden devreye girmedi.

⚠ **2497/2500 — 3 satır kaldı.** Dilim 4b'nin ilk kararı bu yüzden
"Caddy'ye kim konuşuyor" olmalı: executor üzerinden gitmek bütçeyi kesin
olarak aşar ve K-040 gereği yazılı gerekçe ister.

---

## K-046 — Başarının kanıtı KATMAN ATLAYAMAZ: `image_id` akışa eklendi

Dilim 3, Docker'ın klasik derleyicisinde başarının ölçütünün pozitif
`aux` karesi olduğunu saptadı (K-042) ve executor bunu doğru uyguladı.
Ama kimlik **yalnızca executor'ın kendi denetim günlüğüne** yazılıyordu;
`ImageBuildResponse` sadece `data` ve `is_stderr` taşıyordu.

Yani panelyd'nin elindeki tek ölçüt "akış hatasız bitti"ydi — **olumsuzun
yokluğu**, tam olarak K-042'nin reddettiği şey.

### Eksik, sözleşmeyi KULLANMAYA çalışınca ortaya çıktı

Kontrol düzlemi şeması (göç 0002) imaj kimliği olmayan bir `BUILT`
satırını kabul etmiyor:

```sql
CHECK (status != 2 OR image_id != '')
```

panelyd bir sürümü mühürlemeye kalkınca kimliğin hiç gelmediği görüldü.
Şema, eksik bir sözleşmeyi ilk kullanımda görünür kıldı — kısıtı
veritabanına koymanın beklenmedik getirisi.

### Çözüm ve maliyeti

`ImageBuildResponse.image_id` eklendi; yalnızca SON mesajda ve yalnızca
başarıda dolu. Executor tarafındaki değişiklik üç satır:

```go
if imageID != "" {
    params["image_id"] = imageID
    if opErr == nil {
        opErr = stream.Send(&panelyv1.ImageBuildResponse{ImageId: imageID})
    }
}
```

Gönderim başarısız olursa `opErr` doluyor ve kayıt FAILURE yazıyor. Yön
doğru: panelyd kimliği öğrenmediyse sürümü mühürleyemez, dolayısıyla
sistem açısından derleme teslim edilmemiştir.

### Aynı ölçüt ÜÇ katmanda tekrarlanıyor

| katman | pozitif kanıt |
|---|---|
| dockerdrv | Docker'ın `aux` karesi |
| execclient → api | `ImageBuildResponse.image_id` |
| api → istemci | son mesaj `DeploySucceeded` |

Üçü de "hata görmedim" değil, "kanıtı gördüm" diyor. Tekrar israf değil:
bir katman sessizce gevşerse diğerleri yakalar, ve her katmanın kendi
testi var.

---

## K-047 — panelyd ağa çıkamaz; dal→commit çözümü İSTEMCİDE

`deploy/systemd/panelyd.service` satır 92:

```
RestrictAddressFamilies=AF_UNIX
```

panelyd **TCP soketi açamaz**. Bu, uygulama modelinin şeklini belirledi:
`git ls-remote` de HTTPS de mümkün değil, dolayısıyla "hangi commit"
sorusunu daemon yanıtlayamaz.

### Sonuç: `Deploy` TAM 40 haneli sha alır

Çözümü iş istasyonundaki istemci yapıyor (`cmd/panely/gitref.go`), git'in
smart-HTTP keşif ucuyla. `git` ikilisine kabuk çağrısı YOK: iş
istasyonunda git kurulu olmayabilir ve bir alt sürece dal adı geçirmek,
projenin baştan beri kaçındığı serbest-argv sınıfını istemci tarafında
geri getirirdi. Dal adı burada bir sorgu parametresi bile değil; yalnızca
yanıtta ARANAN bir dize.

Model de zaten bunu istiyor: uygulama bir DALA bakar (hareket eder),
sürüm bir COMMIT'tir (donmuştur). Bu yüzden `AppSpec` `git_branch`,
`GitSource` `commit_sha` taşıyor ve ikisi aynı mesaj değil.

### Kısıt ÖLÇÜLDÜ, birim dosyasından okunmadı

Bir `RestrictAddressFamilies=` satırının orada YAZMASI, çekirdeğin onu
zorladığını kanıtlamaz. Gerçek sunucuda A/B (aynı kullanıcı, tek fark
kısıt):

| koşum | TCP bağlantısı (`/dev/tcp/github.com/443`) |
|---|---|
| A — kısıt YOK | `exit=0` (bağlandı) |
| B — `RestrictAddressFamilies=AF_UNIX` | `exit=1` (engellendi) |
| C — B ile aynı, unix soketi | erişilebilir |

C satırı önemli: kısıt yalnızca ağı kesiyor, daemon'ın kendi soketini
değil. Yani "panelyd ağa çıkamaz ama işini yapabilir" ölçülmüş bir olgu.

### Bu bir eksiklik değil, ölçülebilir en-az-yetki

Faz 4'ün webhook akışı da bunu bozmuyor: GitHub'ın yükü zaten commit
sha'sını taşıyor. Yani daemon'ın ağa çıkması için bilinen bir gerekçe
kalmıyor.

⚠ **Dilim 4b bunu değiştirmek zorunda kalacak.** Sağlık denetçisi
konteynerlere HTTP yoklaması atacak; birimin kendi yorumu bunu zaten
yazıyor ("Faz 1'de … AF_INET AF_INET6 eklenecek"). O an, gevşetmenin
gerekçesi ayrıca yazılmalı — Caddy'nin admin ucu unix soketi olduğu için
gevşetmenin TEK sebebi sağlık yoklamasıdır.

---

## K-048 — Doğrulama kopyası: ne zaman kabul edilir

Daemon, uygulama tanımını `internal/api/appvalidate.go`'da doğruluyor ve
aynı desenler executor'ın doğrulayıcılarında da var. Dilim 3'te
`ImageTag`'in kopyası tam da "iki tanım" gerekçesiyle SİLİNMİŞTİ. İkisi
neden farklı?

### Ayırt edici soru: sapma NE ÜRETİR?

| kopya | sapmanın sonucu |
|---|---|
| `ImageTag` (silindi) | derleme bir etiketle, konteyner başka etiketle → **sessiz çalışma-zamanı uyuşmazlığı** |
| doğrulayıcı (korundu) | biri daha katı → **istek reddedilir** |

Doğrulayıcıda sapma her iki yönde de yalnızca redde yol açıyor; hiçbir
yönde bir kaçış üretmiyor. Ret gürültülüdür ve hemen görülür.

Kopyanın karşılığında alınan şey gerçek: `app create` anında hatalı bir
tanım yakalanıyor. Kopya olmasaydı hata ancak ilk `deploy`'da, executor
tarafında görünürdü.

### Politika ASLA kopyalanmıyor

Daemon "bu bir host adına benziyor mu" diyor; **izinli host listesini
bilmiyor ve bilmemeli**. Liste executor'ın `-allow-git-host` bayrağında
ve bir işletme kararı: ele geçirilmiş bir panelyd ona ekleme yapamamalı.

Kopyalanan şey **karakter kümesi**, kopyalanmayan şey **politika**.

Aynı ayrım göç 0002'deki CHECK'ler için de geçerli — orası ÜÇÜNCÜ katman
ve bazı kısıtlar (`CHECK (status != 2 OR image_id != '')`) yalnızca orada
zorlanabiliyor: uygulama katmanındaki bir hata bile o satırı yazamaz.

---

## K-049 — "active" ikilinin değiştiğini KANITLAMAZ

Dilim 4a'yı gerçek sunucuda koşturmadan önce yeni ikililer kuruldu. Kurulum
betiği `/usr/local/bin` altına yazdı, `systemctl restart` koştu ve şunu
raporladı:

```
== sonra: durum ==
active
active
```

Journal da temizdi: "daemon hazır", hata yok. **Hiçbir şey değişmemişti.**
Birimler `/usr/local/lib/panely/` altını çalıştırıyor:

```
ExecStart=/usr/local/lib/panely/panelyd \
```

Yani eski ikili çalışmaya devam ediyordu ve göç 0002 hiç uygulanmadı.

### Arıza NASIL yakalandı

Yeşil sinyallerle değil, **beklenen bir sonucun yokluğuyla**: yeni tabloların
(`apps`, `releases`) var olması gerekiyordu, yoktular. Kurulumun kendisine
bakan hiçbir kontrol bunu göremezdi — üç sinyal de (exit 0, `is-active`,
temiz journal) doğruydu ve hepsi yanlış soruyu yanıtlıyordu.

Kodun sağlam olduğu ayrıca ölçüldü: aynı göç mantığı sunucu
veritabanının bir KOPYASINA karşı koşturulunca tabloları sorunsuz yarattı.
Yani sorun kodda değil, **değiştirdiğimizi sandığımız dosyadaydı**.

### Kural

Kurulum betiği hedefi TAHMİN ETMEZ, birimden OKUR; ve sonucu ÇALIŞAN
SÜREÇTEN doğrular, kurduğu yoldan değil:

```bash
DEST=$(systemctl cat panelyd | sed -n 's|^ExecStart=\(.*\)/panelyd .*|\1|p' | head -1)
...
pid=$(pgrep -x panelyd)
md5sum "$(readlink /proc/$pid/exe)" "/tmp/panelyd"
```

İki md5 eşitse ve `readlink` beklenen yolu gösteriyorsa, çalışan şey
gerçekten kurduğumuz şeydir. Bu, "servis active" ile
"servis DOĞRU İKİLİYİ çalıştırıyor" arasındaki farkı kapatan tek kontrol.

⚠ **Dilim 4b bunu devralıyor.** `internal/bootstrap/install.sh`'a Caddy
eklenecek ve Hetzner kutusu ZATEN bootstrap'lanmış durumda: yani betik
mevcut kuruluma karşı yeniden koşturulabilir olmalı ve sonucu yine
çalışan süreçten doğrulanmalı.

Aynı ailenin üçüncü örneği: "kurulumdan sonra active" kabul ölçütü değil
(systemd birimi yeniden başlatmayı geçmeli), garanti ortamda `t.Skip`
yasak, ve şimdi bu.

---

## K-050 — Caddy'ye panelyd konuşur; yetkinin sınırı BINARY'de çizildi

Dilim 4b'nin ilk sorusu "Caddy admin soketine kim konuşuyor"du. İki aday
vardı: panelyd doğrudan (bütçe maliyeti yok) veya executor üzerinden
(şema beyaz listesi, ama bütçeyi kesin aşar — 2497/2500).

Soru kod yazmadan ÖLÇÜLDÜ ve ölçüm ikisini de değiştirdi.

### Ölçüm 1 — dosya izinleri zaten bir sınır çiziyor

| kullanıcı | exec.sock | api.sock |
|---|---|---|
| `nobody` (≈ caddy) | ENGELLENDİ | ENGELLENDİ |
| `panely` | bağlandı | bağlandı |

Yani "ele geçirilmiş panelyd, Caddy'ye `reverse_proxy unix//…/exec.sock`
yazıp executor'ı internete açar" senaryosu, Caddy `panely` grubuna
girmediği sürece **dosya sistemi tarafından** kapalı.

### Ölçüm 2 — asıl tehdit başkaydı: TLS özel anahtarı

Stok Caddy'ye admin soketinden şu yüklendi:

```json
{"handler":"file_server","root":"/var/lib/caddy/.local/share/caddy","browse":{}}
```

ve o dizindeki dosya **düz HTTP üzerinden servis edildi**. Orada alan
adlarının TLS özel anahtarları durur.

Yani stok Caddy ile "panelyd ters vekili yapılandırabilir" yetkisi,
sessizce "panelyd ele geçirilirse alan adının özel anahtarı KALICI olarak
çalınabilir" demek oluyordu. Ele geçirilmiş bir panelyd zaten hangi
sürümün aktif olduğuna karar veriyor (yani alan adında istediği içeriği
servis edebiliyor); anahtarın çalınabilmesi bunun ÜSTÜNE eklenen ve
sunucu kurtarıldıktan sonra da süren gerçek bir delta.

⚠ Bu ölçümün ilk iki denemesi YANLIŞ "güvenli" raporladı — bkz. K-051.

### Karşı önlem olarak "veri dizinini kısıtla" ÇALIŞMIYOR

`file_server` dosyayı Caddy'nin KENDİ kimliğiyle okuyor: aynı süreç,
aynı uid. Süreç içinde sınır yok. systemd sertleştirmesi de yardım etmez,
çünkü Caddy o dizini okuyabilmek zorunda.

### Çözüm: sınır BINARY'de

`build/caddy` — Caddy'nin yalnızca gereken modülleriyle derlenmiş hâli.
`file_server`, `templates` ve `caddyfs` **binary'de hiç yok**.
Doğrulanan değil, TEMSİL EDİLEMEYEN bir yetenek; exec.proto'daki "host
yolu kabul EDİLMEZ, hiç alınmaz" kararının aynısı.

Bedava değildi ama zaten gerekiyordu: Ubuntu'nun Caddy'si 2.6.2 (Kasım
2022) ve admin soketinin modunu ayarlayamıyor (`|0660` sözdizimi 2.7'de
geldi), Faz 2'nin `caddy-dns/cloudflare` eklentisi de özel derleme
istiyor. xcaddy tek başına yetmezdi: o yalnızca modül EKLEYEBİLİYOR,
ÇIKARMAK için özel bir main.go şart.

Ayrı bir Go modülü (`build/caddy/go.mod`): Caddy'nin bağımlılık ağacı
panely'nin go.mod'unu ve `go test ./...` süresini şişirmesin.

### Soket sahipliği: üyelik DEĞİL, grup sahipliği

Caddy soketi kendisi yaratırsa grup olarak kendi birincil grubunu
kullanıyor — ölçüldü: `srw-rw---- caddy:caddy`, panely bağlanamıyor.
(Unix soketine bağlanmak YAZMA izni ister.)

Çözüm systemd soket aktivasyonu: soketi systemd yaratıyor
(`SocketUser=caddy SocketGroup=panely SocketMode=0660`), Caddy `fd/3`
olarak devralıyor. Caddy `panely` grubuna GİRMİYOR; yalnızca soketin
grup sahibi panely.

Değerlendirilen alternatif `ExecStartPost` ile root'un `chgrp` yapmasıydı;
çalışıyor ama doğru izinler Caddy başladıktan SONRA oturuyor ve kurulum
bir zamanlama yarışına bağlanıyor. Soket aktivasyonunda pencere yok.

⚠ `.socket` birimi varsayılan olarak aynı adlı `.service`'i tetikliyor;
`Service=caddy.service` satırı olmadan birim hiç başlamıyor (ölçüldü).

### Nihai tasarımın ölçümü (altısı birlikte)

| # | özellik | sonuç |
|---|---|---|
| 1 | soket `caddy:panely` 0660 | ✓ |
| 2 | caddy `panely` grubunda DEĞİL | ✓ |
| 3 | panely admin API'ye ulaşıyor | ✓ |
| 4 | caddy exec.sock'a ulaşAMIYOR | ✓ |
| 5 | `file_server` yapılandırması REDDEDİLİYOR | ✓ HTTP 400 |
| 6 | `reverse_proxy` ÇALIŞIYOR | ✓ |

6. satır olmadan ölçüm hiçbir şey kanıtlamazdı: her yapılandırmayı
reddeden bozuk bir binary de 5.'i geçerdi.

### Kalan yüzey (açıkça yazılıyor)

Kapatılan şey kalıcı anahtar hırsızlığıydı. Kapatılmayan şey `dial`
hedefinin serbest olması — ve bu, ilk yazdığımdan DAHA GENİŞ:

> ⚠ İlk hâli "panelyd zaten trafiği istediği yere yönlendirebiliyordu"
> diyordu. Yanlış. Önceden yönlendirebildiği şey KENDİ YÖNETTİĞİ
> KONTEYNERLERDİ; Caddy admin erişimiyle `dial` alanına herhangi bir
> adres yazabiliyor — `127.0.0.1:<herhangi bir port>` dahil.
>
> Yani host'ta yerel çağıranlara güvenen bir servis çalışıyorsa, Caddy
> ona açılan public bir köprü hâline gelir. Bugün hostta öyle bir servis
> yok (kontrol düzlemi unix soketlerinde ve onlara Caddy erişemiyor —
> ölçüldü), bu yüzden acil değil. Ama denklik iddiası fazlaydı ve
> düzeltiliyor.

**Yükümlülük (dilim 4b, `internal/proxydrv`):** `dial` alanı çağırandan
ALINMAZ; Panely'nin yönettiği konteyner adreslerinden KURULUR. Aynı
desen ContainerCreate'te imaj alanının olmamasıyla aynı:

	"Serbest bir tutamaç, hostta çalışan HERHANGİ bir şeye işaretçidir."

### İkinci yükümlülük: yükledikten sonra GERİ OKU

`POST /load` 200 dönmesi, canlı yapılandırmanın gönderdiğimiz şey olduğunu
kanıtlamaz. Admin soketine `panely` olarak (veya root olarak) çalışan
başka bir süreç de yazabilir ve kontrol düzlemi bunu göremez; o zaman
SQLite'taki "gerçeğin kaynağı" canlı olmayan bir şeyi tarif eder.

proxydrv, yüklemeden sonra `GET /config/` ile geri okuyup gönderdiğiyle
karşılaştırmalı. Bu, `aux` → `image_id` → `DeploySucceeded` zincirinin
ters vekildeki karşılığı: "hata almadım" değil, "istediğim durumu
doğruladım".

`build/caddy/main.go`'daki dışlama listesi bir "yapılacaklar" değil
GÜVENLİK SINIRIDIR; oraya modül eklemeden önce yukarıdaki ölçüm
tekrarlanmalı.

---

## K-051 — Bir güvenlik ölçümü, ÖLÇEBİLDİĞİNİ kanıtlamadan sonuç vermemeli

K-050'nin sızdırma testi **üç kez** koştu ve ilk ikisi YANLIŞ "güvenli"
raporladı:

| deneme | gerçekte olan | betiğin dediği |
|---|---|---|
| 1 | Caddy hiç başlamamıştı (`bind: permission denied`) | "✓ SIZDIRILAMIYOR" |
| 2 | `/load` 403 aldı, sonra Caddy düştü; istek HTTP 000 | "✓ REDDEDILDI" |
| 3 | gerçekten ölçüldü | ⚠ SIZDIRILABILIYOR |

Her iki yanlış geçişte de mekanizma aynı: **cevapsızlık istenen cevap
sayıldı**. Boş gövde "anahtar servis edilmedi" gibi, HTTP 000 "yapılandırma
reddedildi" gibi okundu.

Bu, bu oturumda görülen diğer iki örnekle aynı aile:
`panelyd -version` boş çıktı verdi (dosya yoktu, `|| true` yuttu) ve
`os._exit()` stdio tamponunu boşaltmadığı için alt süreçlerin çıktısı hiç
görünmedi. Üçünde de "hiçbir şey duymadım" → "sorun yok".

**Why:** Bir güvenlik kontrolünde bu yön ÖLÜMCÜL. Sıradan bir testte
yanlış negatif zaman kaybettirir; güvenlik ölçümünde açık bir deliği
kapalı ilan ettirir.

**How to apply:** Ölçüm betiği, sonucu raporlamadan önce ÖLÇEBİLDİĞİNİ
kanıtlamalı:

```bash
require_live() {
    code=$(curl -s -o /dev/null -w "%{http_code}" ... /config/)
    [ "$code" = "200" ] || { echo "OLCUM GECERSIZ"; exit 1; }
}
```

Ve sonuç kodları AÇIKÇA ayrılmalı — `case` ile 200/400/diğer; "200
değilse reddedilmiştir" varsayımı tam olarak yukarıdaki 2. satırdır.

Ayrıca her güvenlik ölçümünün bir KARŞI KONTROLÜ olmalı: "tehdit
kapandı" ölçümünün yanında "sistem hâlâ işini yapıyor" ölçümü. K-050'de
bu 6. satır; onsuz bozuk bir binary de güvenli görünürdü.

---

## K-052 — Doğrulanan yapılandırma ile GÖNDERİLEN yapılandırma aynı olmalı

K-050'nin altı ölçümü geçti — ama **elle kurulmuş bir durum üzerinde**.
Beş ayrı probe betiği sırayla `/etc/tmpfiles.d/`, birim drop-in'leri ve
`/etc/caddy/config.json` yazmıştı; hiçbiri repoda yoktu.

Bu, K-049'un bir üst seviyesi. Orada "servis active ama ikili eski"ydi;
burada "ölçüm geçti ama ölçülen artefakt gönderilecek olan değil".

### Ölçüm: iki gerçek boşluk

`enable` edilmemişti — yalnızca `start`. Repo artefaktları kurulurken
çıktı bunu gösterdi:

```
Created symlink /etc/systemd/system/sockets.target.wants/caddy-admin.socket → …
```

Yani ilk yeniden başlatmada soket hiç yaratılmayacak ve Caddy `fd/3`
üzerinde `getsockopt: socket operation on non-socket` ile ölecekti.

`/etc/caddy/config.json` de yalnızca bir probe betiğinin ürünüydü. İçindeki
`"origins": ["localhost"]` **taşıyıcı bir alan**: onsuz panelyd'nin her
`POST /load` isteği HTTP 403 "host not allowed" alıyor (ölçüldü).

### Kabul ölçütü: YENİDEN BAŞLATMA

Artefaktlar repoya alındı (`deploy/caddy/config.json`,
`deploy/systemd/panely-caddy-tmpfiles.conf`), sunucuya onlar kuruldu,
elle kurulmuş durum SİLİNDİ ve makine yeniden başlatıldı.

Yeniden başlatma sonrası, kimse bir şeye dokunmadan:

| kontrol | sonuç |
|---|---|
| `caddy-admin.socket`, `caddy`, `panelyd`, `panely-exec` | dördü de `active` |
| `/run/caddy` | `drwxr-x--- caddy panely` (tmpfiles yeniden kurdu) |
| soket | `srw-rw---- caddy panely` |
| journal'da yeniden başlatma döngüsü | 0 |
| K-050'nin altı ölçümü | altısı da geçti |
| dilim 4a regresyonu (`panely deploy`) | r3 derlendi, çıkış 0 |

`/run` bir tmpfs; dizin yeniden başlatmayı geçmiyor ve `systemd-tmpfiles`
onu her açılışta yeniden kuruyor. Bu, elle `mkdir` ile kurulmuş bir
dizinin sessizce kaybolacağı anlamına geliyordu.

**Kural:** bir yapılandırma "doğrulandı" sayılmadan önce (1) repodaki
artefaktlardan kurulmuş, (2) yeniden başlatmayı geçmiş olmalı. Elle
biriktirilmiş durum üzerinde alınan ölçüm, o durumun kendisi hakkındadır
— gönderilecek olan hakkında değil.

---

## K-053 — Bütçe freni çalıştı: yükseltilmedi, KÜÇÜLTÜLDÜ

Ters vekil hostta çalışıyor ve konteynerler host portu yayınlamıyor;
Docker'ın gömülü DNS'i de yalnızca ağın içinden çözüyor. Dolayısıyla
panelyd'nin Caddy'ye verecek bir upstream adresi yok — `ManagedContainer`
adres taşımıyordu.

Eklemenin maliyeti ölçüldü: **~11 kod satırı** (dar `NetworkSettings`
yapısı + çıkarım + proto alanı). Bütçe 2497/2500'dü, yani **aşacaktı**.

### K-040'ın freni: önce küçültmeyi ara

Kural "sınır aşılamaz" değil; "yükseltmeden ÖNCE küçültme seçeneğinin
neden reddedildiği YAZILI olarak gerekçelendirilmeli". Arandı ve bulundu.

`internal/pbconv` her iki dönüşüm yönünü de taşıyordu ve
`cmd/panely-exec`'in içe aktarma grafiğindeydi. Ama ölçüldü:

```
executor   → yalnızca pbconv.AuditRecordsToProto   (kendi günlüğünü dışa yazar)
execclient → yalnızca pbconv.AuditRecordsFromProto (executor'ın yanıtını okur)
```

Çözümleme yönü — `AuditRecordFromProto`, `AuditRecordsFromProto`,
`outcomeFromProto`, `sourceFromProto` — **58 kod satırı** ve root süreçte
**hiç çalışmıyor**. Ayrıcalıklı binary'nin çalıştırmadığı kodu taşıması
için bir sebep yok: hem bütçeyi hem elle denetlenecek yüzeyi büyütüyordu.

`internal/execclient/auditconv.go`'ya taşındı (dışa aktarılmadan).

### Sonuç

| adım | bütçe |
|---|---|
| başlangıç | 2497 |
| pbconv çözümleme yönü taşındı | **2439** |
| `ip_address` eklendi | **2448** |

Sınıra **dokunulmadı**; boşluk 3 satırdan 52'ye çıktı.

### Eklenen alan neden yeni bir YETKİ değil

`ip_address` SALT OKUNUR ve zaten var olan salt okunur bir RPC'ye
ekleniyor. Ele geçirilmiş bir panelyd'ye yeni bir şey yaptırmıyor:
konteynerleri zaten listeleyebiliyordu ve Docker ağları hosttan zaten
erişilebilir. Öğrendiği şey "hangi adres", "neye izinli" değil.

Adresi **executor doldurur**, panelyd'den alınmaz — ve yalnızca
uygulamanın KENDİ ağındaki adres okunur. Konteyner başka bir ağa da
bağlıysa oradaki adres bilerek göz ardı ediliyor.

### Yan kazanç: testin yeri düzeldi

Gidiş-dönüş testleri artık `internal/execclient`'ta — iki yönün de
görünür olduğu tek yer. `pbconv` test dosyasız kaldı ama yönü sınanmıyor
değil: gidiş-dönüş testi `pbconv.AuditRecordsToProto`'yu çağırıyor.

### Bu, metriğin DÖRDÜNCÜ değişimi DEĞİL

K-040 "ölçtüğü şeye uyacak şekilde sürekli ayarlanan bir metrik, ölçmeyi
bırakır" diyordu. Burada metrik değişmedi — kapsam, birim ve sınır aynı.
Değişen şey ÖLÇÜLEN KOD: ayrıcalıklı grafikten oraya ait olmayan bir
parça çıkarıldı. Kural tam olarak bunu üretmek için yazılmıştı.

---

## K-054 — Geri okuma İKİ YÖNLÜ olmalı; tek yön, kontrolün var olma sebebini kaçırıyordu

K-050 panelyd'ye Caddy'nin admin soketini verdi ve karşılığında bir
yükümlülük yazdı: `POST /load` sonrası `GET /config/` ile geri oku, çünkü
"200 aldım" canlı yapılandırmanın benimki olduğunu kanıtlamaz.

Yazılan kod bu yükümlülüğü YARIM karşılıyordu. `verifyApplied`, yalnızca
*gönderdiğim her rota canlıda var mı* diye soruyordu. Canlıdaki FAZLA
rotalar hiç bakılmayan taraftaydı.

### Kaçırdığı iki durum

**1. Kontrolün var olma sebebi.** Yorumun kendisi "admin soketine yazan
başka bir süreç varsa" diyordu. Ama başka bir sürecin yazması, canlıda
benim göndermediğim bir rota BIRAKIR — ve tam da o taraf okunmuyordu.
Yani kod, yorumunun andığı mekanizmayı uygulamıyordu.

**2. Sessiz dağıtım hatası.** `POST /load` kök nesnenin tamamını
değiştiriyor. Dağıtılan uygulamadan üretilmiş bir yapılandırma, diğer
uygulamaların rotalarını siler. Tek yönlü karşılaştırmada "benim rotam
canlıda" der ve YEŞİL geçerdi; ikinci uygulama internetten düşmüş olurdu.

Ayrıca `if len(wantRoutes) == 0 { return nil }` kestirmesi en tehlikeli
durumda susuyordu: bütün rotaların KALDIRILMASI istendiği hâlde canlıda
durmaya devam etmesi.

### Neden yanlış alarm vermiyor — ÖLÇÜLDÜ

Çift yönlü karşılaştırmanın gerçek riski şuydu: Caddy otomatik HTTPS için
HTTP→HTTPS yönlendirme rotaları üretiyor. Bunları saklanan yapılandırmaya
geri YAZSAYDI, kontrol her yüklemede patlardı — ve yanlış alarm veren bir
kontrol kapatılmaya mahkûmdur.

Gerçek sunucuda, gerçek `panely-caddy` binary'siyle iki deney:

| deney | gönderilen | `GET /config/` dönen |
|---|---|---|
| `automatic_https.disable_certificates: true` | 1 rota | **1 rota**, ek alan yok |
| `automatic_https.disable: true` | 1 rota | **1 rota**, ek alan yok |

Caddy'de **saklanan yapılandırma** ile **sağlanmış çalışma zamanı durumu**
ayrı: `GET /config/` POST edileni döndürüyor, otomatik HTTPS
genişlemesinden sonraki hâli değil. Yani dönen yapıdaki her fark Caddy'nin
normalleştirmesi değil, başka bir yazarın izidir.

### Doğrulama

İki yeni test, düzeltmede yeşil ve **mutasyonda kırmızı**: `verifyApplied`
tek yönlü hâline geri döndürüldüğünde ikisi de düşüyor. Mutant DERLENDİ,
yani sonuç geçerli — derlenmeyen bir mutant "yakalandı" sayılmaz (K-043).

### Doğurduğu yükümlülük

Dağıtım akışı, yapılandırmayı **TÜM uygulamaların** aktif sürümlerinden
üretmek zorunda. Tek uygulamadan üretmek artık sessizce geçmiyor —
yükleme hata veriyor — ama doğru davranış zaten baştan buydu.

---

## K-055 — Ters vekil dağıtım paketine bağlanmıyor; birim de depodan geliyor

Önceki tasarım `apt install caddy` yapıp bir systemd drop-in ile
`ExecStart`'ı `panely-caddy`'ye çeviriyordu. Bırakıldı; yerine
`panely-caddy.service`, `panely-caddy-admin.socket`, tmpfiles kuralı ve
`caddy.json` **depodan** gidiyor.

### ÖNCE çürütülen gerekçe

Bırakma sebebi olarak akla ilk gelen şey şuydu: "paket güncellenirse
çalışan ikili stok Caddy'ye döner." Yazılmadı, çünkü **ölçüldü ve YANLIŞ
çıktı**:

```
ÖNCE : /usr/local/lib/panely/panely-caddy
apt-get install --reinstall -y caddy   → exit 0
SONRA: /usr/local/lib/panely/panely-caddy
```

Drop-in mekanizması sağlamdı; dpkg `/etc/systemd/system/` altına
dokunmuyor. Ölçülmeseydi bu, gerekçe diye yazılmış bir yanlış olurdu.

### Gerçek gerekçeler

1. **Taşınabilirlik.** `caddy`, Ubuntu'da `universe` bileşeninde
   (ölçüldü: `noble-updates/universe`), Debian'ın kendi depolarında ise
   hiç yok. `panely bootstrap` bir dağıtım paketinin varlığına bağlı
   olamaz.

2. **K-052.** Doğrulanan yapılandırmanın TAMAMI depoda olmalı. Devralınan
   birimin içeriği depoda değildi: dağıtım onu güncellediğinde
   doğruladığımız kurulum sessizce değişirdi.

3. **Paketin verdiği tek şey** bir kullanıcı, üç dizin ve ezdiğimiz bir
   birimdi. Çalıştırdığımız ikili zaten bizimki. Bu bir bağımlılık değil,
   tören — ve yanında hiç çalıştırmadığımız, dosya servis eden modüller
   içeren bir binary'yi diskte tutuyordu.

Yan kazanç: drop-in bir ALT DİZİN gerektiriyordu
(`caddy.service.d/10-panely.conf`), oysa bootstrap'ın tar üreticisi düz
bir `ad → yol` haritası kullanıyor. Kendi birimimizle bu sorun tamamen
ortadan kalktı.

### Kurulum artık K-050 SINIRINI ÖLÇÜYOR

En değerli parça bu. install.sh, kurduğu binary'ye `list-modules`
soruyor ve dosya servis eden bir modül bulursa **duruyor**. Sınır artık
yalnızca `build/caddy/main.go`'da iddia edilmiyor; kurulum anında,
çalıştırılacak ikilinin üzerinde ölçülüyor.

⚠ **Pozitif kontrol ÖNCE.** Doğrudan "file_server var mı" diye sormak,
binary hiç çalışmasa bile "yok" cevabı üretirdi — boş çıktıda grep hiçbir
şey bulmaz. Bu yüzden önce `http.handlers.reverse_proxy`'nin VARLIĞI
kanıtlanıyor; ancak ondan sonra yokluk iddiası anlam taşıyor. Sıra bir
testle zorlanıyor (`TestInstallScriptProvesTheModuleBoundaryWasMeasured`),
çünkü K-051 aynı hatayı üç kez üretti.

Gerçek sunucuda: **114 modül, dosya servisi 0, reverse_proxy var.**

### `--resume` KULLANILMIYOR — ve bunun bir bedeli var

Caddy `--resume` ile son API yapılandırmasını diskten geri yükler.
Kullanılsaydı Caddy'nin diski ile SQLite ayrı birer "gerçek" olurdu.
Model açık: gerçeğin kaynağı kontrol düzlemi, Caddy onun yansıması.

**Bedeli ölçüldü ve saklanmıyor.** Yeniden başlatmadan sonra:

```
ss -lntp | grep -E ':80 |:443 '   →  (boş)
```

Rota olmadığı için Caddy portları HİÇ DİNLEMİYOR. Yani bir çökme,
`systemctl restart` veya reboot sonrası bütün uygulama trafiği, panelyd
yapılandırmayı geri yükleyene kadar düşer.

**Doğurduğu yükümlülük:** panelyd açılışta — ve ters vekil yeniden
başladığında — Caddy'yi SQLite'tan UZLAŞTIRMAK ZORUNDA. Bu, dilim 4b'nin
dağıtım akışında karşılanıyor.

### Doğrulama: kurulum + REBOOT

Sunucudan `caddy` paketi purge edildi (ikili silindi, `:80` boşaldı),
sonra `panely bootstrap` sıfırdan koşturuldu — yani "paketi olmayan
makine" senaryosu gerçekten sınandı. Kurulum sonrası 13 kontrolün 13'ü
geçti.

Ardından **reboot**:

| kontrol | sonuç |
|---|---|
| dört birim de `active` **ve** `enabled` | ✓ |
| `/run/panely-caddy` (tmpfs) yeniden kuruldu, 750 panely-caddy:panely | ✓ |
| `admin.sock` 660 panely-caddy:panely | ✓ |
| çalışan imaj = kurulan binary (`/proc/<pid>/exe` md5) | ✓ |
| `NRestarts=0` — çökme döngüsü yok | ✓ |
| `panely` olarak `GET /config/` → **200** | ✓ |
| `panely-caddy` olarak `exec.sock` → erişemiyor | ✓ |

Son iki satır modelin can alıcı noktası: kontrol düzlemi ters vekili
yönetebiliyor, ters vekil ayrıcalıklı executor'ı GÖREMİYOR.

---

## K-056 — Özel depo kimliği RPC'den GEÇMEZ; bedeli beyaz listeyle sınırlandı

> ⚠ **BU KAYDIN SEÇTİĞİ MEKANİZMA ÇÜRÜTÜLDÜ — bkz. K-057.**
> "Hostta duran git kimlik bilgisini dockerd kullanır" iddiası gerçek
> sunucuda YANLIŞ çıktı: moby, git'i `HOME=/dev/null` +
> `GIT_CONFIG_NOSYSTEM=1` ile çalıştırıyor. Aşağıdaki *beyaz liste* yarısı
> geçerli ve doğrulandı; *kimlik bilgisi* yarısına göre iş yapmayın.

Kullanıcının üç Vercel projesi de ÖZEL depolarda ve Panely hiçbirini
derleyemiyordu. Ölçüldü, varsayılmadı:

```
panely: depo sorgusu 401 Unauthorized döndü (github.com/erkanrzgc/portfolio)
```

`BuildContextURL` düz `https://host/owner/repo.git#sha` üretiyor — kimlik
bilgisi taşımıyor ve sır kasası Faz 2'de.

### Kimlik bilgisi NEREDEN akmalı? — ölçüldü

Üç aday vardı: (a) token'ı RPC'den geçirmek, (b) istemcinin tarball
göndermesi, (c) hostta duran bir git kimlik bilgisini dockerd'nin
kullanması.

(c)'nin çalışıp çalışmadığı BİLİNMİYORDU. Gerçek token kullanmadan
ölçüldü: root'a *sahte* bir credential helper kuruldu ve çağrılıp
çağrılmadığına bakıldı.

```
=== KONTROL: düz git bu yardımcıya danışıyor mu? ===  EVET (2 kez)
=== ASIL ÖLÇÜM: dockerd uzak git bağlamını çekerken ===
  >>> dockerd YARDIMCIYI ÇAĞIRDI
      === cagrildi: get ===
      protocol=https
      host=github.com
      wwwauth[]=Basic realm="GitHub"
```

Kontrol grubu önce koşuldu: yardımcı düz `git` tarafından çağrılmasaydı,
dockerd'nin çağırmaması bir şey KANITLAMAZDI (K-051).

Seçilen yol (c). Kazandırdıkları: kimlik bilgisi RPC'den geçmiyor, proto
değişmiyor, ayrıcalıklı yüzey bütçesi kimlik için hiç artmıyor, ve token
kontrol düzlemine HİÇ girmiyor — operatör doğrudan hosta koyuyor.

### Bedeli: token'ın erişimi Panely'nin erişimi olur

Somut saldırı: `ImageBuild` çağırabilen biri `owner/repo`'yu kurbanın
özel deposuna çevirir.

⚠ Kurban deposunda Dockerfile OLMASI GEREKMEZ. `dockerfile_path` de
istekten geliyor ve derleme çıktısı `Deploy` üzerinden istemciye AYNEN
akıyor. Yani kaynak, hiçbir konteyner çalışmadan sızar. (İlk
değerlendirmede "Dockerfile'ı olmayan depo sızdırılamaz" diye
düşünülmüştü; YANLIŞTI.)

"İnce taneli token kullanın" yeterli bir cevap değil çünkü
DOĞRULANAMAZ: operatörün token'ı hangi depolara açtığını executor
göremez.

### Zorlanabilir yarı: depo beyaz listesi

`-allow-repo owner/repo,...` — executor'ın YAPILANDIRMASINDAN gelir,
istekten değil. `allowedGitHosts` ile aynı desen.

- **Harfe duyarsız**: GitHub/GitLab owner/repo'da harf ayrımı yapmıyor;
  duyarlı olsaydı kısıt tek harfle aşılırdı.
- **Bozuk girdi KAPALI tarafa düşer**: yazım hatası olan girdi hiçbir
  depoyla eşleşmez, derleme reddedilir. Sessizce fazla izin veren bir
  kısıt, hiç olmayan bir kısıttan kötüdür.
- **Boş liste = kısıt yok**: kimlik bilgisi yokken doğru davranış.

### Zorlanamayan yarı — ve neden gizlenmiyor

Executor "hostta kimlik bilgisi var mı" sorusunu GÜVENİLİR biçimde
yanıtlayamıyor: kimlik başka bir credential helper'da olabilir.
`.gitconfig` varlığına bakmak yanlış pozitif üretir ve başlamayı
reddetmek ağır bir yan etkidir.

Bu yüzden yapılamayan şey yapılıyormuş gibi gösterilmiyor. Yapılan:
durum GÖRÜNÜR kılınıyor — executor açılışta `depo_kisiti=YOK (kısıt
uygulanmıyor)` yazıyor. Operatörün göreceği tek satır bu.

### Bütçe

| adım | satır |
|---|---|
| başlangıç | 2448 |
| ilk (yapılandırmacı + tip'li) beyaz liste | 2478 |
| **sadeleştirilmiş hâli** | **2462** |
| doğrulamaya bağlanması + bayrak | **2479** |

İlk tasarım 30 satır yiyordu ve kalan işle sınırı zorluyordu; K-040'ın
freni uygulandı, tip ve yapılandırmacı atıldı, maliyet 14 satıra indi.
Sınıra DOKUNULMADI.

### Doğrulama

4 test; mutasyonda (kısıt etkisizleştirilince) ikisi KIRMIZI.

---

## K-057 — K-056 YANLIŞTI: dockerd host git kimliğini KULLANAMAZ

K-056, özel depoların "hostta duran bir git kimlik bilgisiyle"
derlenebileceğini söylüyordu ve bunu ölçtüğünü iddia ediyordu. **Yanlış.**
Gerçek sunucuda uçtan uca denendiğinde derleme şu hatayla öldü:

```
docker: HTTP 500: error fetching: fatal: could not read Username
for 'https://github.com': No such device or address
```

Token yerindeydi, doğruydu ve çalışıyordu — sorun oydu ki dockerd onu
okuyamıyor.

### Neden — ölçüldü, tahmin edilmedi

Derleme sırasında dockerd'nin başlattığı git süreci `/proc` üzerinden
yakalandı ve ortamı okundu:

```
git -c protocol.file.allow=never fetch origin -- 6c0b5548403f…
  ppid=1020 (dockerd)
  GIT_CONFIG_NOSYSTEM=1
  HOME=/dev/null
  GIT_PROTOCOL_FROM_USER=0
```

moby, git'i **kasten** hiçbir host yapılandırmasını okuyamayacak biçimde
çalıştırıyor:

| değişken | neyi kapatıyor |
|---|---|
| `HOME=/dev/null` | `~/.gitconfig`, `~/.git-credentials` |
| `GIT_CONFIG_NOSYSTEM=1` | `/etc/gitconfig` |

Yani kimlik bilgisinin **nerede durduğu önemli değil**. Bu bir
yapılandırma hatası değil, moby'nin sertleştirmesi; "doğru yere koymak"
diye bir çözüm yok.

Ara adımlar da ölçüldü ve hepsi elendi:

| deneme | sonuç |
|---|---|
| `git config --global` (root) | dockerd'de HOME yok → görünmez |
| `git config --system` (`/etc/gitconfig`) | dosya ad alanında GÖRÜNÜYOR ama `NOSYSTEM=1` okutmuyor |
| dockerd ortamını birebir taklit + düz git | ÇALIŞIYOR — yani engel ortamda değil, moby'nin eklediği üç değişkende |

### K-056'daki ölçüm neden yanıltıcıydı

K-056'da root'a sahte bir credential helper kurulmuş ve "dockerd
yardımcıyı ÇAĞIRDI" sonucuna varılmıştı. `HOME=/dev/null` +
`NOSYSTEM=1` altında bu **mümkün değil**. Gözlenen çağrı kontrol
grubundaki düz `git`'ten geliyordu ve dockerd'ye atfedildi.

Bu, K-051'in tarif ettiği hatanın ta kendisi — bu sefer kendi karar
kaydımızda. Ders güncelleniyor: **kontrol grubu ile asıl ölçümün
çıktıları ayırt edilebilir olmalı.** İkisi aynı kanala yazıyorsa,
kontrolün başarısı ölçümün başarısı gibi okunur. Ayırt edici olan şey
burada süreç soyağacıydı (`ppid`), çıktının kendisi değil.

### Neyi ETKİLEMİYOR

Depo beyaz listesi (K-056'nın diğer yarısı) geçerli ve gerçek sunucuda
DOĞRULANDI:

| depo | listede | sonuç |
|---|---|---|
| `erkanrzgc/portfolio` | ✓ | kapıdan geçti (`Internal`, kimlik hatası) |
| `erkanrzgc/panely` | ✗ | kapıda reddedildi (`InvalidArgument`) |

İki farklı hata kodu, kapının ayrım yaptığını gösteriyor — her şeyin
birden bozulmadığını.

Ayrıca K-056'nın tehdit modeli de artık geçersiz: dockerd host kimliğini
kullanamadığı için "token'ın gördüğü her özel depo sızdırılabilir"
saldırısı bu yolla KURULAMIYOR. Beyaz liste yine de duruyor: seçilecek
mekanizma kimliği bir biçimde derlemeye ulaştıracak ve kısıt o gün
gerekecek.

### Geriye kalan seçenekler (henüz seçilmedi)

| yol | kimlik nereden | bedeli |
|---|---|---|
| İstemci bağlamı yükler | iş istasyonundaki mevcut git kimliği | akış RPC'si; sunucu hiç token tutmaz |
| Executor kimliği okuyup URL'e gömer | host dosyası | token `/proc/<pid>/cmdline`'da görünür |
| BuildKit + secret | host dosyası | derleyici değişimi, K-042 yeniden ölçülmeli |
| Faz 2 kasasına ertele | kasa | özel depo desteği gecikir |

Karar verilene kadar hostta duran token **atıl**: dockerd onu
kullanamıyor, executor git çalıştırmıyor. Yani ne fayda ne risk
üretiyor.

---

## K-058 — Tek sunucu iki port dinleyince site ŞİFRESİZ servis ediliyordu

Faz 1'in 2. kabul ölçütü ilk kez gerçek alan adıyla koşuldu ve GEÇTİ:

```
issuer  = C=US, O=Let's Encrypt, CN=YE1
subject = CN=panely.erkanrzgc.dev
```

Ama aynı ölçüm bir kusur ortaya çıkardı: `http://` aynı içeriği **308
yerine 200 ile, şifresiz** veriyordu.

### Sebep

Ürettiğimiz Caddy yapılandırmasında tek bir sunucu hem `:80` hem `:443`
dinliyordu. Caddy bu durumda o sunucu için otomatik HTTP→HTTPS
yönlendirmesi EKLEMEZ — aynı sunucu iki portu da servis ettiği için
rotalar düz HTTP üzerinde de eşleşir.

Yani "otomatik HTTPS açık" olması yetmiyordu; sertifika alınıyor,
HTTPS çalışıyor, ama düz metin yolu da açık kalıyordu. Bu, **yalnızca
gerçek bir alan adıyla dışarıdan ölçüldüğünde** görünür: `hello.localhost`
ile yapılan bütün önceki testler bu kusuru göremezdi, çünkü orada zaten
HTTPS'e bakılmıyordu.

### Düzeltme

Sunucu artık YALNIZCA HTTPS portunu dinliyor. Ölçüldü: Caddy bunun
üzerine kendi `:80` sunucusunu kuruyor (ayrı fd) ve o sunucu hem ACME
HTTP-01 doğrulamasını karşılıyor hem yönlendirmeyi yapıyor.

```
listen = [':443']                       (yapılandırma)
:443 users:(("panely-caddy",pid=768,fd=8))
:80  users:(("panely-caddy",pid=768,fd=14))   ← Caddy'nin kendi kurduğu
```

Düz portu bırakmak bir yetenek kaybı değil; tersine, elle üstlendiğimiz
bir işi Caddy'ye geri veriyor.

`http_port`/`https_port` alanları da uygulama düzeyine eklendi: sunucu
varsayılan dışı bir port dinlediğinde Caddy yönlendirme hedefini bu
alanlardan hesaplıyor. Boş bırakılsalardı 443 varsayılır ve yönlendirme
yanlış porta yapılırdı — üstelik SESSİZCE, çünkü HTTPS tarafı yine
çalışırdı.

### Doğrulama

| ölçüm | önce | sonra |
|---|---|---|
| `http://panely.erkanrzgc.dev/` | **200 (şifresiz)** | **308 → https** |
| `https://.../` | 200 | 200 |
| `https://.../privacy` | 200 | 200 |
| `https://.../yok-boyle` | 404 | 404 |
| sertifika vereni | Let's Encrypt | Let's Encrypt |

İki birim testi eklendi; ikisi de düzeltmeden önce KIRMIZI koştu.

### Ders

Bu kusur aylardır koddaydı ve bütün testler yeşildi. Görünür olması için
gereken tek şey **gerçek bir alan adında, dışarıdan, HTTP tarafına
bakmak**tı. `hello.localhost` yönlendirmeyi kanıtlıyordu ama TLS
davranışını hiç sınamıyordu — "trafik akıyor" ile "trafik doğru akıyor"
arasındaki fark burada.

---

## K-059 — Vekil, upstream'in sıkıştırmasını AÇIP düz metin gönderiyordu

Kullanıcı "çok yavaş açılıyor" dedi. Ölçüldü:

| | Panely | Vercel |
|---|---|---|
| JS paketi | **311.929 bayt** | 104.354 bayt |
| `Content-Encoding` | **yok** | `br` |
| toplam süre | ~8,5 sn (bir denemede 20 sn'de timeout) | ~2,6 sn |

### Sebep — katman katman ölçüldü

```
doğrudan nginx (konteyner)  → Content-Encoding: gzip     ✓
Caddy üzerinden             → content-length 311929, encoding YOK
```

Yani sıkıştırma upstream'de VARDI, vekilde kayboluyordu.

Caddy'nin vekil taşıyıcısı (Go `http.Transport`) upstream'den kendiliğinden
`Accept-Encoding: gzip` istiyor ve yanıtı ŞEFFAF biçimde açıyor. Üretilen
yapılandırmada `encode` işleyicisi olmadığı için Caddy açtığı gövdeyi düz
metin olarak gönderiyordu.

Bu tek bir uygulamanın sorunu değildi: **Panely'nin servis ettiği HER
uygulama sıkıştırmasız gidiyordu.** Uygulamanın kendi sunucusunda gzip
açık olması hiçbir şey değiştirmiyor — bu, "uygulama doğru yapılandırılmış"
diye bakıp geçilecek bir kusur değil, vekilin kusuru.

### Düzeltme

Her rotanın işleyici zincirine `encode` eklendi, **reverse_proxy'den ÖNCE**.
Sıra taşıyıcı: sonra gelseydi hiç çalışmazdı, çünkü vekil yanıtı çoktan
yazmış olurdu.

Kodlayıcılar `zstd` ve `gzip` ile sınırlı. brotli YOK: stok Caddy'de
bulunmuyor, eklenti gerektiriyor. Sınırın gerçekliği ölçüldü —
`panely-caddy list-modules` → `http.encoders.gzip`, `http.encoders.zstd`,
`http.handlers.encode`. Olmayan bir kodlayıcı istenseydi Caddy
yapılandırmanın TAMAMINI reddeder ve o an canlı olan bütün rotalar
düşerdi; test bu sınırı sabitliyor.

### Doğrulama

| | önce | sonra | Vercel (CDN) |
|---|---|---|---|
| boyut | 311.929 | **105.822** (zstd) | 104.354 (br) |
| toplam | ~8,5 sn | **1,0–2,0 sn** | 0,6–0,9 sn |

Kalan fark yapısal ve kapatılamaz: Vercel küresel bir CDN kenarından,
Panely Nürnberg'deki TEK bir VPS'ten servis ediyor. TCP el sıkışması
0,67 sn'ye karşı 0,21 sn — bu mesafenin kendisi.

### Ders

Kusur, uygulamayı doğru yapılandırmakla gizlendi: nginx gzip'i açıktı ve
konteynere doğrudan bakan herkes "sıkıştırma çalışıyor" görürdü.
Görünmesi için ölçümün İSTEMCİNİN durduğu yerden yapılması gerekiyordu.
Ara katman, doğru yapılandırılmış iki ucun arasında sessizce bir özelliği
düşürebiliyor.

---

## K-060 — Sağlık kapısı: yoklama panelyd'de, çit systemd'de

Kapı bugüne kadar konteynerin **çalıştığını** ölçüyordu, uygulamanın
**cevap verdiğini** değil. Açılan ama 500 dönen bir uygulama — bozuk bir
commit'in en yaygın hâli — kapıdan geçip canlıya alınırdı. Faz 1'in 4.
kabul ölçütü tam olarak bunu yasaklıyor.

### Yoklama neden panelyd'de, executor'da değil?

| | panelyd | panely-exec |
|---|---|---|
| yetki | uid 999, yeteneksiz | **root**, Docker soketi |
| bütçe | yok | **2479/2500 satır** |

Bir HTTP istemcisi (`net/http` + `net/url` + bağımlılıkları) ayrıcalıklı
bütçeyi tek başına patlatırdı; kalan boşluk 21 satır. Ama asıl gerekçe
bütçe değil **geri alınabilirlik**: yetkisiz bir sürece ağ vermek
`IPAddressAllow` ile çitlenebilir ve istenirse geri alınır; ayrıcalıklı
bir binary'ye eklenen satırlar orada kalır.

### Çit

```
RestrictAddressFamilies=AF_UNIX AF_INET     # AF_INET6 kasten yok
IPAddressDeny=any
IPAddressAllow=172.16.0.0/12
```

Aralık **ölçülerek** seçildi, tahmin edilerek değil:

```
panely-web        172.18.0.0/16
panely-pfprobe    172.19.0.0/16
panely-portfolio  172.20.0.0/16
```

Docker'ın varsayılan yerel havuzu 172.17–172.31 aralığında; hepsi
`172.16.0.0/12` içinde.

**192.168.0.0/16 KASTEN açılmadı.** Docker o havuzu ancak 172.x tükendiğinde
(≈15 ağ) kullanır. Açsaydık panelyd, kalıcı sunucunun (Legion) **ev ağına**
erişebilirdi. Sınıra dayanılırsa yoklama `permission denied` ile düşer ve
dağıtım kapıda durur — sessiz değil, görünür bir arıza.

⚠ **Yükümlülük:** dockerd'nin adres havuzu `daemon.json` ile bu aralığa
sabitlenmeli. Yapılmadı; sınıra dayanmak bugün için uzak ama kalıcı çözüm bu.

### Ölçüm — çit gerçekten ısırıyor mu?

Yoklayıcı, panelyd'nin **cgroup'una girerek** koşturuldu. systemd'nin
`IPAddressAllow` politikası bir BPF programıdır ve birimin cgroup'una
bağlıdır; aynı cgroup'a konan her süreç aynı filtreden geçer. Bu yüzden
ölçülen şey "aynı direktiflerle kurulmuş bir kopya" değil, **çalışan
birimin kendi politikası** (K-052).

| hedef | panelyd içi | kontrol grubu | sonuç |
|---|---|---|---|
| konteyner `172.20.0.3:8080` | BAĞLANDI | BAĞLANDI | izin doğru |
| `1.1.1.1:80` | engellendi | BAĞLANDI | engellendi |
| `169.254.169.254:80` | engellendi | BAĞLANDI | engellendi |
| `127.0.0.1:22` | engellendi | BAĞLANDI | engellendi |

**Her satırda kontrol grubu var.** "1.1.1.1'e ulaşılamadı" tek başına
hiçbir şey kanıtlamaz — sunucunun interneti kapalı da olabilir. Anlamlı
olan fark.

⚠ İlk deneme HTTP ile ölçüyordu ve kontrol grubu "başarısız" göründü:
1.1.1.1 **301** dönüyor, port 22 HTTP konuşmuyor. Betik bunu başarı saymak
yerine "ölçüm geçersiz" dedi. `IPAddressAllow` **soket** katmanında
çalıştığı için doğru ölçüm ham TCP bağlantısıdır; HTTP araya gereksiz bir
çeviri katmanı koyuyordu.

`10.0.0.0/8` gibi "izin listesinde olmayan başka bir özel ağ" hedefi
**kasten yok**: orada dinleyen bir şey olmadığından kontrol grubu da
bağlanamıyor ve "engellendi" ile "zaten kimse yok" ayırt edilemiyor.
Ölçemediğimiz bir şeyi geçmiş saymaktansa hiç sınamamak dürüst.

### Ölçüm — yoklama gerçekten koşuyor mu?

Gerçek bir dağıtım koşturuldu. nginx erişim günlüğü doğrudan tanıklık
ediyor:

```
172.20.0.1 - - [21:28:56] "GET / HTTP/1.1" 200 826 "Go-http-client/1.1"
172.20.0.1 - - [21:28:58] "GET / HTTP/1.1" 200 826 "Go-http-client/1.1"
172.20.0.1 - - [21:29:00] "GET / HTTP/1.1" 200 826 "Go-http-client/1.1"
```

Tam **üç** yoklama, tam **iki** saniye arayla — `DefaultGate.Successes=3`,
`Interval=2s`. `172.20.0.1` Docker köprüsünün geçidi, yani host, yani
panelyd.

### Yönlendirme İZLENMİYOR

İzlenseydi, dağıtılan uygulama 302 döndürerek panelyd'ye istediği adrese
istek attırabilirdi: kontrol düzlemi, iş yükünün seçtiği bir hedefe
bağlanan bir araca dönüşürdü. Çit ikinci katmanda da kapatıyor ama savunma
istemcide başlıyor. Gövde de sınırlı okunuyor (4 KB).

URL bir **yapıdan** kuruluyor, dize birleştirmesinden değil; konak ayrı bir
alan olduğu için hiçbir yol değeri hedefi kaydıramaz.

### Boş sağlık yolu

HTTP yoklamasını **açıkça** kapatır (HTTP konuşmayan iş yükleri için).
Yoklayıcının kendisi ise zorunlu: `nil`'i "yoklama yok" diye kabul etmek,
kapının sessizce eski seviyesine düşmesi demekti.

### Ders

Bu değişiklik dört yorumu birden yalana çevirdi ve biri her dağıtımda
kullanıcının ekranına basılıyordu ("kapı uygulamanın cevap verdiğini
ölçmez"). K-056 tam olarak böyle olmuştu. **Bir mekanizma değiştiğinde,
onu anan her yorum aramayla bulunup düzeltilmeli** — `grep AF_UNIX` beş
yerde eskimiş iddia buldu.

---

## K-061 — Boşaltma: eski sürüm durduruluyor ama SİLİNMİYOR

Gerçekte gözlendi: `panely_portfolio_r1_0` 28 saat boyunca `Up` kaldı.
Trafik almıyordu (uzlaştırıcı yalnızca aktif sürümü rotalar) ama kaynak
tüketiyordu. Her dağıtımda bir konteyner birikiyordu.

### Sıra taşıyıcıdır

```
kapı → SetActiveRelease → Caddy yüklendi → BOŞALTMA → durdur
```

Boşaltma penceresi olmadan: ters vekil yeni upstream'lere çevrilse bile
eski konteynerlere **uçan** istekler var. Anında öldürmek, her dağıtımda
bir avuç kullanıcıya yarım yanıt göstermek demek. Pencereden **önce**
durdurmak daha da kötü olurdu: o an trafiği **alan** konteyner ölürdü.

Testte bu sıra, durdurma anındaki ters vekil yükleme sayısı okunarak
sabitlendi. Yalnızca uykuları saymak yetmiyordu — boşaltma yanlışlıkla
Caddy'den önce yapılsaydı süreler aynı görünürdü.

### En ince durum

Uygulama uzlaştırmada **atlandıysa** trafik taşınmamıştır; ters vekilde
hâlâ eski sürümün rotası duruyordur. Bu durumda boşaltmaya hiç
girilmiyor — aksi hâlde dağıtım, düzeltmeye çalıştığı siteyi kendisi
düşürürdü.

### Durdurma hatası dağıtımı başarısız SAYMAZ

Trafik o noktada zaten taşındı ve site sağlıklı. Ayrı bir `DrainError`
tipi çağıranın farkı görmesini sağlıyor: düz hata dönseydi CLI çalışan bir
dağıtıma "başarısız" der, kullanıcı da muhtemelen geri alırdı.

### Neden silinmiyor?

Duran bir konteyneri yeniden başlatmak saniyeler sürer; imajdan yeniden
kurmak dakikalar. Geri alma bunun üstüne oturacak. Silme politikası
(son N sürümü tut) dağıtım geçmişiyle birlikte gelecek.

## K-063 — Alan adı benzersizliği ŞEMADA, doğrulayıcıda değil

İki uygulama aynı alan adını taşıyabiliyordu. Sonuç tek bir uygulamanın
bozulması değil: `proxydrv.BuildConfig` yinelenen alan adında
yapılandırmanın **tamamını** reddediyor (haklı olarak — hangisinin
kazandığı sıraya bağlı olurdu). Bundan sonra hiçbir uzlaştırma
başarılamaz ve panelyd yeniden başladığında **hiçbir rota kurulmaz**:
sunucudaki bütün siteler düşer.

Bunu bir doğrulayıcıya bırakmak, o doğrulayıcıyı atlayan her yolun (göç
betiği, elle SQL, ileride eklenecek bir RPC) aynı deliği yeniden açması
demekti. Şema kısıtı böyle bir yol bırakmıyor.

İndeks **kısmi** (`WHERE domain != ''`): alan adı olmayan uygulama
geçerli ve yaygın, düz bir UNIQUE ikinci alan adsızı reddederdi.

### Göç canlı veriye uygulanmadan ÖNCE ölçüldü

Başarısız bir göç `schema_migrations`'a yazılmıyor (tek transaction),
yani panelyd **her açılışta** aynı yerde ölürdü — ve onarım aracı
(`panely app update`) o panelyd'nin içinde. Araç kendini kilitler.

Ölçüm: canlı veritabanının kopyasına indeks kuruldu → çıkış 0, indeks
mevcut. **Kontrol grubu zorunluydu**: ikinci bir kopyaya kasten
yinelenen alan adı eklendi → çıkış 19, `UNIQUE constraint failed:
apps.domain`. Kontrol grubu olmadan "kopyada kuruldu" hiçbir şey
kanıtlamazdı; sqlite3 hiç koşmasa da aynı çıktı alınabilirdi.

İlk ölçüm denemesi `RC=True` bastı: PowerShell çift tırnak içindeki `$?`
işaretini uzak kabuğa **göndermeden önce kendisi** genişletti. Ölçülen
şey uzak sqlite3'ün çıkış kodu değil, PowerShell'in kendi durumuydu.

Göç 14 Ağustos akşamı gerçek sunucuda uygulandı ve `schema_migrations`
listesinde doğrulandı.

## K-064 — Çakışma açıklaması yazma DENEMESİNDEN sonra soruluyor

`UpdateApp` alan adı çakışmasını önden kontrol etmiyor. Akla yakın olan
"bu alan adı başkasında mı" diye sormaktı, ama o sorgu uygulamanın
**kendi satırını** da bulur: `WHERE domain = ?` yazan bir kontrol, alan
adına hiç dokunmayan bir güncellemeyi bile çakışma sanıp reddederdi ve
doğruluğu `AND id != ?` yazmayı hatırlamaya bağlı kalırdı.

Yazmayı deneyip hatayı açıklamak o sınıfı tamamen siler: bir satırı
kendi değeriyle güncellemek benzersizlik indeksini zaten ihlal etmez.
**Kendiyle çakışma temsil edilemez hâle geliyor**, doğrulanan değil —
`exec.proto`'daki yasak alan mantığının aynısı. Ek fayda: başarılı yolda
fazladan sorgu yok.

### CreateApp'in eski eşlemesi bu göçle YALANA döndü

Göç 0004'ten önce `apps` tablosundaki tek benzersizlik kısıtı birincil
anahtardı, dolayısıyla "ihlal ⇒ kimlik zaten var" çıkarımı **doğruydu**.
İndeks o çıkarımı geçersiz kıldı: aynı hata sınıfı artık iki sebepten
doğuyor ve eski eşleme, alan adı çakışmasını *"uygulama zaten var:
&lt;henüz-yaratılmamış-kimlik&gt;"* diye raporlardı — hem yanlış hem de
yanlış alanı gösteren bir mesaj.

Bu K-056'nın sınıfı, ama farklı bir biçimi: bir mekanizma değişince ona
dayanan **çıkarımlar** da sessizce yalana döner, yalnızca yorumlar değil.
Sürücünün hata KODU ayrımı taşımıyor (ikisi de `SQLITE_CONSTRAINT_UNIQUE`)
ve mesaj metnine bakmak sürücü sürümüne bağımlılık olurdu; tek dürüst
kaynak veritabanının kendisi.

## K-065 — `app update` alan adı değiştirince UZLAŞTIRMA da koşuyor

`Reconcile` yalnızca iki yerden çağrılıyordu: panelyd açılışı
(`cmd/panelyd/main.go`) ve dağıtım (`internal/deploy/rollout.go`).
Ölçüldü, varsayılmadı.

Yani `app update -domain` tek başına trafiği **taşımazdı**: alan adı
veritabanında değişir, canlıda hiçbir şey olmaz, komut "başarılı" der.
Kullanıcı taşındığını sanır ve bunu ancak yeni alan adı cevap vermeyince
fark eder. Bu işin var olma sebebi apex'i dağıtımsız taşımak olduğu için
uzlaştırıcı API sunucusuna da veriliyor.

### Üç sonuç, üç ayrı cevap

Hepsini "tamam" diye raporlamak en tehlikeli ikisini gizlerdi:

| durum | cevap |
|---|---|
| uzlaştırma başarılı, uygulama rotalı | trafik taşındı |
| uzlaştırma başarılı, uygulama ATLANDI | alan adı kaydedildi ama trafik taşınmadı; `panely deploy` gerekiyor |
| uzlaştırma başarısız | hata — ve mesaj değişikliğin **kaydedildiğini** söylüyor |

Üçüncüsü `DrainError`'ın sınıfı: işlem oldu, arkasından gelen adım
olmadı. Düz bir "güncelleme başarısız" mesajı kullanıcıyı kaydın eski
değerde kaldığını sanmaya iterdi — oysa yeni değerde.

### Uzlaştırma yalnızca alan adı GERÇEKTEN değişince koşuyor

Caddy'nin `POST /load` ucu kök nesnenin tamamını değiştiriyor, yani her
uzlaştırma sunucudaki bütün sitelerin yapılandırmasını yeniden yazıyor.
Dala dokunan bir güncelleme yüzünden bunu yapmak gereksiz risk.

### Gerçek sunucuda ölçüldü — "satır değişti" kabul ölçütü DEĞİL

`web` uygulaması `hello.localhost` → `merhaba.localhost` taşındı:

| ölçüm | önce | sonra |
|---|---|---|
| Caddy rotası | `hello.localhost` | `merhaba.localhost` |
| `hello.localhost` | HTTP 200 | **cevapsız** |
| `merhaba.localhost` | **cevapsız** | HTTP 200 |
| denetim zinciri | — | `app.update · app/web · {"domain":"..."}` |

İki yön de ölçüldü. Yalnızca yeni alan adının cevap verdiğine bakmak
yetmezdi: eski rota da silinmiş olmalı, yoksa iki alan adı aynı
uygulamaya gider ve bir sonraki taşımada çakışırdı. Diğer iki sitenin
(`pf.localhost`, `panely.erkanrzgc.dev`) rotalarına dokunulmadığı ayrıca
doğrulandı — uzlaştırma yapılandırmanın tamamını yeniden yazdığı için bu
gerçek bir risk.

Denetim kaydında **yalnızca** `domain` var; dokunulmayan alanlar
girmemiş.

## K-066 — "Verilmedi" ile "boşalt" üç katmanda birden ayrı

Boş dize hem `domain` hem `health_path` için **geçerli** bir değer
("ters vekilde görünme", "HTTP yoklaması yapma"). Tam-tanım-değiştirme
modelinde alanı doldurmayan bir istemci onları sessizce silerdi — ve bu
iki alan tam olarak bu işin var olma sebebi. `replicas` aynı hataya düşse
doğrulayıcıya çarpıp gürültülü ölürdü; yani sessizce kaybolabilecek iki
alan, en çok önem taşıyan ikisi.

Ayrım üç katmanda da korunuyor: proto3 `optional`, `store.AppUpdate`
işaretçileri ve CLI'da `fs.Visit`. Go'nun `flag` paketi
"`-domain` verilmedi" ile `-domain=""` durumlarını aynı boş dizeye
indirger; `fs.Visit` **yalnızca gerçekten ayarlanmış** seçenekleri gezer.
Bu olmasaydı dala dokunmak isteyen bir kullanıcı alan adını da silerdi.

### `container_port` KASTEN değiştirilemez

`store.Deployment` portu `apps`'ten JOIN'le **canlı** okuyor.
Değiştirmek, bir sonraki uzlaştırmada ters vekili çalışan konteynerlerin
dinlemediği bir porta yönlendirir ve siteyi anında düşürür. Port
değişikliği yeni bir dağıtım gerektirir; dağıtımsız temsil edilebilir
olması bir tuzaktı.

Git kaynağı (host/owner/repo) da değiştirilemez: deponun değişmesi bir
güncelleme değil, başka bir uygulamadır.

### Ayrıcalıklı yüzeye maliyeti SIFIR

Şemaya iki mesaj ve bir RPC eklendi, bütçe 2479/2500'de kaldı.
`check-exec-surface.sh` üretilen protobuf kodunu (`internal/pb/*`)
bütçeden dışlıyor ve dışlamayı körü körüne yapmıyor: o dizindeki her
dosyanın "Code generated ... DO NOT EDIT" başlığı taşıdığı
doğrulanıyor, yani orası elle kod saklanacak bir yer değil.

## K-067 — Dönen struct'a bakan test, diski ölçmez

`UpdateApp` satırı transaction içinde okuyor, bellekte değiştiriyor ve
geri yazıyor; döndürdüğü struct bu **bellek kopyası**. `release_seq`
korunma testi bu kopyaya bakıyordu.

Mutasyon geçişinde ölçüldü: SQL cümlesine `release_seq = 0` eklendiğinde
— yani sayaç diskte gerçekten sıfırlandığında — test **yeşil kaldı**.
Sayaç bellekteki kopyada hâlâ 3'tü.

Sayacın sıfırlanması sinsi bir hata: bir sonraki sürüm yine `r1` adını
alır ve hostta **var olan** konteynerleri adresler — iki farklı commit,
aynı ad.

Düzeltme yalnızca o testi değil üçünü birden kapsadı; aynı kusur
hepsinde vardı. `mustUpdate` yardımcısı artık diskten yeniden okuyor
**ve** dönen değerle diskteki hâli karşılaştırıyor: ayrışırlarsa çağıran,
yazılmamış bir durumu doğru sanır.

Bu, K-047'nin ("yeşil test hiçbir şey korumayabilir") dördüncü örneği ve
ilk üçünden farklı bir biçimi: iddia doğru şeyi kontrol ediyordu ama
**yanlış yerden** okuyordu.

### Zaman damgası testi de saat çözünürlüğüne takıldı

`updated_at > created_at` iddiası kırmızı verdi: bu makinede ardışık iki
`time.Now()` çağrısı **aynı nanosaniyeyi** döndürdü (fark = 0s; Windows
sistem saati ~15ms'de bir ilerliyor). Araya `Sleep` koymak testi saat
çözünürlüğüne bağımlı bırakırdı; damga bilinen eski bir değere çekilerek
iddia saatten tamamen bağımsız hâle getirildi.

## K-068 — İddiayı kullanıcının OKUDUĞU katmana yaz

K-065'te sunucu, alan adı yazıldıktan sonra ters vekil güncellenemezse
hatanın içinde değişikliğin **KAYDEDİLDİĞİNİ** söylüyor; mesajın var olma
sebebi kullanıcının kaydın eski değerde kaldığını sanmasını engellemekti.
Sunucu tarafındaki test bunu doğruluyordu ve yeşildi.

Ama CLI o hatayı diğer komutlardaki gibi sarmalıyordu:

```go
return c.fail(fmt.Errorf("uygulama güncellenemedi: %w", err))
```

Terminalde çıkan satır kendi kendisiyle çelişiyordu:

```
panely: uygulama güncellenemedi: alan adı ... KAYDEDİLDİ, ama ...
```

Operatör ilk üç kelimeyi okuyup **tam ters** sonuca varır. Yani sunucudaki
özenli mesaj, bir satır aşağıda tersine çevriliyordu.

Ön ek artık bir sonuç iddia etmiyor (`app update:`), yalnızca hangi
komutun konuştuğunu söylüyor; sunucunun mesajları zaten kendi kendini
açıklıyor. Mutasyonla doğrulandı: eski ön ek geri konunca yeni test
kırmızıya dönüyor.

**Bu, K-067 ile aynı sınıfın ikinci biçimi.** Orada test doğru şeyi
kontrol ediyordu ama yanlış YERDEN okuyordu (bellek kopyası, disk değil);
burada doğru şeyi kontrol ediyordu ama yanlış KATMANDA (sunucu hatası,
kullanıcının gördüğü satır değil). Aynı dilimde iki kez çıkması tesadüf
değil: bir mesajın değeri onu okuyan yerde ölçülür.

## K-069 — Geçmiş, ikinci tabloya değil AYNI tabloya yazıldı

`deployments` uygulama başına tek satır tutuyordu (`app_id` birincil
anahtar) ve her aktivasyon bir öncekini eziyordu. "Önceki AKTİF sürüm"
sorusunun şemada cevabı yoktu, yani `panely rollback` yazılamıyordu.

İki tasarım vardı:

| | ayrı `deployment_history` tablosu | tabloyu ekle-sadece yapmak |
|---|---|---|
| göç | ucuz (yalnızca CREATE) | tablo yeniden kurulur |
| "hangisi aktif" | **İKİ kaynak** | tek kaynak |
| tutarlılık | tetikleyiciyle sürdürülür | yapısal |

İkincisi seçildi. Gerekçe: iki tablo, "aktif sürüm" sorusuna iki cevap
üretebilen bir sapma sınıfı açar (`deployments` r5 der, geçmişin açık
satırı r3 der) ve bunu tetikleyiciyle senkron tutmak, tek bir indeksin
bedava verdiği garantiden daha fazla makine demektir.

Kaybedilmemesi gereken şey 0003'ün asıl kazancıydı: "bir uygulamanın aynı
anda iki aktif sürümü olamaz" bir kontrol değil, **temsil edilemez** bir
durumdu. Kısmi tekil indeks aynı garantiyi geçmişi silmeden veriyor:

```sql
CREATE UNIQUE INDEX idx_deployments_one_active
    ON deployments (app_id) WHERE deactivated_at IS NULL;
```

Kapanmış satırlar indeksin dışında kaldığı için geçmiş istendiği kadar
birikiyor. Mutasyonla doğrulandı: `UNIQUE` kaldırılınca ilgili test
kırmızıya dönüyor.

### Sıra numarası aktivasyon geçmişi DEĞİLDİR

Geri alma hedefi `releases.seq - 1` olamaz. r5 canlıyken r3'e geri
alınırsa, bir sonraki geri alma r2'ye değil **r5'e** gitmelidir — gerçekten
canlı olan en son önceki sürüm odur. Bu yüzden hedef, son KAPANMIŞ
dağıtım satırından okunuyor.

Testin bunu ayırt ettiği ölçüldü: `releases.seq` tabanlı yanlış uygulama
denendiğinde testin ilk adımı (r5 → r4) **geçiyor**, ayırt edici üçüncü
adımı `"r2" — "r5" olmalıydı` diye kırmızı veriyor.

### UPDATE tetikleyicisi KASTEN geri konmadı

0003'te BUILT kontrolü hem INSERT hem UPDATE'te vardı. Ekle-sadece
şemada aktivasyon yalnızca INSERT; UPDATE sadece açık satırı kapatıyor.
Oraya BUILT kontrolü koymak, aktifken FAILED'a düşmüş bir sürümün satırını
**kapatılamaz** yapardı — yani geri almanın en gerekli olduğu anda geri
almayı engellerdi.

### Mekanizma değişince onu anan her yorum tarandı

`app_id` birincil anahtarı kalktığı için onu gerekçe gösteren iki yorum
yalan oldu ve düzeltildi (`deployments_test.go`, `api/deploy.go`).
Uygulanmış göç 0003'ün gövdesine dokunulmadı (0002'nin gerekçesi), yalnızca
başına "bu dosya tarihseldir, 0005 tabloyu yeniden kurdu" uyarısı eklendi.

## K-070 — Boş veritabanıyla koşan göç testi, göçü sınamaz

Bütün depo testleri boş bir `:memory:` veritabanıyla başlıyor ve göçleri
tek seferde uyguluyor. Bu kurulumda 0005'in

```sql
INSERT INTO deployments_new ... SELECT ... FROM deployments
```

satırı **sıfır satır** taşıyor — ve sıfır satırlık bir kopya her zaman
başarılıdır. Yani göçün asıl işi olan veri taşıma yolu, testler yeşilken
bile hiç çalışmamıştı. İlk gerçek koşusu canlı sunucuda olacaktı.

İki ölçüm eklendi:

1. **Birim testi**, göçleri elle 0004'e kadar getirip ESKİ şemaya bir
   dağıtım satırı yazıyor, sonra 0005'i uyguluyor ve satırın AÇIK olarak
   hayatta kaldığını ölçüyor.
2. **Gerçek veri**, Hetzner yedeğinin (`panely-hetzner-20260817.tgz`)
   kopyası üzerinde koşturuldu:

```
GÖÇ ÖNCESİ         →  GÖÇ SONRASI
pfprobe   r1          pfprobe   r1  pf.localhost
portfolio r3          portfolio r3  panely.erkanrzgc.dev
web       r1          web       r1  hello.localhost
```

Canlı `portfolio r3` dağıtımı olduğu gibi taşındı. Bu, [[K-047]]
sınıfının göç katmanındaki karşılığı: yeşil bir test, sınadığını
sandığın kod yolunu hiç çalıştırmıyor olabilir.

## K-071 — Yeşil kalan mutasyon, testin değil MUTASYONUN zayıflığı olabilir

Mutasyon geçişinde ekle-sadece tetikleyicisi zayıflatıldı ve test **yeşil
kaldı**. İlk okuma "test hiçbir şey ölçmüyor" idi.

Yanlıştı. Mutasyon şu satırı değiştiriyordu:

```sql
WHEN NEW.seq            IS NOT OLD.seq     -- ← yalnızca bu silindi
  OR NEW.app_id         IS NOT OLD.app_id
  OR NEW.release_id     IS NOT OLD.release_id
  ...
```

Yani tetikleyici hâlâ `app_id`, `release_id` ve `activated_at`
değişikliklerini yakalıyordu — testin sınadığı üç şeyin üçünü de. Test
doğru çalışıyordu; **mutasyon** hedefi ıskalamıştı. `WHEN` bloğunun tamamı
etkisizleştirilince test beklendiği gibi kırmızıya döndü.

Ders, mutasyon testinin kendisine dair: bir mutasyonun yeşil kalması iki
şey demek olabilir ve ikisi zıt sonuçlar doğurur —

- test zayıf → testi düzelt
- **mutasyon zayıf** → mutasyonu düzelt

Ayırt etmeden birincisine atlamak, çalışan bir testi "işe yaramıyor" diye
gevşetmeye götürür. Mutasyonun gerçekten hedefi bozduğu, önce
doğrulanmalı. Aynı geçişte bir mutasyon da desen uyuşmazlığından hiç
uygulanamamıştı ve betik bunu sessizce geçmek yerine
`MUTASYON UYGULANAMADI` diye rapor ettiği için fark edildi — ölçüm
aracının kendi başarısızlığını bildirmesi, [[K-047]]'nin araca uygulanmış
hâli.

## K-072 — Geri alma, dağıtımın KUYRUĞUNU paylaşır; başını paylaşamaz

`panely rollback` yazıldı. İlk içgüdü `Rollout.Run`'ı yeniden çağırmaktı;
çalışmıyor. Hedef sürümün konteynerleri hostta **zaten var** — boşaltma
onları durduruyor ama silmiyor (K-061) — ve `Run` doğrudan
`CreateReplica` çağırdığı için Docker aynı adla ikinci konteyneri 409 ile
reddediyor.

Ayrım şurada: **baş** farklı, **kuyruk** aynı.

```
dağıtım:   derle → konteynerleri KUR   ─┐
geri alma: (derleme yok) → VAR OLANI BAŞLAT ─┤
                                            ├→ switchTraffic:
                                            │   KAPI → SetActiveRelease
                                            │   → uzlaştır → boşaltma
                                            └   → eskiyi durdur
```

Kuyruk kopyalansaydı sıranın taşıdığı garantiler iki yerde ayrı ayrı
korunmak zorunda kalırdı ve biri düzeltilirken diğeri unutulurdu; sonuç,
geri alma sırasında siteyi düşüren bir sıra hatası olurdu.

**Paylaşımın gerçek olduğu ölçüldü.** `switchTraffic` içindeki sağlık
kapısı mutasyonla devre dışı bırakıldığında **iki test birden** kırmızıya
döndü: yeni `TestRollbackStopsAtHealthGate` ve mevcut
`rollout_test.go:433` (bozuk commit testi). Tek bir noktanın iki yolu da
koruduğunun kanıtı bu.

### Kapı geri almada da işliyor

"Bu sürüm daha önce çalışıyordu" bir sağlık kanıtı DEĞİL: imaj bozulmuş,
bağımlı bir servis düşmüş olabilir. Kapı atlanırsa geri alma, siteyi
kurtaran değil **ikinci kez düşüren** işlem olur — hem de operatörün en
çaresiz olduğu anda.

### RPC akışlı DEĞİL

`Deploy` akışlı çünkü derleyicinin ham çıktısını taşıyor ve başarının
ölçütü pozitif bir son mesaj (K-042). Geri alma hiçbir şey derlemiyor;
taşınacak çıktı olmayınca akış iki mesajlık bir tören olurdu. Tekil
çağrıda ölçüt zaten pozitif: yanıt geldiyse trafik taşınmıştır.

Yanıt `from` ve `to` sürümlerini birlikte döndürüyor. Operatörün
doğrulaması gereken şey "komut hata vermedi" değil, trafiğin nereden
nereye taşındığı; yalnızca hedefi döndürmek, yanlış uygulamaya komut
verildiğinde bunu görünmez kılardı.

## K-073 — Yan etkisini atlayan sahte, koca bir katmanı sınanamaz yapar

API katmanındaki `fakeRollout`, `Run` çağrıldığında yalnızca çağrıyı
kaydediyordu. Gerçek `Rollout` ise kapıdan sonra `SetActiveRelease`
çağırıyor — **dağıtım geçmişi oradan doğuyor.**

Sonuç: sahte sessizce yalan söylüyordu. Geri alma testleri yazılınca
ortaya çıktı; üç kez dağıtım yapılıp geri alınmak istendiğinde depo
"uygulamanın aktif sürümü yok" dedi, çünkü hiçbir dağıtım geçmişe
girmemişti.

Bu, K-047 ailesinin yeni bir biçimi. Öncekiler testin kendisiyle
ilgiliydi (zayıf iddia, yanlış yerden okuma, yanlış katman); bu ise
**düzenekle**: sahte, gerçeğin bir yan etkisini atlayınca o yan etkiye
dayanan HER davranış — geri almanın hedefi, ardışık geri almalar, denetim
kaydı — o katmanda sınanamaz hâle geliyor. Testler yazılamadığı için de
eksiklik "kapsama düşük" diye bile görünmüyor; hiç akla gelmiyor.

Düzeltme sahteyi gerçeğe yaklaştırmak değil, **yalan söylemesini
engellemek**: `fakeRollout` artık isteğe bağlı bir depo alıyor ve
verildiğinde aktivasyonu gerçekten yazıyor.

Kural: bir sahte, taklit ettiği şeyin **gözlemlenebilir yan etkilerini**
atlıyorsa, o sahteyi kullanan testler o yan etkiye dayanan hiçbir şeyi
kanıtlamaz — ve bunu fark etmenin tek yolu, o davranışı sınamayı
denemektir.
## K-074 — Legion projeden çıktı: üç bağımsız engel, her biri tek başına yeterli

Eski Lenovo dizüstü ("Legion") 17 Ağustos'ta kalıcı sunucu olsun diye
kuruldu: Ubuntu 24.04.4, Docker Hetzner'le birebir aynı sürüme
sabitlendi, LVM elle genişletildi, kapak kapanma davranışı ayarlandı,
insansız yeniden başlatma ölçüldü ve geçti. Yani makine teknik olarak
hazırdı. Buna rağmen sunucu OLAMIYOR ve karar iptaldir.

Üç ayrı engel birikti; üçü de ayrı ayrı ölçüldü ve **her biri tek başına
yeterliydi**:

**1. Gelen bağlantı hiç içeri girmiyor (18 Ağu).** Ev ağı üç kat NAT
arkasında ve en üstte Starlink CGNAT var (`100.64.0.0/10`, RFC 6598).
Zyxel'e 80/443 yönlendirme kuralları girildi ve dışarıdan yine erişim
olmadı; tcpdump paketin modeme HİÇ ulaşmadığını gösterdi, yani sorun
yapılandırmada değil taşıyıcıda. Panely'de şu an yalnızca ACME HTTP-01
var, o da gelen 80'e muhtaç. Anten komşunun olduğu için plan değiştirmek
de kullanıcının elinde değil.

**2. Elektrik kesintisinden sonra kendi açılmıyor (1 Eyl).** BIOS'un
tamamı gezildi: InsydeH20 Setup Utility, sekmeler yalnızca
`Information / Configuration / Security / Boot / Exit`. `Power` ya da
`Advanced` sekmesi yok; Configuration'daki tek "power" girdisi
`Power Beep`. `Restore on AC Power Loss` tüketici Lenovo dizüstülerinde
bulunmuyor. Kısa kesintiyi batarya kurtarır, uzun kesintide makine kapanır
ve elektrik gelince açılmaz.

Wake-on-LAN bunu KURTARMIYOR — ilk düşünüldüğü gibi değil. NIC yeteneği
ölçüldü (`ethtool enp7s0` → `Supports Wake-on: pumbg`, yani magic packet
destekli), ama sihirli paketi gönderecek makine de aynı elektriği
kullanıyor; kesintide o da kapalı. WoL yalnızca makineyi KASTEN kapatıp
başka bir şey ayaktayken uyandırmak için anlamlı.

**3. Makine taşındı ve fiziksel erişim bitiyor (1 Eyl).** Legion başka bir
konuma alındı, yeni WiFi'ye elle bağlandı; kullanıcının ~7 Eylül'den sonra
makineye fiziksel erişimi kalmayacak. Düğmeye basacak kimse yok.

**Sonuç: Hetzner kalıcı barındırıcıdır.** `panely-test`, cx23
(2 vCPU / 4 GB / 40 GB), nbg1, 6,49 €/ay net. Taşıma hiç yapılmadığı için
"geri dönüş" diye bir iş de yok — apex ve portfolyo baştan beri orada.
Legion Ubuntu'suyla duruyor, projeden çıkarıldı; kimseye zararı yok.

**Asıl ders planlamayla ilgili.** Bu plan haftalarca "kalıcı sunucu
Legion olacak" varsayımını taşıdı ve engeller tek tek çıktıkça tek tek
çözülmeye çalışıldı: port yönlendirme kuralları girildi, DDNS düşünüldü,
Tailscale kuruldu, ethernet kablosu alınacaklar listesine yazıldı. Her
adım kendi başına makuldü. Oysa plan **birbirinden bağımsız N şeyin aynı
anda doğru çıkmasına** bağlıydı ve her biri belirsizdi.

Kural: bir plan bağımsız birden çok belirsizliğe aynı anda dayanıyorsa,
engelleri sırayla çözmeye çalışmadan önce planın kendisi yeniden
değerlendirilmeli. Sırayla çözmek, her adımda ilerliyormuş hissi verirken
toplam başarı olasılığının düştüğünü gizler.

**Korunan kazanım:** Tailscale'in adres modeli. Makine bambaşka bir ağa
taşındığında LAN IP'si değişti ama tailnet IP'si (`100.109.233.30`)
değişmedi, çünkü o adres Tailscale'in kayıt defterinde tutuluyor. Ağ
katmanı değişse de kimlik katmanı sabit kalıyor — CGNAT'ı delmek için
kurulan şey, taşınmayı da bedavaya çözdü. Bu, ileride uzak ajan (Faz 6)
tasarlanırken hatırlanmalı.

## K-075 — Gözetmen: iyileştirme UZLAŞTIRMAYLA biter, ve testin bunu şansa bırakmaması gerekir

Faz 1 ölçütü #3 için sağlık gözetmeni yazıldı. En pahalı karar, iyileştirme
yolunun nerede BİTECEĞİYDİ.

**İlk akla gelen yanlış:** çökmüş konteyneri `StartReplica` ile geri
başlatmak. `docker ps` düzelir, konteyner durumu ölçen her iddia geçer —
ama site 502 dönmeye devam edebilir. Sebep, Caddy'nin upstream'lerinin
konteyner IP'sinden türemesi.

**Ölçüm, tuzağın neden sinsi olduğunu gösterdi.** Gerçek sunucuda `web`
konteyneri üç kez öldürülüp başlatıldı; adres ÜÇÜNDE DE aynı geldi
(`172.18.0.2`). Yani `Reconcile`'ı atlayan bir gözetmen tek atışlık bir
testi ŞANSA geçer. Adres ancak konteyner silinip yeniden kurulduğunda ya
da araya başka bir konteyner girdiğinde değişir — yani hata, üretimde
aylar sonra ve teşhisi zor bir biçimde ortaya çıkardı.

**İkinci tuzak sıradaydı ve daha ince.** `Reconcile` yapılandırmayı
sıfırdan kuruyor ve rotalanabilir replikası olmayan uygulamayı ATLIYOR.
Konteyner başlatılıp cevap vermeden uzlaştırılsaydı uygulama
yapılandırmadan düşerdi; bir sonraki turda `Check` onu sağlıklı görür,
gözetmen "iyileşti" der ve BİR DAHA uzlaştırmazdı. Sonuç: sağlıklı
GÖRÜNEN ama dışarıdan erişilemeyen bir uygulama — kalıcı olarak.

Bu yüzden sıra: `ağ → konteynerler → KISA KAPI → uzlaştır`, ve
uzlaştırmanın sonucu pozitif ölçütle okunuyor ("uygulamamız `Routed`
içinde mi"), "hata dönmedi" ile değil.

**Kapı neden AYRI:** dağıtım kapısının 90 saniyesi var, iyileştirmenin
toplam 30. Kapı alandan okunsaydı iyileştirme dağıtımın sınırını miras
alır ve ölçütü tek bir yavaş konteynerle kaçırırdı. `awaitReady` artık
kapıyı parametre alıyor.

**`switchTraffic` kasten KULLANILMADI.** K-072 dağıtımla geri almanın
kuyruğu paylaşmasıyla ilgiliydi. Burada üçüncü bir BAŞ ekleniyor, kuyruk
genişletilmiyor: `SetActiveRelease` iyileştirmede no-op olurdu ve
`drainStale` doğrudan tehlikeli — çökmüş bir uygulamayı kurtarırken
başka sürümleri durdurmanın hiçbir gerekçesi yok.

## Testin kendisiyle ilgili iki ders

**1. Kendi kendine referans veren iddia.** `TestHealWaitsForConsecutive-
Failures` eşiği sabitin KENDİSİNDEN okuyordu
(`DefaultOptions.FailuresBeforeHeal - 1` tur koş). Eşik 1'e düşürülünce
testin beklentisi de onunla kaydı ve mutasyon GÖRÜNMEZ kaldı. Mutasyon
sınaması olmasaydı fark edilmezdi.

Kural: bir testin doğruladığı sayı, testin okuduğu sabitten gelmemeli.
Mekanizma ile seçilen değer AYRI testlerde sınanmalı — biri "eşik neyse
ona uyuluyor mu", diğeri "seçilen eşik doğru mu".

**2. Kabul testi doğru sebeple geçmeli.** Canlı ölçümde `docker kill`
sonrası site 6,82 saniyede döndü. Bu tek başına gözetmeni KANITLAMAZ:
konteynerin restart politikası `always` olsaydı siteyi Docker kurtarırdı
ve gözetmen hiçbir şey yapmadan test geçerdi. Ayırt eden ölçüm
`docker inspect .HostConfig.RestartPolicy.Name` → `no` idi; üstüne
gözetmenin kendi günlüğü ve denetim zincirindeki üç geçiş kaydı
(`app.unhealthy` → `app.heal` → `app.healed`) sıralandı.

Bu, K-047 ailesinin canlı ölçüme uzanan biçimi: doğru sonuç, yanlış
sebep de olabilir.

## Sonuç

Ölçüldü: öldürme 20:12:46.4 · sağlıksız 20:12:51.5 · iyileştirme
20:12:53.0 · ilk 200 **+6,82 sn** (ölçüt 30 sn) · `yeniden_kuruldu=false`
yani imaj derlenmedi, duran konteyner uyandırıldı.

Ayrıcalıklı yüzey 2482/2500 — DEĞİŞMEDİ. Yeni exec RPC'si gerekmedi,
çünkü `StartReplica` zaten `ContainerStart`'a bağlıydı. Bu, işi bir şema
değişikliğinden ~300 satırlık bir dilime indiren ölçümdü ve koda
başlamadan ÖNCE yapıldı.

**Faz 1'in dört kabul ölçütü de geçti.**

## K-076 — Kaydedilmiş bir "yapma" kararı, kapsamı daraltarak çözülebilir

`api.proto` DeleteApp'in **kasten yok** olduğunu yazıyordu: silmek geri
alınamaz, şartname §1.3 bu sınıfı TOTP kapısına koyuyor, kapı Faz 2'de ve
şimdi eklemek kapısız bir yıkıcı işlem bırakırdı. Gerekçenin can alıcı
kısmı şuydu: **sonradan kapı eklemek var olan bir RPC'yi KISITLAMAK
demektir ve kısıtlanan arayüzler geriye dönük uyumsuzdur.**

Bu arada koşullar değişti. Legion iptal olunca (K-074) `pfprobe` ve `web`
Hetzner'de kalıcı hâle geldi; rotalı oldukları için her uzlaştırmada geri
geliyorlar ve temizlenemiyorlar.

**Çözüm kararı çiğnemek değil, kapsamı daraltmak oldu.** "Canlı sürümü
olmayan uygulama" silmek yıkıcı değil: trafik yok, rota yok, gözetmen
izlemiyor, ters vekil yapılandırmasında adı bile geçmiyor. Ve asıl önemli
olan, TOTP geldiğinde yapılacak şey kısıtlamak değil **gevşetmek** olacak
— canlı uygulamalara da izin vermek — ki gevşetme geriye dönük UYUMLUDUR.
Yani orijinal gerekçeyi çürütmeden, bugün güvenle yapılabilen yarısı
bugün yapıldı.

Genel ders: kodda kayıtlı bir "yapma" kararıyla karşılaşınca iki kolay
yanlış var — sessizce üzerine gitmek, ya da maddeyi süresiz dondurmak.
Üçüncü yol genellikle var: kararın gerekçesini oku, o gerekçenin
GEÇMEDİĞİ bir alt kümeyi bul.

## K-077 — Alanın yokluğu, Docker'ın VARSAYILANINI kaldırmaz

Sürücüdeki sertleştirmelerin çoğu alanı hiç tanımlamayarak çalışıyor:
`Privileged`, `CapAdd`, `Devices`, `PidMode` için `hostConfig`'te alan
YOK, dolayısıyla kimse yanlışlıkla true atayamıyor. Bu kural iyi
işliyordu ve tam da bu yüzden bir boşluğu görünmez kıldı.

Yetenek düşürme bu kalıba UYMUYOR. Docker'ın varsayılanı boş değil ~14
yetenek: CHOWN, SETUID, SETGID, MKNOD, NET_RAW ve diğerleri. Alanı
yazmamak onları kaldırmaz, **bırakır**. `no-new-privileges` de yetmez —
o yalnızca yetenek KAZANMAYI engeller, verilmiş olanları düşürmez.

Ölçüldü: canlı sunucuda konteyner içinde root koşan bir uygulamanın
`CapEff` değeri `0xa80425fb`'ydi, yani tam varsayılan set. NET_RAW tek
başına ortak köprüde ARP/DNS sahteciliğine yeter.

75 kararın hiçbirinde bu konu geçmiyordu; yani bilinçli bir tercih değil,
gözden kaçmış bir eksikti. Ayırt edici işaret şuydu: bu depoda her
kasıtlı yokluk yorumla gerekçelendirilmiş. `CapDrop` ve `PidsLimit` ise
yorumsuz yoktu.

**Karar:** `CapDrop: ["ALL"]` ve `PidsLimit` POZİTİF olarak yazılıyor.
`exec.proto`'ya alan EKLENMİYOR — çağıran hâlâ yetenek isteyemez; bu
sabit bir executor politikası, yani tasarım kuralı 3 korunuyor.

Genel ders: "yokluk güvenliktir" kuralı yalnızca güvenli varsayılanın
SIFIR olduğu alanlarda geçerli. Varsayılanı sıfır olmayan her ayar için
koruma pozitif ifade edilmek zorunda.

### Ölçmeden düşürülmedi

`CapDrop: ALL` körlemesine yazmak canlı siteyi düşürebilirdi: 1024
altına bağlanan bir imaj `CAP_NET_BIND_SERVICE` ister. Önce ölçüldü —
üç canlı uygulamanın dinlediği portlar 8080, 8080 ve 8000, yani hiçbiri
ayrıcalıklı porta bağlanmıyor. Sonra üç imaj da `--cap-drop=ALL
--pids-limit=512` ile ayrı ayrı koşturuldu ve üçü de HTTP 200 döndü;
kontrol grubu (düşürmesiz) da 200 döndü, yani ölçüm ayırt ediciydi.

Canlı doğrulamada `CapEff` **bilgi taşımadı**: yeni ve eski konteynerde
de 0'dı, çünkü ikisi de uid 101 koşuyor. Ayırt eden `CapBnd` oldu —
`0x80425fb`'den `0`'a indi. PID sınırı ise sayıyla değil davranışla
sınandı: 700 süreç denendi, tam 512'de durdu, konteyner ayakta kaldı.

## K-078 — Geri alınamaz göç, yedeksiz uygulanamaz

Göçler ileri yönlü: hiçbirinin `.down.sql` karşılığı yok ve `0005`
yıkıcı (`DROP TABLE deployments`). Üstelik `Open()` içinde, daemon
hiçbir şey servis etmeden önce OTOMATİK uygulanıyorlar. Başarısız bir
göç, systemd `Restart=on-failure` ile mutasyona uğramış bir veritabanının
üzerinde çökme döngüsü kurar — ve yedek olmadan bu durumdan çıkış YOK.

**Karar:** ilk bekleyen göçten önce `VACUUM INTO` ile anlık görüntü.
Yedek alınamıyorsa göç DE yapılmıyor; daemon açılmayı reddediyor.
Operatör dolu diski düzeltebilir, kötü bir göçü geri alamaz. Bu, yüzey
denetçisinin "ölçemiyorsak onaylamayız" kuralının aynısı.

Üç ayrıntı taşıyıcı:

- **`cp` değil `VACUUM INTO`.** WAL modunda ham kopya `-wal` dosyasını
  geride bırakır ve son yazmaları içermeyebilir; sessizce eski bir
  duruma dönülür.
- **Taze veritabanında atlanıyor.** Hiç göç uygulanmamışsa kaybedilecek
  şey yok.
- **Tekrar denemede ÜZERİNE YAZILMIYOR.** systemd açılışı tekrar tekrar
  deniyor; her deneme ezseydi ilk denemeden önceki temiz hâl kaybolurdu.

### Test önce ZAYIFTI ve mutasyon bunu gösterdi

"Üzerine yazmıyor" iddiasının ilk testi dosya SAYISINA ve boyutuna
bakıyordu. Mutasyon (var-mı kontrolünü kaldır) **yeşil geçti**: üzerine
yazan bir uygulamada da tek, dolu bir dosya kalıyor, yani iddia ayırt
edici değildi. Test denemeler ARASINA bir işaretçi satır yazacak şekilde
güçlendirildi — yedek ezilirse o satır içine girer — ve mutasyon
yakalandı.

K-071 yeşil bir mutasyonun MUTASYONUN zayıflığı olabileceğini
söylüyordu. Bu vaka ters yönü gösteriyor: bazen gerçekten TEST zayıftır.
Ayrım ancak mutasyonun neyi değiştirdiğine bakarak yapılabilir.

### Varsayılan bir kısıt, ölçünce yok çıktı

Yedek yolu önce SQL dizgesine gömülüyordu, tek tırnaklar elle
kaçırılarak; gosec G202 verdi ve neredeyse `//nolint` eklenecekti.
Küçük bir programla sınandı: **`VACUUM INTO ?` bağlı parametreyi kabul
ediyor.** Yani kısıt hiç yoktu. Bir uyarıyı bastırmadan önce, uyarının
işaret ettiği kısıtın gerçek olup olmadığı ölçülmeli.

## K-079 — Kod, sahip olmadığı bir özelliği anlatmaya devam edebilir

`VerifyAuditChain`'in yorumu "Faz 0'da durum değiştiren executor çağrısı
olmadığı için executor zinciri boş" diyordu. Faz 1 inince
`ContainerCreate` ve `ImageBuild` yazmaya başladı; **yorum
güncellenmedi.** Aynı iddia `exec.proto`'nun tasarım kurallarında da
vardı: "panelyd ele geçirilip kayıt düşürse bile VerifyAuditChain iki
zincir arasındaki farkı yakalar."

Bu bugün YANLIŞ. Çapraz karşılaştırma kodu hiçbir yerde yok; kendi
kayıtlarını düşüren ele geçirilmiş bir panelyd için her iki zincir de
VALID döner. Yani tehdit modelinin merkezindeki senaryo için belge bir
koruma vaat ediyordu, kod vermiyordu.

**Karar:** iddia geri çekildi, eksiklik açıkça yazıldı. Gerçek çapraz
doğrulama executor'ın yanıtında kayıt hash'ini döndürmesini gerektiriyor
— yani `exec.proto` değişikliği ve ayrıcalıklı yüzey bütçesinden yer.
Bütçede 13 satır kaldığı için o iş kendi dilimine ait.

Bu K-068'in ("iddiayı kullanıcının OKUDUĞU katmana yaz") ve "dokümanın
vaadi test altında değilse yalana dönüşür" dersinin üçüncü tekrarı.
Ortak mekanizma şu: **koşullu bir yorum, koşulu değişince kendiliğinden
yanlışa döner ve hiçbir test bunu yakalamaz.** "Faz 0'da … olmadığı
için" gibi tarihe bağlı gerekçeler yazarken, o tarihin geçeceği
varsayılmalı.

Aynı turda `SECURITY.md` de düzeltildi: yetki sınırını `panely` grubu
diye tanımlıyordu, oysa `api.sock`'u **`panely-client`** koruyor
(`panely` grubu `exec.sock`'u koruyor). Yetki sınırını TANIMLAYAN
belgede yanlış grup adı.

## K-080 — Ölçek küçültme: kayıt doğruydu, gerçeklik değişmiyordu

`panely app update -replicas 1` "başarılı" diyor, `apps` satırını da doğru
yazıyordu — ama hostta hiçbir şey olmuyordu. İki bağımsız eksik vardı ve
ikisi birbirini gizliyordu:

- `ensureReplicas` yalnızca `[0, Replicas)` aralığını **kuruyor**;
  indeksi bu aralığın dışında kalan konteynerleri hiç durdurmuyordu.
- `upstreamsFor` indekse hiç bakmadan, aktif sürümün **bütün** ayakta
  replikalarını rotalıyordu.

Sonuç: 3'ten 1'e inen bir uygulamada üç konteyner de trafik almaya devam
ediyordu. Gözetmen de yakalamıyordu, çünkü `ready >= app.Replicas`
karşılaştırması 3 ≥ 1 ile **sağlıklı** diyor.

**Karar:** iki taraf da düzeltildi ve sıra taşıyıcı — **önce rota daralır,
sonra konteyner durur.** Tersi, hâlâ istek alan bir konteyneri koparırdı.
Rota daralması `app update` anında uzlaştırmayla oluyor; durdurma bir
sonraki dağıtımda/iyileştirmede. Yani durdurulan konteyner, o ana kadar
zaten trafik almıyor.

Fazlalıklar siliniyor değil **durduruluyor** — K-061'in aynı gerekçesi:
tekrar büyütmek gerekirse imajdan kurmaya gerek kalmasın.

### `Replicas` sıfırsa sessiz kalınmıyor

Şema `CHECK (replicas BETWEEN 1 AND 64)` ile sıfırı yasaklıyor, yani
sıfır ancak eksik doldurulmuş bir `Deployment`'tan gelebilir. İndeks
filtresi böyle bir değerle **her** replikayı eler ve uygulama tamamen
rotasız kalırdı. Asıl tehlike sessizlik: mesaj "ayakta replikası yok"
olsaydı operatör konteynerlerin peşine düşerdi — oysa konteynerler gayet
ayakta. Artık sebep açıkça yazılıyor.

Bunu testler buldu: yeni alan eklenince mevcut sahtelerde sıfır kaldı ve
üç test birden düştü. Kusur sahtelerdeydi ama **düşüş gerçek bir
başarısızlık kipini gösterdi.**

### Yeşil kalan mutasyonun ÜÇÜNCÜ sebebi

K-071 yeşil bir mutasyonun iki sebebi olabileceğini söylüyordu: test
zayıftır ya da mutasyon zayıftır. Bu dilimde üçüncüsü çıktı ve aynı
mutasyon iki tur boyunca yeşil kaldı, her turda sebep farklıydı:

1. **Test zayıftı.** Sahte dünyada tek sürüm vardı, dolayısıyla sürüm
   filtresi hiçbir şey yapmıyordu. Mavi-yeşil senaryosu eklendi.
2. **Kod tutarsızdı.** Test eklendi, mutasyon hâlâ yeşil geçti. Sebep:
   durdurma çağrısı gezilen replikanın `rep.ReleaseID`'sini değil aktif
   `rel.ID`'yi geçiyordu. Filtre varken ikisi eşit olduğu için sonuç
   doğruydu — ama gezilen öğeyle çağrılan öğe farklı kaynaklardan
   geldiği için aradaki bağ yalnızca filtreye dayanıyordu. `rep.ReleaseID`
   geçilince mutasyon görünür oldu.
3. **Mutasyon zayıftı.** Üçüncü bir mutasyon (ikisini birleştiren)
   yine yeşil kaldı ve bu kez gerçekten geçerliydi: çağrı aktif sürümü
   adlandırdığı için seçici eski sürümün konteynerini bulamıyor, yani
   hiçbir şey durdurulmuyor. Korunan özellik ihlal edilmiyor. Mutasyon
   elendi, gerekçesi betiğe yazıldı.

### Çağrı kaydına bakmak ölçüm değildir

Ara aşamada test `stoppedReplicas` listesindeki **sürüm adını** kontrol
ediyordu. Yetmedi: kaydın kendisi yanlış sürüm adı taşıyabiliyordu, yani
"relNew durduruldu" yazarken fiilen eski sürümün konteyneri inmiş
olabilirdi. İddia dünyanın **durumuna** çevrildi — eski sürümün
replikaları hâlâ RUNNING mı. Kaydın kendisi yanılabiliyorsa kayda bakmak
ölçüm değildir.

Ayrıca her iki yönde de kontrol grubu eklendi: `TestScaleUpRoutesEveryReplica`
ve `TestHealKeepsEveryReplicaWhenCountMatches`. Bunlar olmasaydı "her
zaman ele" ya da "her zaman durdur" diyen bir uygulama da testleri
geçerdi.

## Sıra taşıyıcıdır: kayıt, konteynerlere ulaşmanın TEK yolu

Konteyner adları `app_id`/`release_id`'den türüyor. Kayıtlar önce silinse
ve konteyner kaldırma yarıda kalsa, geriye **kimsenin adını bilemediği**
çalışan konteynerler kalırdı — ne panely görür, ne bir sonraki deneme
bulur. Bu yüzden: konteynerler önce, kayıtlar en sonda; ve kaldırma
başarısız olursa kayıtlara DOKUNULMUYOR, komut yeniden çalıştırılabiliyor.

Canlılık kontrolü hem API'de hem deponun işleminde. İkincisi olmadan,
kontrol ile silme arasında tamamlanan bir dağıtım canlı bir uygulamanın
kaydını sildirebilirdi.

Silme sırası `deployments → releases → apps` ve bunu veritabanı zorluyor
(ters sıra `FOREIGN KEY constraint failed`). **İlk ölçüm bunu
göstermemişti:** sıfır dağıtımı olan bir uygulama (`blog`) seçilmişti,
yani bileşik yabancı anahtar hiç çalışmadı. K-047 ailesi — test, hatanın
YAŞADIĞI yolu çalıştırmalı.

## Dokümanın vaadi test altında değilse yalana dönüşür

Proto `FAILED_PRECONDITION` döneceğini yazıyordu; ilk canlı ölçümde
`InvalidArgument` döndü, çünkü `s.denied()` her zaman onu döndürüyor.
Birim testleri hatayı yakalamıştı ama **kodu** iddia etmiyorlardı, o
yüzden uyumsuzluk sessiz kaldı ve ancak gerçek sunucuda göründü.
Test artık `status.Code(err)`'i de iddia ediyor.

## Bilinçli borç

`panely-<app>` ağı ve `panely/<app>:<sha>` imajları BIRAKILIYOR:
`NetworkRemove`/`ImageRemove` RPC'leri yok ve ayrıcalıklı bütçe
2482/2500. Ağ zararsız — `NetworkEnsure` aynı adla yeniden kullanıyor ve
havuz `/12` içinde ~4096 derin. İmajlar disk yiyor; saklama politikasıyla
birlikte ele alınacak.

**Canlıda ölçüldü:** `portfolio` (canlı) reddedildi, `blog`/`hello`/
`ozelrepo` silindi (2+3+3 sürüm), site 34 yoklamanın hepsinde 200,
denetim zinciri geçerli, artık satır sıfır.

## K-081 — Birleştirme bir tercih değil, şemanın temsil edebildiği tek davranış

`app update -env` için üç semantik düşünüldü: tamamen değiştir,
birleştir, birleştir + ayrı silme. Tartışma kullanılabilirlik üzerinden
başladı ve orada kalsaydı yanlış cevaba varacaktı — çünkü asıl kısıt
kullanıcı deneyimi değil, **tel formatı**.

proto3'te `map<string,string>` alanlarının **presence'ı yoktur** ve
`optional map` geçersizdir. Yani "hiç verilmedi" ile "boş harita
gönderildi" tel üzerinde **aynı şeydir**. Diğer dört güncellenebilir
alan (`domain`, `git_branch`, `health_path`, `replicas`) `optional`
sayesinde bu ayrımı taşıyor; harita taşıyamaz.

Sonuç: "tamamen değiştir" semantiği dürüstçe uygulanamaz. Uygulansaydı
`-env` yazmayan **her** güncelleme — yalnızca alan adını değiştiren bir
komut bile — uygulamanın bütün ortam değişkenlerini silerdi. Go
tarafına işaretçi eklemek bunu çözmez; tel üzerinde var olmayan bir
bilgiyi Go'da varmış gibi göstermek yalnızca hatayı gizlerdi.

Silme bu yüzden **ayrı bir alan** (`env_remove`): birleştirme tek başına
bir anahtarı kaldıramaz ve boş dizeye ayarlamak silmek değildir —
konteyner değişkeni "tanımlı ama boş" görür, "tanımlı mı" diye bakan
uygulama yanlış cevap alır.

Aynı anahtarın hem `env`'de hem `env_remove`'da geçmesi ÇELİŞKİ sayılıp
reddediliyor. Sessizce bir tarafı seçmek, kullanıcının iki niyetinden
hangisinin uygulandığını belirsiz bırakır ve belirsizlik er geç
haritanın gezilme sırasına bağlı bir hataya dönüşür.

**Taşınabilir ders:** bir API kararını kullanılabilirlik gerekçesiyle
savunmadan önce, şemanın o davranışı temsil edip edemediğine bakın.
Temsil edilemeyen bir semantik, ne kadar arzu edilirse edilsin,
uygulandığında sessiz veri kaybına dönüşür.

## K-082 — Env zinciri sekiz katman ve hiçbiri derleme hatası vermez

Ortam değişkeni desteği CLI → proto → doğrulama → depo → göç → rollout →
execclient → sürücü hattından geçiyor. Bu hattın herhangi bir halkasında
alanı düşürmek **derlemeyi kırmaz**: Go'da atlanan alan sıfır değerine
düşer.

En kritik halka `rollout.createReplica`. Ayrıcalıklı katman (executor
doğrulaması, `RedactEnv`, `dockerdrv.envList`) env'i aylardır destekliyor
ve çalışıyordu; eksik olan tek şey `CreateReplicaOptions`'a `Env`
yazılmasıydı. O satır olmadan şema, göç, doğrulama, denetim ve CLI'ın
hepsi doğru çalışır, `app show` değişkeni gösterir, komut "başarılı" der
— ve konteyner ortamsız doğar.

Bu, K-080'in birebir aynısı: **mekanizma bağlandı, son halkası
unutuldu.** Fark şu ki bu kez hata yazılmadan önce arandı ve önce testi
yazıldı.

`createReplica`'ya, alan listesinin elle tutulduğunu ve derleyicinin
yardım etmeyeceğini söyleyen bir uyarı eklendi. `Volumes` hâlâ aynı
durumda ve aynı tuzağı bekliyor.

## K-083 — Yeşil mutasyonun üçüncü sebebi bu kez de çıktı, ama farklı yerden

`mutate-env.sh` ilk koşusunda üç mutasyon yeşil kaldı ve **üçünün sebebi
farklıydı** — K-080'in listelediği üç kategorinin canlı bir örneği:

1. **Zayıf test (yanlış sebeple geçiyordu).** Bayt bütçesi testi
   `string(rune('A'+i))` ile anahtar üretiyordu; i>25 için `[`, `\`, `]`
   çıkıyor ve `validateEnv` bunları **anahtar deseninden** reddediyordu.
   Test "hata döndü" diye geçiyordu ama hata bütçe kontrolünden
   gelmiyordu — bütçeyi kaldıran mutasyon bu yüzden yeşil kaldı.
   Düzeltme: geçerli anahtarlar **ve** hatanın sebebini de iddia etmek.

2. **Eksik test.** `validateEnv`'i `validateAppSpec`'ten çıkaran mutasyon
   hiçbir testi kırmadı: bütün doğrulama testleri fonksiyonu **doğrudan**
   çağırıyordu. Fonksiyon kusursuz çalışıyor, hiç kullanılmıyordu.
   Düzeltme: geçersiz env ile `CreateApp`/`UpdateApp` RPC'lerini çağıran
   testler.

3. **Zayıf mutasyon.** `set["env"]` kontrolünü kaldıran mutasyon korunan
   özelliği **kırmıyor**: `-env` verilmediğinde `stringMapFlag` boş ama
   nil olmayan bir harita döndürüyor (ölçüldü), ve hem `isEmptyUpdate`
   hem `ChangesEnv` `len()`'e bakıyor — boş harita atamak ile hiç
   atamamak aynı sonucu veriyor. Mutasyon betikten ÇIKARILDI, kontrol
   kodda BIRAKILDI: niyeti belgeliyor ve yardımcı bir gün nil dönerse
   koruyacak.

Üçüncüsü `mutate-scaledown.sh`'deki elenen mutasyonla aynı sınıf. Kural
pekişti: **yeşil bir mutasyon bir bulgudur, bir arıza değil** — ama hangi
kategoriye düştüğü ölçülmeden karara bağlanamaz. "Testi güçlendir"
refleksi üçte birinde yanlış cevaptır.

## K-084 — Uyarı mesajını doğru alana yazmak

`app update -env` "kaydedildi ama konteynerler eski ortamla koşuyor"
uyarısı üretmek zorunda (Docker çalışan konteynerin ortamını
değiştiremez). Hazır bir alan vardı: `UpdateAppResponse.proxy_detail`.

Kullanılmadı. O alanın adı "ters vekil" diyor ve içine env mesajı
koymak alanın **adını yalancı** yapardı — K-079'un kaydettiği hatanın
alan adıyla yapılan hâli. Ayrıca iki uyarı **aynı anda** doğru olabilir:
hem alan adı hem env değişmiş bir güncelleme iki farklı şey söylemek
zorunda ve tek alan bunlardan birini yutardı.

`env_detail` açıldı. Maliyeti üretilmiş kodda birkaç satır; kazancı,
istemcinin hangi uyarının hangi mekanizmaya ait olduğunu bilmesi.

**Ayrıca ölçüldü:** `internal/pb/*` ayrıcalıklı yüzey bütçesinden
**hariç** (üretilmiş kod, ve betik dosyaların gerçekten üretilmiş
olduğunu doğruluyor). Yani `api.proto`'ya alan eklemenin bütçe maliyeti
**sıfır**. Hafızadaki "sıradaki iş bütçeye çarpacak" notu executor işleri
için doğru, bu iş için yanlıştı. Bütçe iş öncesi ve sonrası 2493'te
kaldı.

## K-085 — "Aynı son-mil şekli" varsayımı, ölçünce yarısı doğru çıktı

Kalıcı disk işi, env'in ikizi sanılıyordu ve hafızaya öyle yazılmıştı:
ayrıcalıklı katman hazır, eksik olan yalnızca sütun + şema alanı + CLI +
`createReplica`'da doldurma. İlk üçü doğruydu. Dördüncüsü eksikti.

**Ölçüm.** Canlı sunucuda:

```
docker run -v <kök>/_probe/data:/veri --user 101:101 alpine \
  sh -c 'echo x > /veri/test.txt'
→ sh: can't create /veri/test.txt: Permission denied
```

Sebep: hiçbir yerde `MkdirAll` yoktu. Bind kaynağı eksikse Docker onu
kendisi yaratıyor ve sahibi **root:root** oluyor. Panely'nin canlı
imajlarının ikisi `USER 101` ile koşuyor (`Config.User` okundu: `101`,
`101`, `""`).

Sonuç, env'de **karşılığı olmayan** bir arıza sınıfı: hacim bağlanır,
konteyner başlar, hiçbir hata çıkmaz, ve uygulama kendi kalıcı diskine
yazamaz. Belirti dağıtımda değil, uygulamanın kendi günlüğünde ve saatler
sonra görünür.

**Çözüm.** Executor, imajın `Config.User`'ını okuyup dizini o kimliğe
`chown` ediyor. Sayısal olmayan `USER` (ör. `nginx`) **açıkça
reddediliyor**: adı kimliğe çevirmek imajın `/etc/passwd`'ını okumayı,
yani ayrıcalıklı sürece bir dosya sistemi ayrıştırıcısı eklemeyi
gerektirirdi. Sessizce 0'a düşmek en kötü seçenekti — yazamayan bir
hacmi "hazır" ilan ederdi.

**Elenen seçenek: `0777`.** Ucuzdu ve bütçeye sığardı. Reddedildi çünkü
hacim kökü `drwxr-x--x`; `--x` sayesinde hosttaki herhangi bir kullanıcı
bilinen yola geçip yazabilirdi. "Tek yöneticili bir VPS'te önemsiz"
argümanı bu projenin kabul etmediği argümandır.

**Taşınabilir ders:** "X'in aynısı" demek bir hipotezdir, plan değil.
Hipotezi işe başlamadan önce ölçün — burada ölçüm 20 dakika sürdü ve
tasarımın yarısını değiştirdi.

## K-086 — Güvenlik denetçisinin şema taramasında kör nokta vardı

Hacim mesajını `common.proto`'ya taşımak değerlendirilirken ölçüldü:

```
exec.proto'ya   host_path eklendi → ✗ yakalandı
common.proto'ya host_path eklendi → "değişmezler korunuyor" ✓
```

`common.proto`'yu `exec.proto` içe aktarıyor ve `ResourceLimits`
doğrudan `ContainerCreateRequest`'in içinde duruyor. Yani yasak bir alan
oraya yazılsaydı **ayrıcalıklı şemanın parçası olurdu** ve kontrol yeşil
verirdi.

Bu, aynı betiğin **kod tarafında** çözdüğü hatanın şema tarafındaki
ikizi: K-034'te sabit yol listesi yüzünden yeni bir paket sessizce kapsam
dışı kalıyordu. Orada çözüm kapsamı derleyiciden türetmekti; burada
çözüm `import` satırlarından türetmek.

Düzeltmenin **kendi kanıtı** var: `check-exec-surface-test.sh`'e iki vaka
eklendi — içe aktarılan şemadaki yasak alan yakalanıyor, **ve** temiz bir
içe aktarma yanlış alarm üretmiyor. İkincisi olmadan "her içe aktarmada
patla" diyen bir uygulama da geçerdi.

`VolumeMount` sonuçta `exec.proto`'da BIRAKILDI ve api.proto'ya ayrı bir
`AppVolume` kondu. Tarama artık içe aktarmaları kapsasa da, güvenlik
sınırını tarif eden mesajların tek bir denetlenebilir dosyada durması
tercih edildi — ve kullanıcıya görünen sözleşmenin değişmesi ayrıcalıklı
beyaz listeyi kendiliğinden değiştiremiyor.

## K-087 — Ders taşındı: 26 mutasyonun 26'sı İLK koşuda yakalandı

`mutate-env.sh` ilk koşusunda üç mutasyon yeşil kalmıştı (K-083).
`mutate-volumes.sh` ilk koşusunda **hiçbiri** kalmadı. Fark tesadüf
değil; env'de öğrenilen üç şey baştan uygulandı:

1. **Depo testleri diskten okuyor.** env'de `UpdateApp`'in DÖNÜŞÜNE bakan
   testler, SQL hiç yazmasa bile geçiyordu. Hacim testleri en baştan
   `GetApp` ile yeniden okuyor.
2. **Doğrulama RPC seviyesinde de sınanıyor.** env'de bütün doğrulama
   testleri fonksiyonu doğrudan çağırıyordu, yani `validateAppSpec`'ten
   çağrıyı silmek hiçbir testi kırmıyordu.
3. **Her iddianın bir kontrol grubu var.** "Hacimsiz uygulamaya hacim
   uydurulmuyor", "temiz içe aktarma yanlış alarm üretmiyor",
   "`:rw` salt-okunur yapmıyor" — hepsi "her zaman evet de" diyen bir
   uygulamayı eleyen testler.

Ders, mutasyon sayısını artırmak değil: **bir kez ölçülen zayıflık
sınıfını bir sonraki işe ŞABLON olarak taşımak.**

## K-088 — Silme komutu NE SİLMEDİĞİNİ de söylemeli

`app delete` hacim verisine dokunmuyor. Bu bilinçli: geri alınamaz bir
veri kaybının tek bir komutun yan etkisi olarak gerçekleşmesi bu projede
kabul edilmiyor (aynı çizgi `-volume-rm`'de de geçerli — o da yalnızca
bağlamayı kaldırıyor).

Ama **sessiz kalmak** da kabul edilemezdi. Kullanıcı uygulamayı
sildiğinde diskin de gittiğini varsayar; varsaymadığında bile nerede
durduğunu bilmez. Aylarca farkına varmadan yer tüketir.

Yanıt artık `volumes_kept` taşıyor ve CLI onu açıkça basıyor:
"N hacmin VERİSİ DİSKTE DURUYOR (silinmedi)".

Mesaj **yolu adlandırmıyor** ve bu da kasıtlı: hacim kökü executor'ın
yapılandırması, panelyd onu bilmiyor. Bilmediği bir yolu yazmak
doğrulanmamış bir iddia olurdu — K-079'un sınıfı.

**Taşınabilir ders:** "ne yapıldı" kadar "ne yapılmadı" da çıktının
parçasıdır. Bir komutun dokunmadığı şey, kullanıcının dokunulduğunu
sandığı şeyse, susmak yanlış bilgi vermekle aynı kapıya çıkar.

## K-089 — "Budama = imaj silme" varsayımı ölçünce çöktü: sorun günlüktü

Plan aylardır şunu diyordu: imajlar hiç silinmiyor, o yüzden bir
`ImageRemove` RPC'si gerekiyor, o yüzden ayrıcalıklı yüzey bütçesinde
kavga çıkacak. Kod yazmadan önce canlı sunucu ölçüldü ve plan çöktü.

### Ölçüm

```
/var/lib/docker                    408 MB
├─ containers/  (JSON günlükler)   289 MB   ← %71
└─ rootfs/      (BÜTÜN imajlar)    119 MB
```

En büyük dört günlük:

```
panely_portfolio_r5_0   ÇALIŞIYOR   110 MB
panely_web_r1_0         ÇALIŞIYOR    69 MB
panely_pfprobe_r3_0     durmuş       68 MB
panely_pfprobe_r1_0     durmuş       33 MB
```

Yani **tek bir konteynerin günlüğü, sunucudaki bütün imajların toplamından
büyüktü.** Planlanan iş, son 14 bütçe satırını üç sorunun EN KÜÇÜĞÜNE
harcayacaktı.

### ⚠ `docker images` ve `docker system df` bu ölçüm için KULLANILAMAZ

İlk bakış yanıltıcıydı: `docker images` 18 adet `<none>` imaj gösteriyordu
ve neredeyse hepsi **520 MB** yazıyordu. Toplasan ~9 GB eder — oysa disk
toplam 4.5 GB doluydu.

Sebep: paylaşılan katmanlar her imaja AYRI AYRI sayılıyor. Aynı çelişki
`docker system df` çıktısında da göründü:

```
Images   27   5   1.112GB   RECLAIMABLE: -8.495e+08B (-76%)
```

**Negatif geri kazanılabilir alan.** Bir araç imkânsız bir sayı basıyorsa
ölçtüğü şey aradığın şey değildir. Gerçeği `du -sh` söyledi.

Bu K-084'ün akrabası ama yeni bir sınıf: orada yanlış ALANA yazılmıştı,
burada **doğru araç yanlış soruyu cevaplıyordu.**

### Çözüm ve neden bu şekli aldı

`HostConfig.LogConfig` sürücüde **sabit politika** olarak yazıldı:
`json-file` + `max-size=10m` + `max-file=3` → konteyner başına ≤ 30 MiB.

Şekil CapDrop/PidsLimit ile birebir aynı ve sebebi de aynı: Docker'ın
varsayılan günlük tavanı YOKTUR, yani alanı yazmamak "sınırsız"ı seçmek
demek. Sürücüdeki öteki sertleştirmeler alanı HİÇ TANIMLAMAYARAK çalışıyor
(`Privileged`, `CapAdd`, `Devices`); bu öyle çalışamaz, pozitif ifade şart.

`Type` alanı da yük taşıyor ve yazılmaması iki şeyi birden kırardı:
host'un daemon'ı varsayılan sürücüyü değiştirmişse `max-size` geçersiz bir
seçenek olur **ve** `ContainerLogs` yalnızca json-file okuyabildiği için
`panely logs` çalışmaz.

Proto'ya alan EKLENMEDİ: çağıran günlük politikası isteyemez, executor
kendi kararını uygular. Tasarım kuralı 3 korunuyor.

### Rotasyon `panely logs`'u kırıyor mu? — ölçüldü, hayır

```
max-size=1m max-file=3 → docker logs 15.022 satır   diskte 2.3 MB (.log .log.1 .log.2)
KONTROL rotasyonsuz    → docker logs 60.000 satır   diskte 9.0 MB
```

Okuma döndürülmüş dosyaları da kapsıyor. Takas açık ve kabul edildi:
tavanın ötesindeki ESKİ geçmiş silinir. Kontrol grubu olmadan "15.022
satır okundu" tek başına hiçbir şey söylemezdi — 60.000'e karşı okunduğu
için anlamlı.

### İki testi BİRBİRİNE BAĞLAMAK gerekti

İlk hâlde mekanizma testi gövdeyi okuyordu ("max-size boş değil"), sınır
testi sabitleri okuyordu ("10m makul"). İkisi bağlı değildi ve arada
**delik** vardı: gövdeye elle `"max-size": "0"` yazan bir değişiklik
birinci iddiayı geçer (alan dolu), sabitlere dokunmadığı için ikinciyi de
geçer, ve tavan sessizce kalkar.

Mekanizma testi artık gövdeyi SABİTLE karşılaştırıyor. Zincir kapandı:
gövde sabiti taşıyor + sabit sınırlı ⇒ gövde sınırlı. Mutasyon betiğinde
üç mutasyon tam olarak bu deliği ölçüyor ve delik kapanmadan önce
yeşil geçiyorlardı.

**Taşınabilir ders:** iki test aynı değişmezin iki yarısını koruyorsa,
aralarındaki bağ da sınanmalı. Ayrı ayrı yeşil olmaları birleşimlerinin
yeşil olduğunu göstermez.

### ⚠ Sekiz mutasyon betiği CI'da HİÇ KOŞMUYORDU

Bu iş sırasında bulundu: `scripts/mutate-*.sh` ailesi aylardır vardı ama
`ci.yml` hiçbirini çağırmıyordu. Yalnızca elle çalıştırılan bir betik
guard değildir — koruduğu değişmez sessizce kırılır, rozet yeşil kalır.
K-071'in ("yeşil kalan mutasyon") bir üst katmanı: mutasyon hiç
koşmuyorsa yeşil kalıp kalmadığı da bilinmiyor.

Yeni `mutation` işi betikleri **glob'la** buluyor, liste tutmuyor — liste
tutmak yeni betiğin unutulmasına davetiye olurdu, yani bu işi doğuran
hatanın aynısı. İki kendi-kendini-sınama var: boş glob reddediliyor
(9 betikten az bulunursa hata) ve koşu sonunda `git diff --exit-code` ile
ağacın temiz döndüğü kanıtlanıyor (bozuk bir `restore`, SONRAKİ betiğin
ölçümünü sessizce geçersiz kılardı).

### ⚠ Mevcut konteynerler ETKİLENMEZ

Günlük yapılandırması oluşturma anında sabitlenir; env ve hacimlerle aynı
sınıf. Canlı `portfolio_r5_0` ve `web_r1_0` **yeniden dağıtılana kadar**
110 MB ve 69 MB'lık günlüklerini büyütmeye devam eder. Politikayı indirip
"disk sınırlandı" demek K-088'in reddettiği şekil olurdu; bu yüzden iş,
üç canlı uygulamanın yeniden dağıtımı ve `du` ile ölçülen düşüşle
bitiyor.

### Bütçe: 2486 → 2498, ve küçültme kuyusu kurudu

Rotasyon 12 ayrıcalıklı satıra mal oldu. **2 satır kaldı.**

16 Eylül'de `deadcode` 73 satır açmıştı; bu sefer aynı komut üç aday
buldu ve üçü de işe yaramaz: `audit.Verifier.Count` (1 satır ama api ve
store KULLANIYOR, taşınamaz — `audit.Verifier`'ın metodu),
`audit.VerifyAll` (~8 satır, audit'in kendi meşru API'si) ve
`sdnotify.Status` (1 satır, panelyd kullanıyor).

**Bir sonraki executor işi — imaj budama ya da denetim zinciri çapraz
doğrulaması — gerçek bir limit-yükseltme kararıyla karşılaşacak.**
Betiğin kendi kuralı gereği o karar, küçültmenin neden tercih
edilmediğinin yazılı gerekçesini ister; bu paragraf o gerekçenin
ölçülmüş hâlidir.

## K-090 — Budamanın saklama kümesini CANLI SUNUCU doğruladı

`panely prune` saklama politikası: **aktif sürüm + bir önceki AKTİF
sürüm**. Gerisi durdurulup siliniyor.

İki tane, daha azı değil. K-061 duran konteynerleri kasten biriktiriyor
çünkü geri alma duran bir konteyneri **başlatıyor** (saniyeler), imajdan
kurmuyor (dakikalar). Geri alma hedefini budamak, K-061'in bütün
gerekçesini çökertirdi — üstelik "budama başarılı" diyerek.

### Gerçek veri, tasarım kararını kanıtladı

"Bir önceki" dağıtım GEÇMİŞİNDEN okunuyor, `releases.seq`'ten değil. Bu
ayrım teorik değildi; canlı sunucuda `portfolio`'nun geçmişi şöyleydi:

```
seq  sürüm  aktif oldu            bıraktı
 3   r3     2026-08-14 21:29      2026-09-01 18:03
 4   r4     2026-09-01 18:03      2026-09-01 18:04
 5   r3     2026-09-01 18:04      2026-09-01 18:05
 6   r4     2026-09-01 18:05      2026-09-01 18:07
 7   r3     2026-09-01 18:07      2026-09-01 18:08
 8   r5     2026-09-01 18:08      HÂLÂ AKTİF
```

1 Eylül'deki geri alma denemeleri sürüm sırasını aktivasyon sırasından
ayırmış. En son BIRAKAN sürüm **r3**, r4 değil — yani `panely rollback`
r3'e gider.

`releases.seq` kullanan bir uygulama r4'ü korur ve **r3'ü silerdi**:
geri alma hedefinin ta kendisini. Budama "başarılı" der, hata aylar
sonra rollback yavaşlayınca görülürdü.

Göç 0005 tam olarak bu ayrım için yazılmıştı; burada ilk kez bir
TÜKETİCİSİ oldu ve gerekçesi gerçek veriyle doğrulandı.

### Ayrıcalıklı yüzey: 0 satır

`ContainerRemove` RPC'si zaten vardı (`app delete` kullanıyor). Eksik
olan tek şey POLİTİKAYDI ve politika `internal/api`'de — executor'ın
içe aktarma grafiğinin dışında. Yüzey 2498'de kıpırdamadı.

**Taşınabilir ders:** yeni bir yetenek her zaman yeni bir RPC istemez.
Önce var olan yüzeyin ne kadarının kullanılmadığına bakmak, bütçe
tartışmasını tamamen gereksiz kılabiliyor.

### `app_id` ZORUNLU — boş "hepsi" DEĞİL

proto3'te `string` presence taşımaz: "gönderilmedi" ile "boş
gönderildi" telde ayırt edilemez (K-081'in aynı kökü). Boşu "bütün
uygulamalar" saysaydık, alanı doldurmayı unutan bir çağıran **yıkıcı**
bir işlemi her uygulamaya uygulatırdı — hata almadan.

"Hepsini buda" isteği CLI'da `-all` bayrağı ve `ListApps` üzerinden bir
döngü. Orası ayrıcalıksız kod; oradaki bir hata geri alınabilir, şemadaki
bir belirsizlik değil.

Aynı akıl yürütme fail-closed'da da geçerli: aktif sürüm okunamıyorsa
hiçbir şeye dokunulmuyor. "Korunacak sürüm yok, öyleyse hepsini sil",
`app delete`'in yıkıcılığını kapısız bir komuta kaçırmak olurdu.

### ⚠ Bir mutasyon, SEKİZ testin göremediği bir deliği ölçtü

"`PreviousActiveRelease` hatası sessizce yutuluyor" mutasyonu sekiz
testin sekizini de **yeşil** geçti. Sebep: `default:` dalı testte hiç
uyarılmıyordu, çünkü sahte depo yalnızca başarı ya da
`ErrNoPreviousDeployment` döndürüyordu — üçüncü bir hata tipi hiç
üretilmiyordu.

Yutulduğunda ne olurdu: geçici bir okuma hatasında saklama kümesi
`{aktif}` olarak kalır ve budama **geri alma hedefini siler.** Sessiz,
yıkıcı, ve başarılı görünerek.

Düzeltme `deploymentReader` arayüzü oldu. Gerekçe Executor arayüzüyle
birebir aynı: **başarısızlık yolları ancak cevap kontrol edilebilirse
sınanabilir.** Somut `*store.Store`'a bağlı kalındığı sürece o dalı
uyarmanın hiçbir yolu yoktu.

### ⚠ Betiğin KENDİSİNDE iki kusur çıktı

**1. `-run` süzgeci yeni testi kapsamıyordu.** Yeni test
`TestKeepSetFailsOnUnexpectedDeploymentError` adındaydı, süzgeç ise
`TestPrune`. Test yazıldıktan sonra bile mutasyon yeşil kaldı ve sebep
kodda değil ÖLÇÜM ARACINDAYDI. K-071'in tam tekrarı.

**2. Bir mutasyon ZAYIF çıktı.** "Saklama kümesi yalnızca aktif sürümü
tutuyor" mutasyonu erken bir `return` ekliyordu; `prev` kullanılmadan
kaldığı için **derleyici** hata veriyordu ve `go test` sıfırdan farklı
dönüyordu. Betik bunu "yakalandı" sayıyordu ama yakalayan test değil
derleyiciydi.

**Yanlış sebeple kırmızı, yanlış sebeple yeşil kadar değersizdir.**
Mutasyon gerekçesiyle çıkarıldı; aynı değişmezi derlenebilir kod üreten
başka bir mutasyon zaten ölçüyor.

### Canlı ölçüm: 292 MB → 6.0 MB

```
budama öncesi   /var/lib/docker/containers   292 M   16 konteyner
budama sonrası                               186 M    7 konteyner
yeniden dağıtım + ikinci budama                6 M    8 konteyner
```

Kontrol grubu sağ kaldı: `elastic_poincare` ve `strange_nash` — Panely
etiketi taşımayan iki konteyner — hiç dokunulmadan duruyor. Testte de
adlarıyla aranıyorlar; sayı saymak yetmezdi, "9 kaldırıldı" iddiası
yanlış dokuzu kaldırılsa da geçerdi.

Rotasyonun uygulandığı da ayırt edici şekilde ölçüldü:

```
YENİ konteyner       map[max-file:3 max-size:10m]
politika ÖNCESİ      map[]
```

İkisi aynı çıksaydı ölçüm hiçbir şey söylemezdi.

### ⚠ Eski bir konteyner saklama kümesindeyse logu BİR TUR daha kalır

Yeniden dağıtımdan sonra eski konteyner geri alma hedefi oluyor ve
politika onu korumak zorunda — devasa logu dahil. Diskin gerçekten
düşmesi için İKİ tur dağıtım gerekti.

Bu bir kusur değil, politikanın doğru sonucu; ama "rotasyonu indirdim,
disk sınırlandı" demek yanlış olurdu. Sunucuda hâlâ bir tane
politika-öncesi konteyner var (`pfprobe_r6`, geri alma hedefi) ve bir
sonraki dağıtımda düşecek.

## K-091 — Yedekleme ve geri yükleme: yerel, zamanlı, SINANMIŞ dönüş

Kontrol düzlemi veritabanı artık saatlik yedekleniyor ve yedekten
GERİ DÖNÜŞ YOLU var. Üç parça: `Store.Snapshot` (VACUUM INTO, ayrı
dizin, 24 tane saklanır), `store.Restore` (doğrula → güvenlik kopyası →
yan dosyaları sil → yerine koy) ve `panely backup create|list`.

Kabul ölçütü yedeğin VARLIĞI değil, GERİ DÖNÜŞÜN sınanmasıydı — K-078
bunu bir kez öğretmişti ve o ders burada da geçerli.

### Ayrıcalıklı yüzey: 0 satır (ölçüldü, tartışılmadı)

Bütçe 2498/2500'dü, yani iki satır kalmıştı ve bu dilim büyük
görünüyordu. Tartışmaya girmeden önce kapsam ÖLÇÜLDÜ:

```
go list -deps ./cmd/panely-exec | grep panely
→ audit, dockerdrv, pb, pbconv, version, exec, sdnotify,
  grpcserve, logutil, peercred, sockets
```

`internal/store`, `internal/api` ve `cmd/panely` bu listede YOK. Yedek
kodu tamamen bütçe dışı bir bölgede yaşıyor. Yüzey 2498'de kıpırdamadı.

**Taşınabilir ders (K-090'ın ikizi):** bütçe tartışmasına girmeden önce
işin bütçenin İÇİNDE olup olmadığı ölçülmeli. İki iş üst üste bütçeyi
hiç kıpırdatmadan bitti.

### Hacim verisi KAPSAM DIŞI — ve bu ölçülerek saptandı

Varsayım değil, ölçüm. Canlı sunucuda:

```
drwxr-x--x root:panely   /var/lib/panely/volumes
drwxr-x--- root:root     /var/lib/panely/volumes/pfprobe
drwxr-x--- 101:101       /var/lib/panely/volumes/pfprobe/veri

sudo -u panely ls /var/lib/panely/volumes/pfprobe/veri/
→ Permission denied
```

panelyd (uid 999) uygulama dizinine **traverse bile edemiyor**: engel
`veri` dizininde değil, bir üstündeki `root:root 0750`'de. Yani hacim
yedeği yetkisiz daemon'dan İMKÂNSIZ; ya yeni bir executor RPC'si
(bütçe) ya da hacim dizinlerinin `panely` grubuna açılması gerekir —
ikincisi "panelyd uygulama verisini okuyamaz" güvencesini takas eder ve
bu kullanıcının kararıdır, benim değil.

Bu yüzden kapsam dışılık ÜÇ yerde birden yazılı: `CreateBackupResponse`
içinde bir alan, CLI çıktısında bir uyarı satırı, ve `--restore`
sonrasında stderr'e bir uyarı. K-088'in dersi: kullanıcı "yedek aldım"
deyince her şeyin yedeklendiğini varsayar.

### Uzak yedek (R2) neden bu dilimde yok

panelyd'nin systemd birimi `IPAddressDeny=any` + yalnızca
`172.16.0.0/12` veriyor. Yani daemon internete ULAŞAMIYOR — R2'ye
yükleme, ağ politikasının gevşetilmesini gerektiren AYRI bir karar.
Master spec §9 Litestream'i Faz 5'e koyuyor ve o sıralama doğru.

### Mutasyon testi, ASIL iddiayı çürüttü

`TestRestoreRemovesStaleWAL` bu dilimin gurur duyduğum testiydi: bayat
bir WAL üretip geri yüklemenin sessizce etkisiz kalmadığını sınıyor.
Mutasyon betiği yan dosya silmeyi TAMAMEN kaldırdı ve test **YEŞİL
KALDI**.

Sebep ölçüldü: `safetyCopy` veritabanını açıp kapatıyor, SQLite temiz
kapanışta WAL'i checkpoint edip yan dosyaları KENDİSİ siliyor.

```
safetyCopy ÖNCE : -wal var, -shm var
safetyCopy SONRA: -wal yok, -shm yok
```

Yani test, silmenin çalıştığını değil SONUCUN doğru olduğunu
kanıtlıyordu. İkisi aynı şey değil. Bu, K-080'in üç sebebinden
üçüncüsü: gizli kod tutarsızlığı.

Silme yine de gerekli — ama yalnızca güvenlik kopyası KOŞMADIĞINDA:
`.db` yokken yan dosyalar duruyorsa (operatör bozuk dosyayı kenara
aldı) hiçbir şey checkpoint etmez.
`TestRestoreRemovesSidecarsWhenNoSafetyCopy` tam o yolu sınıyor ve
mutasyonu yakalıyor.

### integrity_check'in NE YAKALAMADIĞI ölçüldü

Doğrulayıcının ilk yorumu "sayfa düzeyinde bozulmayı da yakalar"
diyordu. **Yanlıştı.** SQLite sayfa başına sağlama tutmuyor:

| bozulma | integrity_check |
|---|---|
| 1024–2048 XOR (sayfanın boş alanı) | **"ok"** |
| 2. sayfa sıfırlandı | `btreeInitPage() returns error code 11` |
| kuyruk XOR | hata: `disk image is malformed` |
| dosya kesildi | hata: `disk image is malformed` |
| başlık XOR | hata: `file is not a database` |

Güvence "bit düzeyinde sağlam"a değil, "açılabilir ve yapısı tutarlı"ya
eşit. Yorum düzeltildi.

Bu ölçüm iki testi de düzeltti: hem "bozuk dosya reddedilir" testi
yanlış bozulma biçimi kullanıyordu, hem de `result != "ok"` dalı hiç
çalışmıyordu (kesilmiş dosya HATA veriyor, metin değil).

### Mutasyon betiğinin kendisi de zayıf çıktı

`mutate-restore.sh` 11 mutasyon taşıyor. İlk turda BEŞİ yeşil kaldı ve
üçü farklı sebepten:

- **zayıf test:** `SnapshotDir` mutasyonu, testin beklentiyi sınanan
  fonksiyonun kendi çıktısıyla karşılaştırmasından geçti — totolojik
  iddia. Beklenti artık elle yazılıyor.
- **zayıf mutasyon:** "şema sorgusunun hatası yutuluyor" davranışı hiç
  değiştirmiyordu; hemen ardından gelen `n == 0` kontrolü reddi yine
  üretiyor. İki kontrol birbirini yedekliyor, yani mutasyon gerçek bir
  kusur değildi. Yerine şema doğrulamasının TAMAMINI kaldıran mutasyon
  kondu.
- **gizli tutarsızlık:** yukarıdaki WAL bulgusu.

K-080 bu üç sebebi ayrı ayrı saymıştı; bu dilimde ÜÇÜ BİRDEN aynı
betikte çıktı.

### Geri yükleme neden RPC değil

Çalışan daemon'ın altından veritabanını çekmek, açık dosya tanıtıcısı
eski inode'u tuttuğu için "geri yükledim ama hiçbir şey değişmedi"
durumunu üretir. `panelyd --restore <yedek>` sunucuda, daemon KAPALIYKEN
koşuyor ve soketi yoklayarak bunu DOĞRULUYOR (soket dosyasının varlığı
yetmez — temiz kapanmayan daemon onu geride bırakır; ölçülen şey
bağlanabilirlik).

`panely` CLI'ı da kullanılamazdı: o istemci makinesinde duruyor ve
api.sock üzerinden konuşuyor — yani tam da kapalı olması gereken
daemon'a.

### CANLI SUNUCU doğrulaması (17 Eyl)

Binary `/proc/<pid>/exe` ile doğrulandı (`systemctl is-active` YETMEZ —
[[service-active-does-not-prove-new-binary]]):

```
md5sum /proc/3722166/exe → 481283f8ba02dd85a58bad4b1847c783
yerel bin/linux-amd64/panelyd → 481283f8ba02dd85a58bad4b1847c783
```

Açılış yedeği alındı: 131.072 bayt, **12,7 ms**, dizin `0700 panely:panely`.
Sıfır yeniden başlatma, journal temiz, `panely.erkanrzgc.dev` → 200.

#### Geri yükleme TATBİKATI — üretime dokunmadan

Geri yükleme üretim veritabanını takas eden yıkıcı bir işlem. Tatbikat
bu yüzden `--db` ile AYRI bir kopya üzerinde koştu: gerçek binary,
gerçek dosya sistemi, gerçek SQLite, sıfır üretim riski.

Ayırt edici olması için tatbikat veritabanına üretimde OLMAYAN bir
işaretçi kondu:

```
ÖNCE : ISARETCI: geri-yukleme-oncesi   · uygulama sayısı 3
SONRA: isaretci YOK (no such table)    · uygulama sayısı 3
```

İşaretçi gitti, uygulamalar kaldı — yani dosya gerçekten değişti ve
yedeğin içeriği sağlam.

**En güçlü kanıt güvenlik kopyasında:** içinde İKİ işaretçi de var,
`wal-icinde` dahil. O satır WAL modunda yazılmıştı. Yani `VACUUM INTO`,
ham `cp`'nin KAYBEDECEĞİ WAL'deki yazmaları gerçekten taşıyor — bu
dilimin tasarım gerekçesi canlıda ölçülmüş oldu.

Geri yüklemeden sonra dizinde yalnızca `panely.db` ve `backups/` kaldı;
yan dosyalar yok.

#### Denetim zinciri: kasıtlı eylem girer, zamanlayıcı girmez

```
121 | backup.create | {"bayt":"131072","dosya":"panely-20260917T153535Z.db"}
```

Parametre yalnızca dosya ADINI taşıyor, tam yolu değil. Kontrol
grubuyla doğrulandı (`app.prune` satırları dolu döndü) — ilk sorgu
yanlış tablo adı kullanmıştı ve kontrol grubu boş sonucu "sızıntı yok"
diye okumamı ENGELLEDİ.

Zamanlı yedek zincire GİRMİYOR, yalnızca `panely backup create`
giriyor: canlıda ölçüldü (açılış yedeği journal'da var, zincirde yok).
Saatte bir otomatik kayıt yılda ~8.760 satır demek ve asıl önemli
girdileri boğardı.

#### Bu dilimin KAPATMADIĞI şey

- **Hacim verisi** — ölçülerek kapsam dışı bırakıldı (yukarıda).
- **Uzak kopya** — yedekler sunucunun KENDİ diskinde. Disk ölürse
  yedek de ölür. `IPAddressDeny=any` yüzünden panelyd dışarı
  konuşamıyor; çözüm ya ağ politikası ya sunucu dışından `rsync`/
  Litestream. Master spec bunu Faz 5'e koyuyor.
- **Zamanlayıcı daemon'a bağlı** — panelyd çökmüşse yedek de alınmaz.
  Sıradaki iş (alarm) bunu görünür kılacak.

### CI, mutasyonun YANLIŞ SEBEPLE yakalandığını gösterdi

Yerelde (Windows) `mutate-restore.sh` 11/11 yakalıyordu. CI'da (Linux)
biri kaçtı:

```
KIRMIZI OLMADI: damga değişken genişliğe çevrildi
```

Mutasyon `snapshotStamp`'i `20060102T150405Z`'den RFC3339Nano'ya
çeviriyor. Windows'ta yakalanmasının sebebi **sıralamanın bozulması
değildi**: RFC3339Nano dosya adına `:` koyuyor, Windows bu karakteri
dosya adında kabul etmiyor, `VACUUM INTO` başarısız oluyor ve test
kırmızıya dönüyordu. Linux `:` kabul ediyor, dosyalar oluşuyor — ve
testin kullandığı zamanlar TAM DAKİKA olduğu için saniye altı kısım her
zaman boş kalıyor, yani damgalar zaten eşit genişlikte çıkıyor ve
sıralama bozulmuyordu.

Yani mutasyon iki platformda da testin **iddia ettiği şeyi**
sınamıyordu.

Düzeltme, biçimin kendisini ölçen bir test:
`TestSnapshotStampIsFixedWidth` saniye altı kısımları KASTEN farklı
zamanları biçimlendirip uzunluklarının eşit olduğunu doğruluyor
(ve dosya adında geçersiz karakter bulunmadığını). Mutasyon artık doğru
sebeple yakalanıyor: `uzunluk 22, 20 bekleniyordu`.

**Taşınabilir ders:** yakalanan bir mutasyon da sorgulanmalı. K-080
"yeşil kalan mutasyonun üç sebebi"ni sayıyordu; bunun simetriği de var:
**kırmızıya dönen bir mutasyon, beklenen sebeple dönmemiş olabilir.**
Platform farkı bu sınıfı görünür kılan en ucuz araç — mutasyon
betiklerinin CI'da (Linux) koşması, yerel Windows koşusunun tek başına
yeterli olmadığını kanıtladı.

### Budama ÜRETİM YOLUNDAN hiç koşmamıştı

Budamayı sınayan bütün testler `snapshotAt`'i çağırıyordu — yani saati
enjekte edilen TEST yolunu. Üretimde çağrılan `Snapshot()` ise
`time.Now()` kullanıyor ve budamayı ilk kez **24 yedek biriktikten
sonra**, yani kurulumdan bir gün sonra tetikliyor.

Kısacası üretim girişi hiç budama yapmamıştı: ne yerelde, ne canlıda.
Bir hata olsaydı 24 saat sonra, GERÇEK yedekler silinirken ortaya
çıkardı.

Canlı sunucuda ölçüldü. 30 sentetik yedek + 3 gerçek yedek biriktirilip
panelyd yeniden başlatıldı (açılış yedeği → `Snapshot` →
`pruneSnapshots`):

```
ÖNCE : panely-*.db 33 · pre-restore-*.db 1 · göç yedeği 2
SONRA: panely-*.db 24 · pre-restore-*.db 1 · göç yedeği 2
en eski kalan: panely-20260911T000000Z.db  (01–10 silindi)
```

Üç ayrı ad uzayının birbirine karışmadığı da böylece ÖLÇÜLDÜ, akıl
yürütmeyle değil: budama yalnızca kendi desenine dokundu, geri yükleme
güvenlik kopyasına ve göç öncesi yedeklere dokunmadı.

Testte de kapatıldı: `TestSnapshotPrunesViaProductionPath` eski adlı
dosyalar biriktirip GERÇEK `Snapshot`'ı bir kez çağırıyor, ve
`mutate-restore.sh`'a "üretim yolunda budama çağrısı kaldırıldı"
mutasyonu eklendi (12/12 yakalanıyor).

**Taşınabilir ders:** saati enjekte edilebilir yapmak testi mümkün
kılıyor ama bir tuzak da kuruyor — bütün testler enjekte edilen yolu
kullanırsa, ÜRETİM girişi hiç sınanmamış olur. En az bir test gerçek
girişten geçmeli.

### Bilinen ve kabul edilen: kurulum yolu iki yerde yazılı

`panely backup list` çıktısı geri yükleme yordamını yazarken
`/usr/local/lib/panely/panelyd` yolunu **sabit** taşıyor
(`cmd/panely/backup.go`), oysa gerçek kurulum yolu systemd biriminde
duruyor. Bugün doğru ve tatbikat tam o yola karşı koştu, ama bu
`--config /etc/panely/panelyd.toml` yarasıyla aynı sınıf: iki yer, tek
doğruluk kaynağı yok.

Bilerek kontrol YAZILMADI — çıktı metni için birim dosyasını ayrıştıran
bir test, koruduğundan fazla kırılganlık getirirdi. Kayıt burada dursun.

## K-092 — Alarm: TESPİT bitti, TESLİMAT ayrı bir karar

> ✅ **K-095 bu kayda "ölçülmemiş iddia" işareti koymuştu — O İŞARET
> YANLIŞTI, bkz. K-099.** `panely alarms` çıktısına dair iki blok da
> 17 Eyl **20:12:46Z** ve **20:15:05Z**'te, yerel `bin/panely.exe` ile
> `panely-client@` SSH taşıması üzerinden gerçekten koştu — bu kayıt
> 20:16:43Z'te yazıldı. Kaydın tamamı geçerlidir.

Dört arıza koşulu artık kenar tetiklemeli olarak bildiriliyor:
`heal_exhausted`, `backup_failed`, `proxy_unreconciled`, `disk_low`.
Hedef journal ve `panely alarms`.

### Dilim neden ikiye bölündü

Alarmın iki bağımsız yarısı var: **tespit** (hangi koşul, hangi eşik,
ne zaman) ve **teslimat** (baytları Telegram'a götürmek). İkincisinin
bir duvarı var ve duvar ÖLÇÜLDÜ:

```
systemd-run --uid=panely --property=IPAddressDeny=any \
            --property=IPAddressAllow=172.16.0.0/12 \
            curl https://api.telegram.org/
→ status=6   (couldn't resolve host)

KONTROL GRUBU — aynı istek kısıtsız:
→ 302
```

Kontrol grubu şart: `status=6`'yı tek başına okumak "sunucunun
internet'i yok" diye de yorumlanabilirdi. İkisi birlikte, engelin
politikanın kendisi olduğunu kanıtlıyor.

Teslimat için ya panelyd'nin ağ politikası gevşetilmeli (ele geçirilen
daemon'a dışarı sızma yeteneği vermek demek) ya ayrı bir gönderici
süreç yazılmalı. İkisi de bu dilimden büyük ve ikisi de kullanıcının
kararı. Tespit o kararı beklemek zorunda değil — ve teslimatı önce
yapmak, hangi alarmların doğru olduğunu bilmeden boru döşemek olurdu.

### Ayrıcalıklı yüzey: yine 0 satır

`go list -deps ./cmd/panely-exec | grep panely` çıktısında
`internal/health` YOK. Üç iş üst üste (K-090 budama, K-091 yedekleme,
K-092 alarm) bütçeyi hiç kıpırdatmadan bitti; yüzey 2498'de duruyor.

### Kenar tetikleme neden VERİTABANINDA

Alarm durumu panelyd'nin ömründen UZUN yaşamak zorunda. Bellekte
tutulsaydı `Restart=on-failure` ile çöküp kalkan bir daemon her
açılışta BÜTÜN alarmları yeniden ateşlerdi — yani en çok gürültüyü tam
da en kötü durumda üretirdi.

Tekilleştirme uygulama mantığında bir `if` ile değil, BİRİNCİL ANAHTAR
çakışmasıyla yapılıyor (`ON CONFLICT(id) DO NOTHING`): iki eşzamanlı
yükseltme aynı satırı hedefler ve yalnızca biri kazanır.

`since` KORUNUYOR: üzerine yazan bir uygulama, üç gündür bozuk olan bir
şeyi her turda "az önce bozuldu" diye gösterirdi.

### "Bir kez bildir" kuralının açtığı boşluk

Kural tek başına "kötüleştiğini haber verme"ye dönüşüyordu: %9 boş
diskle uyarı açılır, disk %3'e inince ikinci yükseltme sessizce
yutulurdu. `EscalateAlarm` bunu kapatıyor — ciddiyet ARTIYORSA satır
güncellenir ve yeniden bildirilir.

Tersi YAPILMIYOR: kritikten uyarıya düşürme yok. Eşiğin etrafında gidip
gelen bir koşul aksi hâlde her turda ciddiyet değiştirirdi.

Aynı gerekçe disk eşiklerinde HİSTEREZİS olarak duruyor: alarm %15'te
açılıyor ama ancak %20'nin üstünde kapanıyor. Aradaki bant kasten
"ne aç ne kapat" bölgesi.

### Ölçemiyorsak alarm AÇMIYORUZ

Executor'a ulaşılamadığında ya da disk toplamı sıfır okunduğunda alarm
durumu DEĞİŞTİRİLMİYOR. "Ölçemedim" ile "disk doldu" aynı şey değil ve
birincisini ikincisi gibi bildirmek yanlış alarmın tanımı olurdu.

### 🔴 Mutasyon betiğinin EN ÖNEMLİ dört satırı hiçbir şey ölçmüyordu

`mutate-alarm.sh` ilk koşuda 16/16 "yakalandı" verdi. K-093'ün kendi
dersi uygulandı — yakalanan mutasyon da sorgulanmalı — ve dördünün
sahte olduğu görüldü:

```
if fresh {  →  if true {
go test → FAIL  github.com/.../internal/alarm [build failed]
                "fresh declared and not used"
```

Yani kenar tetiklemeyi sınayan DÖRT mutasyonun hiçbiri testin iddiasını
ölçmemişti; `go test` derleme hatasından düşüyordu ve betik bunu
"yakalandı" diye okuyordu.

Düzeltme iki katmanlı:

1. Mutasyonlar `_ = fresh` ile derlenebilir yapıldı. Artık kırmızı
   gerçekten iddiadan geliyor:
   `10 turda 10 bildirim üretildi, 1 bekleniyordu`
2. **Betiğe DERLEME KAPISI eklendi.** `mutate()` artık testi koşmadan
   önce `go build` yapıyor; derlenmeyen mutant "ölçüm YAPILMADI" diye
   raporlanıyor. Bu, sınıfın tamamını kapatıyor.

⚠ **Bu kusur muhtemelen diğer mutasyon betiklerinde de var.** Derleme
kapısı yalnızca `mutate-alarm.sh`'a eklendi; diğer on bir betik
taranmadı. Ayrı bir iş.

### CANLI SUNUCU doğrulaması (17 Eyl)

Binary `/proc/<pid>/exe` ile doğrulandı (`ff628d6b…`). Göç 0008
uygulandı ve **K-091'in göç-öncesi yedeği kendiliğinden ateşledi**:

```
/var/lib/panely/panely.db.pre-0006_app_env
/var/lib/panely/panely.db.pre-0007_app_volumes
/var/lib/panely/panely.db.pre-0008_alarms   ← yeni
```

Yani bu dilimin dağıtımı, bir önceki dilimin mekanizmasını gerçek
koşulda sınamış oldu.

#### Sağlıklı sistemde SIFIR gürültü

```
panely alarms → "etkin alarm yok"   (çıkış kodu 0)   ← 20:12:46Z, bkz. K-099
journal ALARM satırı → yok
disk %87 boş → eşiklerin çok üstünde, alarm yok
```

Bu negatif sonuç, pozitif olanlar kadar önemli: sağlıklı bir sunucuda
alarm üreten bir sistem ilk günden kapatılırdı.

#### Kenar tetikleme KESİN olarak ölçüldü

Yedek dizini yazılamaz yapıldı (`chmod 0500`), panelyd **iki kez** üst
üste yeniden başlatıldı, sonra izinler düzeltilip bir kez daha
başlatıldı. Aynı pencerede sayım:

```
panelyd başlangıcı : 4
durum=acildi       : 1
durum=kapandi      : 1
```

**Dört başlangıç, tek bildirim.** İkisinde yedek başarısızdı ve ikinci
başarısızlık SESSİZ kaldı — alarm zaten açıktı. `since` damgası da
değişmedi (`1789676049`).

Bu, durumun bellekte tutulması hâlinde KESİNLİKLE başarısız olacak
tek ölçüm: bellekteki bir uygulama her açılışta yeniden ateşlerdi ve
sayım 2 çıkardı. Süreç sınırını aşan kenar tetikleme burada kanıtlandı.

Kapanış da ölçüldü: izinler düzelince `durum=kapandi` (WARN seviyesi,
açılış ERROR'du) ve tablo boşaldı.

#### Operatör yüzeyi

> ✅ **Aşağıdaki blok 20:15:05Z'te ölçüldü** (K-099). K-095 buraya
> "ölçülmedi" işareti koymuştu: sunucudaki CLI'ın 2 Eylül tarihli
> olduğunu görüp ölçümün O ikiliyle yapıldığını varsaymıştı. Ölçüm
> operatörün makinesindeki CLI ile, SSH üzerinden yapılmıştı.

```
CİDDİYET  SÜREDİR  TÜR            HEDEF      AYRINTI
KRİTİK    16sn     backup_failed  panely.db  zamanlı yedek alınamıyor…
UYARI: alarmlar DIŞARI GÖNDERİLMİYOR — …
ÇIKIŞ KODU: 1
```

Çıkış kodu 1 kasıtlı: etkin alarm varken sıfır dönmek,
`panely alarms && echo tamam` gibi bir kabuk zincirinde arızayı
görünmez kılardı.

Tatbikat boyunca site 200 döndü, otomatik yeniden başlatma sayısı 0.

#### Bu dilimin KAPATMADIĞI şey

- **Teslimat** — alarmlar sunucudan ÇIKMIYOR. Görülmeleri için ya
  `panely alarms` çalıştırılmalı ya journal okunmalı. Ağ duvarı
  ölçüldü; çözüm kullanıcının kararı.
- **Vekil alarmı yalnızca AÇILIŞTA** — uzlaştırma açılışta koşuyor,
  dolayısıyla sonraki bir bozulmayı bu koşul görmez.
- **Gözetmenin kendi durumu hâlâ bellekte** — panelyd çökünce geri
  çekilme sayaçları sıfırlanır. Alarm durumu diskte, gözetim durumu
  değil; ikisi kasten ayrı.

---

## K-095 — Hiç koşmamış yollar ölçüldü; ~~K-092'de ölçülmemiş bir iddia bulundu~~ (YANLIŞ — K-099)

**Tarih:** 17 Eylül 2026
**Durum:** ölçüm + düzeltme; kod değişikliği yalnızca iki test

> ⚠ **Bu kaydın İKİ bölümü YANLIŞ — bkz. K-099:** "K-092'de ÖLÇÜLMEMİŞ
> İDDİA" ve "SSH taşıması HİÇ kurulmamış". İkisi de aşağıda
> işaretlendi. Geri kalanı (kapatma yolları, root kontrol grubu,
> sessiz ret, çıkış kodu çakışması, kimlik testi) bağımsız olarak
> ölçüldü ve geçerlidir.

K-092'nin dört alarm koşulundan yalnızca `backup_failed` canlıda
koşmuştu. `disk_low` ve `proxy_unreconciled` hiç ateşlenmemişti: sunucu
%87 boş diskle her turda "kapat" dalına giriyor, vekil de her açılışta
uzlaşıyordu. Yani üretimde **yeşil olup hiç yürünmemiş** iki yol vardı.

### Önce ucuz kalıcı koruma: kimlik eşleşmesi

`checkDisk` alarmı `diskAlarm()` ile açıyor, `diskAlarmID` ile
kapatıyor. İkisi ayrışsa alarm açılır ama **bir daha asla kapanmazdı**
— ve kapanmayan alarm, bakılmayan alarma dönüşür. Hiçbir mevcut test
bunu göremezdi, çünkü her iki taraf da aynı sabitten besleniyor.
`TestDiskAlarmIDMatchesClearID` bu sapmayı kilitliyor.

### Canlı ölçüm 1 — kapatma yolları

İki alarm elle kuruldu (kodun kullandığı TAM kimliklerle), panelyd
yeniden başlatıldı:

```
WARN msg=ALARM alarm=proxy_unreconciled:host durum=kapandi
WARN msg=ALARM alarm=disk_low:host           durum=kapandi
kalan satır: 0
```

`checkDisk`'in `diskClear` dalı ve `recordProxyAlarm(problem=="")`
yolu, gerçek `alarm.Manager` ve gerçek store ile **ilk kez** koştu.
Kimlik dizgilerinin iki tarafta da eşleştiği böylece kanıtlandı.

### Canlı ölçüm 2 — `panely alarms`in GERÇEK çıktısı

```
CİDDİYET  SÜREDİR  TÜR                 HEDEF  AYRINTI
KRİTİK    2sa      proxy_unreconciled  host   elle: vekil yapılandırılamadı
uyarı     1sa      disk_low            host   elle: %9 boş, 3.4 GiB / 38 GiB
UYARI: alarmlar DIŞARI GÖNDERİLMİYOR — …
çıkış kodu 1
```

KRİTİK öne sıralandı, `SÜREDİR` sütunu doğru, teslimat uyarısı bastı.

### Canlı ölçüm 3 — KONTROL GRUBU: root REDDEDİLİYOR

Aynı komut root olarak:

```
failed to write client preface: broken pipe   (çıkış 1)
```

panelyd istemci grubu üyeliğini **root'a karşı bile** uyguluyor. Bu
iyi bir özellik ve daha önce hiç ölçülmemişti. Kontrol grubu olmasa
`panely-client`'ın başarısı "herkes bağlanabiliyor" diye okunabilirdi.

⚠ **Ret SESSİZ.** Journal'da tek satır yok. Bağlanamayan bir istemciyi
teşhis eden operatör sunucu tarafından hiçbir şey alamıyor. Bu, projenin
kendi kuralıyla aynı aileden bir kusur: teşhis edilemeyen bir ret,
yanlış alarm kadar güven yakar.

### 🔴 K-092'de ÖLÇÜLMEMİŞ İDDİA — ⚠ BU BÖLÜM YANLIŞ (K-099)

> ⚠ **Aşağıdaki kanıt yalnızca SUNUCUDAKİ ikilileri saydı.** CLI
> tasarım gereği operatörün makinesinde koşar ve sunucuya
> `panely-client@` ile bağlanır. K-092'nin ölçümü tam olarak böyle
> yapılmıştı (20:12:46Z, yerel `bin/panely.exe`, 20:07:30Z'te
> derlenmiş). Sayım o ikiliyi hiç içermedi; "hiçbir sunucu ikilisi
> bunu yapamazdı" doğruydu, "hiç koşmadı" değildi.

K-092, "CANLI SUNUCU doğrulaması" başlığı altında `panely alarms`
çıktısı ve `çıkış kodu 0` bildiriyordu. Kanıt bunun **koşulmadığını**
gösteriyor:

```
/tmp/panely-stage/panely → 2 Eylül tarihli (c33b58d)
--help → ne `alarms` var ne `backup`
/usr/local/bin → BOŞ; sunucuda başka panely ikilisi yok
```

CLI, kurulduğu iddia edilen komutlardan iki hafta eskiydi. O satırlar
uygulamaya bakılarak yazıldı.

**En rahatsız edici yanı:** biçim, bugün gerçekten ölçülünce **doğru
çıktı**. Yani kayıt okunarak yakalanamazdı. Ölçülmemiş ama doğru görünen
bir iddia, yanlış olandan daha tehlikelidir — kendini ele vermez.
"Ölç, iddia etme" kuralının koruduğu şey tam olarak budur ve bu kez
kural bizzat kuralı yazan kayıtta çiğnendi.

### Yeni bulgu — çıkış kodu ÇAKIŞMASI (kod DEĞİŞTİRİLMEDİ)

`1` hem "etkin alarm var" hem "bağlanamadım" demek. Kontrol grubunda
ikisi de 1 döndü.

Zararlı yön **kapalı**: bağlantı arızası "sorun var" diye okunuyor,
yani sessizce iyi görünmüyor. Ters yön (alarm var ama 0 dönmek)
mümkün değil. `cmd/panely/main.go` doğrulandı: 0/1/2/3 tanımlı, **4
gerçekten boşta** — ama bu bir sözleşme değişikliği ve `panely alarms`
üretimde yalnızca birkaç kez koştu (⚠ burada "BİR kez" yazıyordu; K-092'nin
iki SSH koşusu sayılmamıştı — K-099). Kullanım verisi yokken sözleşmeyi
değiştirmek, bu kayıttaki hatanın aynısını tekrarlamak olurdu.
**Bilinen boşluk olarak bırakıldı; karar kullanıcının.**

### Yan bulgu — SSH taşıması bu sunucuda HİÇ kurulmamış — ⚠ YANLIŞ (K-099)

> ⚠ **Yoklama yanlış dizine baktı.** Komut
> `cat /home/panely-client/.ssh/authorized_keys 2>/dev/null` idi;
> kullanıcının ev dizini `/var/lib/panely-client`. `2>/dev/null`
> "dosya yok" hatasını yuttu ve yokluk "boş" diye okundu. Gerçekte
> dosyada zorlanmış komutlu (`panely-connect`, `restrict`) bir anahtar
> var (8 Ağu) ve 4 Ağu – 17 Eyl arasında **102** kabul edilmiş
> `panely-client` girişi journal'da duruyor.

`panely-client` kullanıcısı var ama `authorized_keys` **boş**, zorlanmış
komut yok. `panely <komut> kullanıcı@sunucu` bu sunucuya karşı hiç
çalışmadı; `root@` ile denenince MOTD gRPC önsözünü bozuyor
("frame too large") — hata doğru ama sebebi hedefin yanlış olması.

Bunun kapsamı bu dilimden geniş: projenin **bütün** CLI canlı
doğrulamaları yerel soket üzerinden yapılmış, belgelenen SSH yolundan
değil. K-091 ve öncesi bu gözle okunmalı. Ayrı bir iş.

### Bilinen boşluk CANLIDA görüldü

Tatbikat temizlenirken `disk_low` kendiliğinden kapandı (5 dakikalık
tur), ama `proxy_unreconciled` **tabloda kaldı**. K-092 bunu zaten
"vekil alarmı yalnızca AÇILIŞTA" diye kaydetmişti; burada sonucu
görüldü: uzlaştırma yeniden koşana kadar kapanamayan bir alarm.

Satır elle silinmedi — panelyd yeniden başlatıldı ve alarm üretim
yolundan kapandı (`durum=kapandi`). Temizliğin kendisi bir ölçüm oldu.

### Taşınabilir ders

Bir mutasyonun yanlış sebeple kırmızıya dönebileceğini K-093'te
öğrenmiştik. Bu kayıt bir üstünü ekliyor: **bir ölçümün hiç yapılmamış
olabileceğini de sınamak gerekir.** "Canlı doğrulandı" başlığı,
altındaki her satırın gerçekten koştuğunu kanıtlamaz. Kanıt, ölçümü
üreten ikilinin kimliğidir — yeni sürüm yüklendiğine dair kanıt
(K-088'deki `/proc/<pid>/exe`) daemon için vardı, **CLI için yoktu**.

> ⚠ Ders geçerli, **ÖRNEĞİ yanlıştı** (K-099): bu kayıt ikiliyi yanlış
> makinede aradı. Suçlama da bir ölçümdür ve aynı kurala tabidir.

---

## K-096 — On iki mutasyon betiği denetlendi: 147 mutasyonun 14'ü hiçbir şey ölçmüyordu

**Tarih:** 18 Eylül 2026
**Durum:** denetim + düzeltme; iki GERÇEK test boşluğu bulundu ve kapatıldı

K-092, derleme kapısı kusurunun diğer on bir betikte de olabileceğini
kaydetmişti. Varsayılmadı, **ölçüldü**.

### Ölçüm

Kapı on iki betiğe de eklendi ve hepsi koşuldu:

```
147 mutasyon
├─ 133 gerçekten ölçüyordu
└─  14 SAHTE  (mutant derlenmiyordu, `go test` derleme hatasından
               düşüyordu ve betik bunu "yakalandı" diye okuyordu)
```

Sahtelerin dokuz betiğe dağıldığı görüldü. Hepsi taşıyıcı özellikleri
hedefliyordu: API canlılık kontrolü, şema doğrulaması, çelişki
kontrolü, belirlenimsiz JSON, budama. **Bu on dört özellik için
testlerin koruduğuna dair hiçbir kanıt yoktu.**

### Kapı neden `go build` değil

`go test <pkg> -run '^$'` seçildi: paketi **ve test dosyalarını**
derler, hiçbir test koşmaz. `go build` yalnızca üretim kodunu derler;
test kodunun derlenmesini bozan bir mutasyon yine sahte "yakalandı"
verirdi. (`internal/client` dışında hiçbir hedef pakette `TestMain`
yok, dolayısıyla `-run '^$'` yan etkisiz.)

K-092'de `mutate-alarm.sh`'a konan `go build` kapısı bu yüzden
yükseltildi. Kapı ayrıca derleyici çıktısını **bastırıyor**: sebebini
göstermeyen bir kapı düzeltmeyi zorlaştırıyordu.

### Sahtelerin üç sebebi

| sebep | adet | düzeltme |
|---|---|---|
| `declared and not used` | 7 | `_ = x` |
| `imported and not used` | 3 | import'u kullanan ETKİSİZ çağrı |
| `undefined` | 2 | silinen bildirim geri kondu |
| kabuk kaçışı sızması | 1 | aşağıda |
| kod kayması | 1 | aşağıda |

**Kabuk kaçışı sızması.** `mutate-appdelete.sh` Go kaynağına
`false \&\& err` yazıyordu: `\&\&` bash'te çift tırnak içinde
korunuyor ve Python dizesine harfiyen giriyor. Go bunu `& &` artı
geçersiz karakter olarak okuyor.

**Kod kayması — ve `mutate()`'in yapısal kusuru.**
`mutate-env.sh`'ın üç `replace`'inden ikincisi artık eşleşmiyordu:
hacimler eklenince UPDATE cümlesi `string(env), string(vols), …`
olmuştu. `mutate()` yalnızca "dosya toptan değişti mi" diye
baktığından, **yarım uygulanmış bir mutasyon tam mutasyon gibi
ölçülüyordu.**

Kapatıldı — 147 ifadenin hiçbirine dokunmadan: `s` artık bir `str`
alt sınıfı ve `replace` hiçbir şey değiştirmezse süreç düşüyor.

### 🔴 Sahtelerin arkasında İKİ GERÇEK BOŞLUK vardı

Mutantlar derlenir hale gelince ikisi **kırmızıya dönmedi**. K-080'in
kuralı uygulandı (yeşil kalan mutasyon ya testin ya MUTASYONUN
zayıflığını gösterir) ve ikisi farklı çıktı:

**1. `sortedVolumes` — MUTASYON zayıftı.** "Hep false" karşılaştırıcı
küçük dilimde sırayı hiç değiştirmiyordu. Sıralama **tersine**
çevrilince yakalandı. Test suçsuzdu.

**2. `pruneGrace` — SAHTE zayıftı, test değil.** Budamanın SIGTERM ile
SIGKILL arasında verdiği süreyi hiçbir test sınamıyordu. Sebep:
`fakeExec.StopRelease` süreyi `_ time.Duration` ile **atıyordu** —
sahte, tam da sınanması gereken argümanı düşürüyordu.

Sıfır süre demek, budanan konteynerin anında SIGKILL ile öldürülmesi
demek: açık bağlantılar kopar, tampondaki yazımlar diske inmez.
"Durdurma" sessizce "öldürme"ye dönüşürdü. `stopGraces` kaydediliyor
ve `TestPruneGivesContainersTimeToStop` eklendi.

**İki kusur üst üste duruyordu:** sahte kaydeden bir sahte nesne, ve
onu görünmez kılan bir mutasyon betiği.

### CI: kapı ZORUNLU

Kapıları tek tek eklemek yetmez — kapısız eklenen yeni bir betik
sınıfı sessizce geri getirirdi. CI artık her `scripts/mutate-*.sh`
dosyasında kapının varlığını şart koşuyor. Denetimin kendisi de
sınandı: bir betikten kapı kasten kaldırılınca kırmızıya döndü.

### Son durum

```
147 mutasyon · 147 yakalandı · 0 sahte · 0 yarım · 0 boşluk
```

### Taşınabilir ders

K-093 "yakalanan mutasyon yanlış sebeple kırmızı olabilir" dedi.
K-095 "bir ölçüm hiç yapılmamış olabilir" ekledi (örneği yanlıştı,
bkz. K-099). K-096 üçüncüsünü
koyuyor: **ölçüm aracının kendisi düzenli denetlenmeli.** Betikler
iki buçuk hafta boyunca (ilki 1 Eyl) "bütün mutasyonlar yakalandı" raporlarken
onda biri hiçbir şey ölçmüyordu — ve rapor hep yeşildi.

Bir kusur bir yerde bulunduğunda sorulacak soru "düzelttim mi"
değil, **"aynı sınıf başka nerede?"**

---

## K-097 — Sunucu sertleştirmesi: saldırı ÖLÇÜLDÜ, parola girişi kapatıldı

**Tarih:** 18 Eylül 2026
**Durum:** canlıda uygulandı ve kontrol gruplu doğrulandı

Dışarıdan bir Linux sertleştirme kontrol listesi geldi. Maddeleri
tartışmak yerine **sunucu listeye karşı ölçüldü**.

### Zaten daha iyisi vardı

| liste maddesi | panely'deki hâli |
|---|---|
| "Container'ları `127.0.0.1:PORT` ile bağla" | konteynerler host'a **hiç port yayımlamıyor** |
| "Ters vekil kullan" | özel derleme Caddy, `file_server` binary'de YOK |
| "Servisleri root yerine kısıtlı kullanıcıyla çalıştır" | uid 999 + `IPAddressDeny=any` + `CapabilityBoundingSet=` |
| "Otomatik güvenlik güncellemeleri" | `unattended-upgrades` aktif, 0 bekleyen |

Dışa açık dinleyen yalnızca üç port: 22, 80, 443.

### 🔴 Ölçülen asıl açık

```
sshd -T → passwordauthentication yes     (derlenmiş VARSAYILAN;
                                          sshd_config'te tanım YOKTU)
```

Hiçbir dosyada `PasswordAuthentication` satırı yoktu — ayar varsayılandan
geliyordu. "Yapılandırmada yok" ile "kapalı" aynı şey değil; `sshd -T`
etkin değeri okumasaydı bu görünmezdi.

### Saldırı gerçek ve devam ediyor — ÖLÇÜLDÜ

fail2ban kurulur kurulmaz saniyeler içinde üç IP banladı. Ardından
journal okundu:

```
son 24 saat, başarısız giriş : 17.820
farklı kaynak IP             : 119
en çok denenen kullanıcılar  : admin(279) user(238) ubuntu(220)
                               debian(114) deploy(69) test(46)
```

Dakikada ~12 deneme, kesintisiz. Parola girişi açıkken bunların her
biri bir şanstı.

### Yapılanlar

**Swap 4 GiB** (`vm.swappiness=10`): 3,8 GB RAM'li makinede OOM
tamponu. Swappiness düşük — RAM tercih edilsin, swap yalnızca gerçek
baskıda devreye girsin.

**fail2ban**, yalnızca `sshd` jail'i: `maxretry=5`, `bantime=1h`.

**Parola girişi kapatıldı** — `/etc/ssh/sshd_config.d/00-panely-hardening.conf`:

```
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin prohibit-password
```

`00-` öneki kasıtlı: **sshd İLK gördüğü tanımı kullanır**, dolayısıyla
bu dosya `50-cloud-init.conf` gibi sonraki drop-in'leri etkisiz kılar.
Sonuncunun kazandığını varsaymak yaygın bir hatadır.

### Sıra: doğrula → uygula → KONTROL GRUBUYLA doğrula

1. Önce anahtarla girilebildiği kanıtlandı (`-o PasswordAuthentication=no`
   ile bağlanıldı) — kilitlenme riski uygulamadan ÖNCE kapatıldı.
2. `sshd -t` restart ÖNCESİ koşuldu. Bozuk yapılandırmayla restart,
   kendini kilitlemenin klasik yoludur.
3. Restart sonrası **yeni** bir bağlantı açıldı:

```
DENEY          (anahtar)  → GİRİŞ BAŞARILI
KONTROL GRUBU  (parola zorlandı) → Permission denied (publickey)
```

Kontrol grubu şart: yalnızca deneyin geçmesi, parola yolunun hâlâ açık
olup olmadığını söylemezdi.

### Kapatılmayan: UZAK yedek

Yedekler **yalnızca yerel** (`/var/lib/panely/backups`). rclone/restic/
borg kurulu değil. Disk giderse yedekler de gider — K-091'in kapsam
dışı bıraktığı şey buydu ve hâlâ açık. Hedef ve kimlik bilgisi
gerektirdiği için **kullanıcının kararı**.

### Taşınabilir ders

Bir güvenlik tavsiyesi listesi, üzerinde tartışılacak bir metin değil,
**sisteme karşı koşulacak bir ölçüm**dir. Ölçünce iki şey çıktı:
listenin "en kritik" dediği madde zaten daha iyi karşılanmıştı, ve
kimsenin bakmadığı bir varsayılan 24 saatte 17.820 denemeye kapı
tutuyordu.

---

## K-098 — Uzak yedek: şifreli, özel anahtar sunucuda YOK

**Tarih:** 18 Eylül 2026
**Durum:** boru hattı canlıda uçtan uca kanıtlandı; hedef yapılandırması kullanıcıda

> ⚠ **"Uçtan uca" fazla iddialıydı — bkz. K-107.** Sınama yerel bir
> rclone hedefiyle yapıldı; birim AĞA hiç çıkmadı. İlk gerçek ağ
> koşusunda birimin kendi `IPAddressDeny=localhost` kuralı DNS
> çözücüsünü kapattığı için düştü. Şifreleme, çözme ve budama
> mantığına dair ölçümler geçerli; ağ yolu değil.

K-091 yerel yedeklemeyi getirdi ama kapsamı açıkça yereldi: disk
giderse yedekler de giderdi. K-097 bunu kırmızı bir açık olarak
kaydetti. Bu kayıt kapatıyor.

### Şifreleme tercih değil, ZORUNLU — ölçüldü

```
sqlite3 yedek.db "SELECT env_json FROM apps"
→ {"DATABASE_URL":"postgres://panely:<parola>@db:5432/..."}
```

Yedekler uygulama sırlarını düz metin taşıyor. Üçüncü tarafa şifresiz
göndermek, sırları o tarafa vermek olurdu.

### Özel anahtar bu makinede YOK

`age` AÇIK ANAHTARLA şifreliyor. Sunucuda yalnızca alıcı açık anahtarı
duruyor. **Sunucu ele geçirilse bile saldırgan geçmiş yedekleri
çözemez** — yalnızca yenilerini yazabilir.

Bedeli dürüstçe yazıldı: özel anahtar kaybolursa yedekler
kurtarılamaz.

### Neden AYRI bir birim

panelyd `IPAddressDeny=any` taşıyor (K-092, kontrol gruplu ölçüldü).
Yükleme yeteneğini panelyd'ye vermek o özelliği çöpe atardı. Yükleyici
ayrı bir systemd birimi: ağ görüyor, ama **yerel yedeklere yalnızca
okuma** erişimi var (`ProtectSystem=strict`, `ReadWritePaths` YOK).
Ele geçirilse bile yedekleri bozamaz.

`TestOffsiteUploaderIsTheOnlyUnitWithNetwork` bu ayrımı kilitliyor —
kural iki AYRI dosya arasındaki ilişkide yaşadığı için hiçbir birim
testi tek başına göremezdi.

Bu aynı zamanda ertelenen **alarm teslimatı** kararının şeklini de
kuruyor: "ayrı gönderici süreç" artık soyut bir seçenek değil,
çalışan bir örneği var.

### Boru hattı ÜÇÜNCÜ TARAF OLMADAN kanıtlandı

Yerel bir rclone hedefiyle uçtan uca koşuldu:

```
24 anlık görüntü → şifrelendi → yüklendi → boyut DOĞRULANDI
indirildi → YEREL makinede çözüldü → integrity_check = ok
schema_migrations = 8 · 3 uygulama · 131 denetim kaydı · sır yerinde
```

Kontrol grupları:

| deney | sonuç |
|---|---|
| doğru anahtarla çöz | ✅ SQLite format 3 |
| **yanlış anahtarla çöz** | ✅ `no identity matched any of the recipients` |
| şifreli dosyanın başlığı | `age-encryption.o…` (SQLite DEĞİL) |

Çözme **sunucuda değil, yerelde** yapıldı — kanıtlanması gereken yol
buydu.

### 🔴 Çalıştırmasaydık görülmeyecek hata: SONSUZ DÖNGÜ

İlk sürüm en eski uzak yedekleri `OFFSITE_KEEP`e göre siliyordu.
Ölçüldü:

```
OFFSITE_KEEP=5, yerelde 24 anlık görüntü
1. koşu : yüklendi=24            → 19 silindi
2. koşu : yüklendi=19 atlandı=5  → 19 silindi
3. koşu : yüklendi=19 atlandı=5  → 19 silindi
```

Budama, bir sonraki koşunun yeniden yükleyeceği dosyaları siliyordu.
Her koşuda aynı 19 dosya yeniden şifrelenip yükleniyor ve hemen
siliniyordu — **ve birim her seferinde BAŞARILI raporluyordu.** Ücretli
bir sağlayıcıda bu, sonsuza kadar süren ve kimsenin fark etmediği bir
masraftı.

Kural değişti: **hâlâ yerelde olan bir yedek uzaktan silinmez.** Uzak
kopyanın işi, yerel kopya gittikten sonra başlıyor.

Düzeltme iki yönden de doğrulandı:

```
yerelde duran → silinmiyor  (3 koşu: 24 / 0 / 0 yükleme)
yerelde OLMAYAN → siliniyor (uydurma eski dosya kondu, silindi)
```

İkinci ölçüm şart: yalnızca ilkine bakmak "budama hiç çalışmıyor"
durumunu da geçerli gösterirdi.

### Sertleştirme KAZARA kanıtlandı

İlk canlı koşuda 24 dosyanın hepsi düştü:

```
Failed to copy: open /var/backups/...: read-only file system
```

Sebep testin kurgusuydu (yerel hedef), ama sonuç gerçekti:
`ProtectSystem=strict` + `ReadWritePaths` yokluğu **gerçekten**
uygulanıyor. Üretimde hedef ağ olduğu için sorun değil.

Aynı koşu ikinci bir şeyi de kanıtladı: 24 başarısızlık, `yüklendi=0
başarısız=24`, çıkış 1, birim `failed`. **Kısmi başarı başarı
sayılmıyor.**

### Kullanıcıda kalan iki adım

1. `age-keygen` ile anahtar çifti (özel anahtar SUNUCUYA GİRMEZ)
2. `rclone config` ile hedef — ve sağlayıcıda **silme yetkisi VERME**

İkincisi "ele geçirilen sunucu uzak yedekleri silebilir" satırını
kapatan tek şey. Şifreleme okumayı engelliyor, silmeyi engellemiyor.
B2'de `deleteFiles` verilmez; S3'te `s3:DeleteObject` reddedilir.

### Kapatılmayan

- **Arıza bildirilmiyor.** Birim `failed` kalır ama kimse haber almaz.
  Alarm teslimatı hâlâ açık bir karar (K-092).
- **Hacim verisi yine kapsam dışı** (K-091): panelyd o dizinleri
  okuyamıyor.

---

## K-099 — K-095'in suçlaması YANLIŞTI: K-092'nin ölçümü gerçekten koştu

**Tarih:** 21 Eylül 2026
**Durum:** kayıt düzeltmesi; kod değişikliği yok

K-095, K-092'nin `panely alarms` satırlarının "koşulmadan yazıldığını"
kaydetti. **Koşmuşlardı.** İki bağımsız birincil kaynak aynı zaman
çizelgesini veriyor: oturum kaydındaki araç sonuçları (modelin değil,
aracın ürettiği metin) ve sunucunun sshd journal'ı.

```
20:07:02Z  6f0f82d   alarm tespiti commit'lendi
20:07:30Z            bin/panely.exe derlendi (yerel, Windows)
20:12:46Z            ./bin/panely.exe alarms panely-client@46.225.95.35
                     → "etkin alarm yok", çıkış kodu 0
20:12:49Z  sshd      Accepted publickey for panely-client
20:15:05Z            aynı komut, etkin alarm varken
                     → KRİTİK 16sn backup_failed panely.db …, çıkış kodu 1
20:15:21Z  sshd      Accepted publickey for panely-client
20:16:43Z  6f0ce16   K-092 yazıldı
20:21:59Z            OTOMATİK BAĞLAM SIKIŞTIRMASI
20:25:54Z            K-095 yoklaması: /home/panely-client/.ssh/authorized_keys
20:36Z     9ed3640   K-095 push edildi
```

Ölçümün sunucuya karşı yapıldığının iç kontrolü de var: aynı koşu
sunucunun diskini `38G 4.3G 32G 13%` diye bastı — K-092'nin "%87 boş"
satırı. K-092'deki `16sn` değeri de araç çıktısıyla birebir aynı;
uygulamaya bakarak tahmin edilebilecek bir sayı değil.

### Suçlama nasıl kuruldu

K-095 üç ayak üzerinde duruyordu:

| ayak | durum |
|---|---|
| sunucudaki staged CLI 2 Eyl tarihli, `alarms` yok | doğru |
| `/usr/local/bin` boş | doğru |
| `panely-client`'ın `authorized_keys`'i boş | **YANLIŞ** — yanlış dizin |

Kanıt "hiçbir SUNUCU ikilisi bunu üretemezdi" sonucunu destekliyordu ve
bu doğruydu. Yazılan sonuç "hiç koşmadı" oldu. CLI tasarım gereği
operatörün makinesinde koşar ve sunucuya SSH ile bağlanır — sayım o
yolu hiç içermedi.

Üçüncü ayak kritikti: SSH yolunu kapatmış gibi göründüğü için ilk iki
ayak KESİN göründü. İki hata birbirini doğruladı. Yoklama
`cat /home/panely-client/.ssh/authorized_keys 2>/dev/null` idi; ev
dizini `/var/lib/panely-client`. `2>/dev/null` "dosya yok" hatasını
yuttu ve yokluk "boş" okundu. Gerçekte dosyada zorlanmış komutlu bir
anahtar var (8 Ağu) ve 4 Ağu – 17 Eyl arasında **102** kabul edilmiş
`panely-client` girişi journal'da duruyor.

### Sıkıştırma VESİLE, mekanizma değil

Ölçüm ile suçlama arasında otomatik bir bağlam sıkıştırması var; komutun
NASIL koştuğu özete taşınmadı. Ama bu tek başına hatayı üretmedi:
birincil kaynak (oturum kaydı `.jsonl`) başından beri diskteydi ve
18 Eyl'de sıfırlanan bir dosyayı kurtarmak için zaten kullanılmıştı.
Suçlamadan önce ona bakılmadı.

### Düzeltilen yerler

- **K-092:** iki `⚠ ÖLÇÜLMEDİ` bandı ve bir satır içi işaret, ölçüm
  zamanlarıyla değiştirildi.
- **K-095:** başlık, iki bölüm (bantla işaretlendi, SİLİNMEDİ — hatanın
  kendisi kayıtta kalmalı), "BİR kez koştu" satırı, ders notu.
- **K-096:** "iki yıl boyunca" → "iki buçuk hafta" (ilk mutasyon betiği
  1 Eyl'de eklendi; bu da doğrulanmadan yazılmış bir sayıydı).

K-095'in geri kalanı bu düzeltmeden etkilenmiyor: kapatma yolları, root
kontrol grubu, sessiz ret, çıkış kodu çakışması,
`TestDiskAlarmIDMatchesClearID`.

### Ayakta kalan iddia da yeniden ölçüldü: root reddi

Aynı yöntemle kurulmuş olabilirdi — sonuçtan çıkarılmış bir mekanizma.
Bir `root@` SSH denemesi "frame too large" ile düşmüştü (sebebin MOTD
olduğu TAHMİN edildi, ölçülmedi); yani farklı sebepler aynı çıkış kodunu
verebiliyordu. K-095'in ölçümü
ise SSH'sız, sunucudaki yerel sokete yapılmıştı. 21 Eyl'de güncel CLI
ile tekrarlandı (md5 iki uçta aynı, tek ikili, tek soket, aynı dakika):

```
panely-client (istemci grubu)       → "etkin alarm yok"            çıkış 0
root                                → connection reset by peer     çıkış 1
panely (daemon, soketin SAHİBİ)     → connection reset by peer     çıkış 1
journal (aynı pencere)              → boş
```

Üçüncü satır ayırt edici: `panely` soket dosyasının sahibi, yani dosya
izinleri onu durdurmuyor. Onu durduran şey yalnızca `SO_PEERCRED` grup
politikası (`internal/api/credentials.go`, `AllowGIDs`). İddia duruyor
ve sessiz ret de yeniden görüldü.

### En ucuz kontrol hafızadaydı

`real-server-finds-what-tests-cannot` hafıza notu Ağustos başındaki
bootstrap'tan beri "SSH taşıması ilk kez gerçekten kullanıldı" diyor ve
her oturumda bağlama
yükleniyor. "Hiç kurulmamış" iddiası, oturum kaydına bile gerek
kalmadan, zaten okunmuş bir notla çelişiyordu.

### Hâlâ doğru olan

Sunucudaki staged CLI (`/tmp/panely-stage/panely`) gerçekten 2 Eyl
tarihli. Sunucu ÜZERİNDE CLI koşturan biri için tuzak olmaya devam
ediyor. Yalnızca K-092'nin ölçümüyle ilgisi yoktu.

### Taşınabilir ders

**Kapsamlı görünen bir olumsuzluk, arkasındaki sayım kadar iyidir.**
"Hiçbir ikili bunu yapamazdı" demek bütün ikilileri saymayı gerektirir
— ve sayılmayan yer, tasarımın ANA yoluydu.

`2>/dev/null`, doğrulanmamış bir yolda "yanlış yol"u "yok"a çevirir.
K-051'in ailesi: cevapsızlığı sonuç diye okumak. Yoklama önce
ölçebildiğini kanıtlamalıydı — `getent passwd panely-client` ev
dizinini tek komutta veriyordu.

**Bir suçlama da ölçümdür** ve aynı kurala tabidir. K-095 "ölç, iddia
etme" kuralını savunurken onu çiğnedi. Kural en çok, kişinin kendini
haklı bulduğu yerde gevşer.

---

## K-100 — README ölçülerek yeniden yazıldı; yürütücü günlüğü daemon'un dizininde duruyor

**Tarih:** 21 Eylül 2026
**Durum:** belge düzeltmesi + bir test; güvenlik bulgusu KAYDEDİLDİ,
düzeltmesi ayrı iş

README 8 Ağustos'tan beri değişmemişti. Yeniden yazmadan önce içindeki
her iddia canlı sunucuya ya da koda karşı ölçüldü.

### Yanlış çıkan iddialar

| README / SECURITY.md diyordu | ölçülen |
|---|---|
| panelyd kendi kayıtlarını düşürse `audit verify` farkı yakalar | **K-079'da geri çekilmişti** — `exec.proto` ve SECURITY.md düzeltildi, README 6 hafta boyunca vaat etmeye devam etti. Karşılaştırma kodu yok |
| `api.sock` grubu `panely` | `panely-client` |
| arm64 gerçek donanımda hiç koşmadı | 14 Eyl'den beri CI'da gerçek ARM'da koşuyor |
| Faz 1 "sürüyor" | canlıda |
| üç ikili | beş: `panely-caddy` hiç yoktu |
| `panely-connect` ~50 satır | 88 |
| "on dört gerçek hata" | kaynaksız sayı; kaldırıldı |
| masaüstü "asıl arayüz" | dört salt-okunur çağrı (`version`, `status`, `audit.list`, `audit.verify`) |
| SECURITY.md: "vault" sırları korur | vault YOK; env değerleri `panely.db`'de düz metin |
| CLI yardımı `app <create\|list\|show>` | `update` ve `delete` gizliydi |

Son satır bir testle kilitlendi: `TestUsageListsEverySubcommand`,
yardım metnindeki alt komut listesini dağıtıcının "bilinmeyen alt komut"
mesajındaki listeyle karşılaştırıyor. Düzeltmeden önce kırmızıya döndüğü
gözlendi (`["create" "list" "show"]` ≠ beş komut).

### Doğru ama YANLIŞ SEBEPLE geçen bir kontrol

README'nin güvenlik doğrulama listesi `ssh panely-client@sunucu docker ps`
için "BAŞARISIZ OLMALI" diyordu. Ölçüldü: **çıkış 0.** Zorlanmış komut
istenen komutu yok sayıyor, `api.sock`'a bağlanıyor ve geri gelen şey
gRPC'nin ayar çerçevesi (`\0\0\006\004…`). Güvenlik özelliği doğru,
kontrolün tarifi yanlıştı — "başarısız olmalı" diye okuyan biri çıkış
0'ı görünce bir delik sanırdı. Artık beklenen çıktı yazıyor.

`systemd-analyze security` için yazılı hedef `< 2.0` idi; `panely-exec`
**2.4** ölçüldü. Hedef yerine birim başına ölçülen değerler yazıldı.

### Kurulum bölümü TAZE KLONLA sınandı

README'de kurulum bölümü hiç yoktu. Yazılan adımlar, GitHub'dan yeni
alınmış bir klonda sırayla koşuldu:

```
buf generate OLMADAN go build   → no required module provides package
                                  .../internal/pb/panely/v1        (kontrol grubu)
buf generate + build-release.sh → 16 sn, dört sunucu ikilisi + panely
```

Üretilmiş kod depoda yok; kontrol grubu olmasa "klonla ve derle"
yazılırdı ve ilk okuyucunun ilk komutu düşerdi. `bootstrap`'ın kendisi
ise Ağustos başından beri taze bir sunucuda koşmadı — README bunu açıkça
söylüyor.

### 🔴 Güvenlik bulgusu: daemon, yürütücünün günlüğünü SİLEBİLİYOR

`exec-audit.log` `root:panely 0640` — daemon içeriğine yazamıyor. Ama
dosya `/var/lib/panely` içinde duruyor ve o dizin `panely:panely 0750`,
yapışkan bit yok. Bir dizine yazma yetkisi, içindeki dosyayı silme ve
yeniden adlandırma yetkisidir; dosyanın sahibi önemli değildir.

Gerçek günlüğe dokunmadan, aynı dizinde kök sahipli bir sınama
dosyasıyla ölçüldü:

```
panely → dosyaya ekleme      Permission denied     (kontrol: izin tutuyor)
panely → rm                  çıkış 0, dosya gitti
panely → aynı adla yeniden   yeni dosya, sahibi panely
```

Yürütücü günlüğü açılışta bir kez açıyor ve o tanımlayıcıya ekliyor
(`/proc/<pid>/fd/3`); `Journal.Read` ise dosyayı YOLDAN açıyor. Yani
ele geçirilen bir panelyd:

1. günlüğü silip yerine kendi zincirini koyabilir — zincirde gizli
   anahtar yok, hash'leri kendisi hesaplar;
2. yürütücü gerçek kayıtları artık adı olmayan inode'a yazmaya devam
   eder, `audit list/verify` sahte dosyayı okur;
3. yürütücü yeniden başladığında sahte zinciri devralır.

Bu SECURITY.md'nin 4. maddesi, yani **kapsam İÇİ**. "Daemon okuyabilir
ama yazamaz" iddiası dosya izni için doğruydu; dizin izni onu boşa
çıkarıyordu.

### Aynı sınıf: uzak yedek yükleyicisinin yapılandırması

`panely-offsite.service` `RCLONE_CONFIG` tanımlamıyor ve `panely`
kullanıcısının ev dizini `/var/lib/panely`. Yani rclone yapılandırması
yine daemon'un yazabildiği dizine düşer — ve `deploy/offsite/README.md`
kullanıcıya tam olarak orayı öneriyordu.

rclone yapılandırması komut çalıştırabilir: sunucudaki rclone 1.60.1'de
webdav arka ucunun `bearer_token_command` alanı belgelenmiş olarak var
(çalıştırılması ayrıca ölçülmedi). Daemon'un yazabildiği bir dosya böylece
**ağ gören** bir süreçte komuta dönüşür — K-098'in ayırmaya çalıştığı
şeyin tam tersi.

Bugün istismar edilemez: zamanlayıcı kapalı ve `rclone.conf` yok. Ama
kullanıcının sıradaki adımı tam olarak bu dosyayı oluşturmak. **Kullanıcı
yapılandırmadan önce kapatılmalı.**

### Ortak mekanizma ve taşınabilir ders

İki bulgu da aynı yanılgıdan: **bir dosyanın korunması yalnızca kendi
iznine değil, içinde durduğu dizinin iznine de bağlıdır.** `0640 root`
bir dosya, yazılabilir bir dizinde, sahibi olmayan tarafından silinebilir
ve yerine başkası konabilir.

Ayrıcalıklı bir sürecin okuduğu ya da yazdığı her yol, **o süreçten daha
az ayrıcalıklı biri tarafından yazılabilen bir dizinde durmamalı.**

---

## K-101 — Uzak yedek yapılandırması root'un dizinine sabitlendi

**Tarih:** 21 Eylül 2026
**Durum:** düzeltildi, canlıya kuruldu; zamanlayıcı hâlâ KAPALI

K-100'ün iki bulgusundan süresi daralan buydu: kullanıcının sıradaki
adımı `rclone config` çalıştırmak ve belge onu daemon'un dizinine
yönlendiriyordu. Bu yüzden yürütücü günlüğünden ÖNCE kapatıldı.

### Üç katman

1. **Birim:** `Environment=RCLONE_CONFIG=/etc/panely/rclone.conf`.
   `/etc/panely` root'un dizini (`0755 root:root`, Ağustos'tan beri
   `caddy.json` da orada).
2. **Betik:** yapılandırma dosyasının VE köke kadar her üst dizinin
   root'a ait olduğunu ve grup/diğerlerinin yazamadığını doğruluyor;
   değilse çalışmayı reddediyor. `-w` ile sınamak işe yaramazdı: birim
   `ProtectSystem=strict` ile koşuyor, bu ad alanında her şey salt
   okunur görünür. Tehdit bu sürecin değil, daemon'un yazabilmesi.
3. **Test:** `TestOffsiteRcloneConfigOutsideDaemonDirs` — birimin
   `RCLONE_CONFIG` yolunu `panelyd.service`'in `ReadWritePaths`'iyle
   karşılaştırıyor. İki elle mutasyonla sınandı: satır silinince ve yol
   `/var/lib/panely` altına taşınınca kırmızıya döndü.

### Canlıda dört durum

Betik `panely` olarak, geçici bir `offsite.conf` ile koşturuldu:

```
a) RCLONE_CONFIG tanımsız                     → red
b) /var/lib/panely altında, dosya ROOT 0640   → red (sahip=999, dizinde yakalandı)
c) KONTROL: /etc/panely, root:panely 0640     → korumayı GEÇTİ, rclone'a ulaştı
d) /etc/panely altında, dosya panely'nin      → red
```

(b) K-100'ün tam vakası: dosyanın sahibi root olduğu hâlde dizin
daemon'un. Kontrol grubu (c) olmasa korumanın her şeyi reddettiği
ihtimali açık kalırdı.

### İlk ölçüm GEÇERSİZDİ — iki kez

1. Betik `/root` altına kopyalanmıştı; `panely` okuyamadı ve dört durum
   da aynı "Permission denied" ile düştü. Aynı çıktı dört farklı
   beklenti için — ölçüm hiçbir şey ölçmüyordu, kontrol grubu bunu
   hemen gösterdi.
2. İkinci denemede dört durum yine aynı hatayı verdi, bu kez
   **sözdizimi**: `${RCLONE_CONFIG:?… daemon'un …}` içindeki kesme işareti
   bash'te tırnak açıyor ve betiğin tamamı ayrıştırılamıyor.

İkincisi yayına çıksaydı yükleyici her gece `failed` kalırdı ve bunu
bildiren hiçbir şey yok (K-098: arıza alarmı bağlı değil). CI hiçbir
kabuk betiğinin sözdizimini denetlemiyordu. Artık `lint` işi depodaki
her `*.sh` için `bash -n` koşuyor; bozuk sürümde çıkış 2 verdiği
gözlendi, 25 betiğin hepsi temiz.

### Kalan açık: aynı kullanıcı

Yükleyici ile daemon aynı kullanıcıyla (`panely`) koşuyor, yani daemon
`rclone.conf`'u OKUYABİLİR — değiştiremez. Sağlayıcı anahtarının silme
yetkisi olmaması (K-098) bu yüzden zorunlu. Ayrı bir kullanıcı daha
güçlü olurdu; bugünkü tehdit (komut çalıştırma) bunsuz kapandı.

Kalıntı temizliği: `/var/lib/panely/.config/rclone` — K-098
denemelerinden kalan boş bir dizin — silindi.

---

## K-102 — Executor denetim günlüğü root'un dizinine taşındı

**Tarih:** 21 Eylül 2026
**Durum:** düzeltildi, canlıda taşındı ve ölçüldü

K-100'ün ikinci bulgusu. Günlük `/var/lib/panely` (daemon'un dizini)
içinden `/var/lib/panely-exec`'e (`0700 root:root`) taşındı. Daemon artık
günlüğü okuyamıyor da — ihtiyacı yok: `panely audit` onu RPC ile
executor'dan alıyor, dosyadan değil. Bu da ölçüldü: taşımadan sonra
`audit verify` executor zincirini yine okudu.

### Değişenler

- `panely-tmpfiles.conf`: `d /var/lib/panely-exec 0700 root root`
- `panely-exec.service`: `--journal /var/lib/panely-exec/exec-audit.log`,
  `ReadWritePaths=/var/lib/panely-exec`. `/var/lib/panely` yalnızca
  hacimler için kaldı.
- `cmd/panely-exec`: varsayılan yol. Ayrıcalıklı yüzey 2498'de, değişmedi.
- `install.sh`: eski günlük varsa executor'ı durdurup taşıyor.
  **Taşımadan önce etkin ExecStart'a bakıyor**: bir drop-in eski yolu
  taşıyorsa durup söylüyor. Canlı sunucuda tam olarak böyle bir drop-in
  vardı (`10-allow-repo.conf`); yalnızca birim güncellenseydi günlük
  taşınır, executor eski yerde BOŞ bir zincir başlatır ve geçmiş sessizce
  kopardı. Kurulum sonrası iki yeni kontrol: `panely` günlük dizinine
  yazamıyor, çalışan executor günlüğü yeni yolda açık tutuyor.

### Testler — ve mutasyonun kendi zayıflığı

`TestExecutorJournalOutsideDaemonDirs` üç dosyayı birlikte okuyor
(executor birimi, daemon birimi, tmpfiles). `TestOwnedPathsAreActuallyCreated`
ise eski bir boşluğu kapatıyor: `panelyOwnedPaths` listesi "bunları biz
yaratıyoruz" diyerek öneksiz `ReadWritePaths`'e izin veriyordu ama bunu
kimse doğrulamıyordu.

Mutasyonlar yeni `scripts/mutate-units.sh`'ta (6/6). İlk koşuda biri
**KIRMIZI OLMADI** — ve kusur testte değil mutasyondaydı: `replace`
ilk eşleşmeyi aldı, o da birimin yorum bloğundaki örnek satırdı. Test
yorumları okumadığı için yeşil kaldı. Mutasyon satır başına
çapalanınca yakalandı. K-080'in "zayıf mutasyon" sebebi, bu kez
betiğin ilk koşusunda.

### Canlı taşıma

`panelyd` executor'ı `Requires=` ile istiyor; executor durunca o da durdu
(ölçüldü: `inactive`) ve ikisi birlikte başlatıldı. Taşıma boyunca site
yoklandı:

```
önce    daemon 131 · executor 166 kayıt, GEÇERLİ · md5 7fff2277…
sonra   daemon 132 · executor 166 kayıt, GEÇERLİ · md5 7fff2277…
site    200 yoklamanın 200'ü → 200
```

### Düzeltme, açığı ölçen yöntemle doğrulandı

Aynı deney iki dizinde — ikincisi kontrol grubu:

```
                                   /var/lib/panely-exec    /var/lib/panely
panely → root dosyasını sil        Permission denied       çıkış 0 (silindi)
panely → aynı adla sahte günlük    Permission denied       —
panely → dizini listele            Permission denied       —
```

Kontrol grubu olmasa "silemedi" sonucu, deneyin hiçbir şeyi
silemeyecek biçimde kurulmuş olmasından da gelebilirdi.

### Kapatılmayan

- **Taşınan günlüğün geçmişi doğrulanamaz.** 166 kayıt açık kaldığı
  süre boyunca değiştirilebilir bir yerdeydi. Zincir geçerli, ama
  gizli anahtarı olmayan bir hash zinciri, yazabilen biri tarafından
  baştan kurulabilir. Bugünkü `GEÇERLİ`, geçmişin sahici olduğunu değil
  yalnızca tutarlı olduğunu söyler.
- **İki zincir hâlâ karşılaştırılmıyor** (K-079).
- **`install.sh`'ın taşıma bloğu gerçek bir kurulumda koşmadı.** Canlı
  taşıma aynı adımları elle, sırayla yaptı; bloğun kendisi bir sonraki
  `bootstrap`'ta ya da taze bir sunucuda ilk kez koşacak.
- Executor günlüğü açarken grubunu `--owner-group` (panely) yapıyor.
  Dizin `0700` olduğu için bu artık etkisiz; ayrıcalıklı yüzeye
  dokunmamak için kod değiştirilmedi.

### K-079'un geri çektiği iddianın KALAN kopyaları

Düzeltmeden sonra "0640 root:panely" ve "karşılaştırır" arandı. K-079
iddiayı `exec.proto`'nun tasarım kurallarından ve SECURITY.md'den
çekmişti, ama iddia yedi yerde daha yaşıyordu — biri **ayrıcalıklı
kodun paket belgesinde** (`internal/exec/journal.go`: "VerifyAuditChain
iki zinciri karşılaştırır ve düşürülen her kayıt fark olarak ortaya
çıkar"), biri **kullanıcıya basılan bir mesajda**:

```
EXECUTOR ZİNCİRİ KIRIK. panelyd bu dosyaya yazamaz (0640 root:panely);
bozulmuşsa root yetkisi kullanılmış demektir.
```

K-102'den önce bu mesajın sonucu yanlıştı: panelyd günlüğü
değiştirebiliyordu. Artık doğru, ama gerekçesi dizin; mesaj buna göre
yazıldı ve disk bozulması ihtimali eklendi. `api.proto`, `exec.proto`
(iki yer), `api/server.go`, `api/record.go`, `cmd/panely/audit.go`,
bir test yorumu ve bir test hata iletisi de düzeltildi. Hepsi yorum ya
da metin; ayrıcalıklı yüzey 2498'de.

Ders K-079'un kendisiyle aynı: **bir iddia geri çekildiğinde bütün
kopyaları aranmalı.** K-079 iki kopyayı buldu, yedisini bıraktı.

---

## K-103 — api.sock reddi artık günlüğe yazılıyor

**Tarih:** 21 Eylül 2026
**Durum:** düzeltildi, canlıda ölçüldü

K-095'in bulduğu kusur: panelyd yetkisiz bir bağlantıyı doğru biçimde
reddediyordu ama journal'da tek satır yoktu. gRPC el sıkışma hatalarını
kendi günlükçüsüne yazıyor ve o günlükçü varsayılan olarak sessiz.
İstemci yalnızca `connection reset by peer` görüyordu.

Somut zararı: `usermod -aG panely-client` ile eklenen ikinci bir
yönetici (SO_PEERCRED yalnızca birincil grubu raporlar, SECURITY.md)
reddedilir ve sunucu tarafında sebebine dair hiçbir iz bulamazdı.

**Karar:** `internal/api/credentials.go`'daki sarmalayıcı her el sıkışma
hatasını `pid/uid/gid` ile WARN olarak yazıyor. `internal/peercred`
executor grafiğinde (yüzeyden yer yer, 2 satır kaldı); daemon tarafındaki
sarmalayıcı grafikte değil — yüzey 2498'de kaldı.

### Ölçüm

`TestRejectedCallerIsLogged` düzeltmeden önce kırmızı gözlendi;
`TestAcceptedCallerIsNotLoggedAsRejected` kontrol grubu. Canlıda
(`e093541`, `/proc/<pid>/exe` md5 ile doğrulandı):

```
root             → reddedildi   pid=… uid=0   gid=0
panely (soket sahibi) → reddedildi   pid=… uid=999 gid=988
panely-client    → "etkin alarm yok", ret satırı YOK
normal kullanım  → 4 SSH komutu, 4 giriş, 0 ret satırı
yeniden başlatma → 120 yoklamanın 120'si 200
```

Son satır gürültü sorusunun cevabı: her bağlantıda "reddedildi" yazan
bir günlük gerçek reddi boğardı. Soket dizini `0750
panely:panely-client` olduğu için oraya ulaşabilen herkes zaten yerel
ve ayrıcalıklı; satırları çoğaltarak journal'ı doldurma riski ihmal
edilebilir.

---

## K-104 — Uzak yedek hedefi olarak Cloudflare R2 hazırlandı

**Tarih:** 21 Eylül 2026
**Durum:** betik + belge; sunucuya KURULMADI, zamanlayıcı KAPALI

Kullanıcı Cloudflare'in ücretsiz hizmetlerini kullanmak istiyor. İlk
aday uzak yedek, çünkü K-098'in mekanizması hazır ve yalnızca bir hedef
bekliyor. Ölçülen: alan adı (`erkanrzgc.dev`) Vercel DNS'te, Cloudflare'de
DEĞİL. R2 alan adına bağlı olmadığı için hemen kullanılabilir; proxy,
WAF, Tunnel ve DNS-01 ise DNS taşımasını gerektirir (kullanıcının kararı).

### R2, K-098'in varsayımını kırıyor

K-098 "sağlayıcıda silme yetkisi verme" diyordu. R2'nin belgesi
(21 Eyl okundu) dört token izni sayıyor: Admin Read & Write, Admin Read,
Object Read & Write, Object Read. **Silmesiz yazma yok** — yazabilen her
token silebilir. Silmeyi durduracak şey kovadaki **bucket lock**
(önek başına saklama süresi).

Bunun iki sonucu var:

1. **Betiğin budaması kilitle çatışır.** Kilitli dosyayı silmeye
   çalışan budama her koşuda hata basardı. `OFFSITE_KEEP=0` bu işi
   GÖRMEZDİ: o değer "yerelde olmayan her şeyi sil" demek. Açık bir
   anahtar eklendi: `OFFSITE_PRUNE=evet|hayir` (varsayılan `evet`,
   bilinmeyen değer reddediliyor). `hayir` iken eskiyenleri R2'nin
   yaşam döngüsü kuralı siliyor.
2. **Kilidin token'a ağır bastığı BELGEDE YAZMIYOR.** Belge kilidin
   silmeyi ve üzerine yazmayı engellediğini söylüyor, ama Object Read &
   Write token'ına karşı da geçerli olduğunu açıkça söylemiyor. Kurulumda
   kontrol gruplu ölçülecek: kilitli önekte silme reddedilmeli, kilitsiz
   önekte aynı token'la silme BAŞARILI olmalı.

### Anahtar sunucuda ölçüldü

Yerel bir rclone hedefiyle, yerelde olmayan eski bir dosya konup betik
koşturuldu:

```
OFFSITE_PRUNE=evet   → "1 tanesi siliniyor", eski dosya SİLİNDİ   (kontrol)
OFFSITE_PRUNE=hayir  → "uzak budama KAPALI",  eski dosya DURUYOR
OFFSITE_PRUNE=belki  → HATA, çıkış 1
```

Şifreli bir yedeğin boyutu da ölçüldü: 143.592 bayt. Günde 24 × 90 gün
≈ 310 MB; ücretsiz katman 10 GB.

### Belgeye eklenen bedel

Kilit silmeyi durduruyor ama YAZMAYI durdurmuyor. Ele geçirilen bir
sunucu kovayı `panely-` önekli büyük dosyalarla doldurabilir; 10 GB'ın
üstü ücretli ve kilit o dosyaların da 30 gün silinmesini engelliyor.
Kilit süresi bu yüzden "fark et ve müdahale et" penceresi kadar
tutuldu (30 gün), yaşam döngüsü 90 gün.

### Koddan okunan, henüz ÖLÇÜLMEYEN bir boşluk

Yükleme döngüsü uzaktaki dosyayı yalnızca ADIYLA atlıyor
(`grep -qxF "$enc" "$remote_list"`). Bir yükleme yanlış boyutla kalırsa
o koşu doğru biçimde başarısız oluyor; ama sonraki koşu dosyayı "zaten
var" diye atlıyor ve yeşil bitiyor. Bozuk kopya kalıcı olur ve R2'de
kilit onu yeniden yazmayı da engeller. Ayrı iş; önce ölçülecek.
→ Ölçüldü ve düzeltildi, bkz. K-105.

---

## K-105 — Uzak yedek, uzaktaki kopyayı yalnızca adıyla doğru saymıyor

**Tarih:** 21 Eylül 2026
**Durum:** ölçüldü, düzeltildi, yeniden ölçüldü

K-104'te koddan okunan boşluk önce ölçüldü. Sunucuda, geçici bir yerel
rclone hedefiyle, gerçek yedekler yalnızca okunarak:

```
KONTROL  boş hedef                      → yüklendi=24, kurban 143.592 bayt
DENEY    kurbanın 100 baytlık KESİK      → atlandı=1, çıkış 0,
         kopyası önceden uzakta            kurban uzakta 100 bayt KALDI
```

Betik bozuk bir yedeği başarı olarak raporluyordu. K-098'in kendi
kuralı — "kesilmiş bir yedek olmayandan kötüdür" — yükleme anında
uygulanıyor ama bir sonraki koşuda ad kontrolüyle atlanıyordu.

### Düzeltme

Uzak liste artık boyutlarıyla alınıyor (`rclone lsf --format ps`).
Her yedek önce şifreleniyor, çünkü beklenen boyutu bilmenin tek
güvenilir yolu bu; sonra uzaktaki boyutla karşılaştırılıyor. Tutmazsa
"UZAK KOPYA BOZUK" yazılıp yeniden yükleniyor.

Bunun mümkün olduğu da ölçüldü: age çıktısının içeriği her seferinde
farklı ama BOYUTU sabit (aynı yedek üç kez → 3 × 143.592 bayt).

### Yeniden ölçüm — dört durum

```
A  boş hedef (kontrol)                  → yüklendi=24
B  ikinci koşu, aynı hedef              → yüklendi=0 atlandı=24
C  kesik kopya uzakta                   → BOZUK yakalandı, yeniden yüklendi,
                                          kurban 143.592 bayt
D  kesik kopya DEĞİŞTİRİLEMEZ           → yüklenemedi, başarısız=1, çıkış 1
   (chattr +i — kilit benzetimi)
```

B, K-098'in sonsuz döngü dersinin kontrolü: düzeltme, sağlam kopyaları
her koşuda yeniden yükleseydi aynı sınıf hata geri gelirdi. D, R2'deki
kilit durumunun benzetimi: bozuk kopya yeniden yazılamıyorsa en azından
sessiz kalmıyor.

### Sınır

AYNI boyutta bozulmuş bir kopya bu denetimden geçer. Şifreli içerik
her seferinde farklı olduğu için karşılaştırılacak bir hash yok. Bunu
yakalayan tek yol bir geri yükleme tatbikatı.

---

## K-106 — R2 kilidi ölçüldü; rclone'un yükleme sonrası HEAD'i R2'de 501

**Tarih:** 24 Eylül 2026
**Durum:** ölçüldü; `rclone.conf`'a `no_head = true` eklendi, belge güncellendi

Kullanıcı R2'yi kurdu: `panely-yedek` kovası (Standard), bucket lock
`panely-` 30 gün, yaşam döngüsü `panely-` 90 gün, token Object Read &
Write yalnız bu kovaya. `/etc/panely/rclone.conf` 640 root:panely.

### Kilit — K-104'ün açık bıraktığı soru

Cloudflare belgesi kilidin token iznine ağır bastığını yazmıyordu.
Aynı token, `panely` kullanıcısı, iki önek:

```
panely-kilit-testi-… (kilitli)   sil          → 409, dosya DURUYOR
                                 üzerine yaz  → 409, boyut değişmedi
kontrol-testi-…      (kilitsiz)  sil          → çıkış 0, SİLİNDİ
```

Kontrol grubu şart: yalnız 409 görülseydi "token'ın silme izni yok"
diye de okunabilirdi. Kilitsiz dosyanın aynı token'la silinmesi reddin
kilitten geldiğini kanıtlıyor. `panely-kilit-testi-…` 30 gün kovada
kalıyor (10 bayt), sonra yaşam döngüsü siliyor.

### Beklenmeyen bulgu: her yükleme "başarısız"

İki test yüklemesi de `501 Not Implemented` ile çıkış 1 verdi — ama
dosyalar kovadaydı. `--dump headers` ile istekler ayrıldı:

```
HEAD /…                  → 404   (var mı? yok — normal)
PUT  /…                  → 200   (yazıldı)
HEAD /…?versionId=…      → 501   (R2 desteklemiyor)
```

Kilitli kovada R2 PUT yanıtında `X-Amz-Version-Id` döndürüyor;
sunucudaki rclone 1.60.1 ardından o sürümü HEAD ile soruyor.
Düzeltilmeseydi betik her yüklemeyi `başarısız` sayar, birim her gece
`failed` kalırdı — ve arıza hâlâ kimseye bildirilmiyor (K-098).

`no_head = true` rclone'un yükleme sonrası HEAD'ini kapatıyor. Bu bir
doğrulamayı kaldırmak değil: betik yüklemeden sonra boyutu kendisi,
LİSTEleyerek ölçüyor (`rclone size --json`) ve o yol R2'de çalışıyor.
Ölçüm, 143.592 baytlık bir dosyayla:

```
KONTROL  ayarsız              → çıkış 1
DENEY    --s3-no-head          → çıkış 0, size=143.592 (yerel 143.592)
         rclone.conf'ta no_head → bayraksız çıkış 0, size doğru
```

Bütün test dosyaları silindi; kovada yalnızca kilit testi dosyası var.

rclone'u güncellemek sorunu başka yoldan çözebilirdi ama dışarıdan ikili
indirmek ayrı bir karar; yapılandırma satırı yeterli ve belgelendi.

---

## K-107 — Uzak yedek ilk kez AĞA çıktı: birim kendi DNS'ini kapatıyordu

**Tarih:** 24-25 Eylül 2026
**Durum:** düzeltildi, gerçek birimle R2'ye yüklendi, kullanıcının
makinesinde çözüldü; zamanlayıcı hâlâ KAPALI

`offsite.conf` yazıldı (`OFFSITE_PRUNE=hayir`) ve ilk yükleme elle
değil gerçek systemd birimiyle başlatıldı. Birim düştü:

```
lookup ….r2.cloudflarestorage.com on 127.0.0.53:53:
write udp 127.0.0.1:…->127.0.0.53:53: write: operation not permitted
```

`IPAddressDeny=localhost` (yükleyici ele geçirilirse host'un yerel
servislerini taramasın diye) systemd-resolved'ın 127.0.0.53'teki
çözücüsünü de kapatıyordu. rclone Go'nun kendi çözücüsünü kullanıyor ve
`/etc/resolv.conf`'taki adrese UDP ile gidiyor.

K-098 bu birimi "uçtan uca kanıtlandı" diye kaydetmişti; sınaması yerel
bir rclone hedefiyle yapıldığı için ağ yolu hiç yürünmemişti. K-098'e
bant eklendi. Doğru olan tek şey: birim hatayı SESSİZ geçmedi, `failed`
kaldı ve hiçbir şey yüklenmedi.

### Düzeltme önce GEÇİCİ birimle ölçüldü

Yüklü birime dokunmadan `systemd-run` ile aynı kısıtlar:

```
KONTROL  birimdeki kısıtlar aynen          → DNS düştü, çıkış 1
DENEY    + IPAddressAllow=127.0.0.53       → R2 listelendi, çıkış 0
YAN ETKİ izinle birlikte 127.0.0.1:22      → KAPALI
KONTROL  kısıtsız süreçten 127.0.0.1:22    → AÇIK
```

Son iki satır istisnanın yalnızca çözücüyü açtığını gösteriyor; kısıtsız
kontrol olmasa "KAPALI" sonucu, deneyin hiçbir şeye bağlanamayacak
biçimde kurulmuş olmasından da gelebilirdi.

(`systemd-run -p IPAddressDeny=localhost` bu sürümde "Failed to parse
IP address prefix" veriyor; ölçümde sembolik adlar açık öneklere
çevrildi. Birim dosyasında sembolik ad çalışıyor — `systemctl show`
çözülmüş hâlini gösterdi.)

### Kilitlendi

`TestOffsiteUploaderCanResolveNamesButNotReachLocalhost` iki yönü de
sınıyor: istisna yoksa ad çözülemez; istisna `localhost` ya da
`127.0.0.0/8` kadar genişse engelin amacı kalmaz. `mutate-units.sh`'a
üç mutasyon eklendi, 9/9 yakalandı.

### Gerçek koşu

```
1. koşu  yüklendi=24 atlandı=0 başarısız=0   birim success
         uzakta 24 × 143.592 bayt (+ K-106'nın 10 baytlık kilit testi)
2. koşu  yüklendi=0  atlandı=24 başarısız=0  (K-105: churn yok)
```

### Geri dönüş yolu — kullanıcının makinesinde

Bir yedek R2'den indirildi ve kullanıcının Windows makinesindeki özel
anahtarla çözüldü:

```
DENEY    doğru anahtar  → çıkış 0, "SQLite format 3", integrity_check=ok
                          md5 528aba4a… = sunucudaki orijinalin md5'i
KONTROL  yeni üretilmiş yanlış anahtar → "no identity matched", çıkış 1,
                          dosya oluşmadı
```

md5 eşitliği bayt bayt aynılık demek: şifreleme, yükleme, indirme ve
çözme zinciri hiçbir şey kaybetmiyor. Çözülen kopya (uygulama sırlarını
taşıyor) ölçümden hemen sonra silindi.

⚠ İlk yanlış-anahtar ölçümü "çıkış 0" gösterdi: komut PowerShell'de bir
boruya bağlanmıştı ve `$LASTEXITCODE` borunun son elemanından geldi.
Dosyanın oluşmaması gerçek kanıttı; ölçüm borusuz tekrarlandı ve çıkış
1 verdi. Eski bir ders yeniden: çıkış kodunu boruyla ölçme.

### Kalan

- Zamanlayıcı **KAPALI** — açmak kullanıcının kararı.
- Arıza bildirimi hâlâ yok: birim düşerse `failed` kalır, kimse haber
  almaz. Bu koşu bunun tam örneğiydi.

> Sonrası: zamanlayıcı kullanıcı onayıyla 24 Eyl'de açıldı; ilk
> otomatik koşu 25 Eyl 00:20 UTC'de `yüklendi=3 atlandı=21
> başarısız=0` ile bitti. Arıza bildirimi K-108'de bağlandı.

---

## K-108 — Alarm teslimatı: ayrı gönderici, journal'dan Telegram'a

**Tarih:** 25 Eylül 2026
**Durum:** kuruldu ve sandbox'ta ölçüldü; gerçek mesaj kullanıcının bot
kurulumunu bekliyor, zamanlayıcı KAPALI

K-092 alarm TESPİTİNİ getirdi, teslimatı kullanıcı kararına bıraktı:
panelyd `IPAddressDeny=any` taşıyor. K-098 "ayrı gönderici süreç"
desenini çalışır hâle getirdi. Bu kayıt onu alarmlara uyguluyor.

### Gönderici NE OKUYOR — karar

İki aday vardı:

| | okunan | anahtar kime açık |
|---|---|---|
| veritabanı | `panely.db` (0600 panely) | gönderici `panely` olarak koşmak zorunda → **daemon anahtarı okur** |
| **journal** | panelyd'nin `msg=ALARM` satırları | gönderici ayrı kullanıcı → daemon okuyamaz |

Journal seçildi. Kenar tetikleme zaten panelyd'de yapılıyor; journal'a
yalnızca GEÇİŞLER düşüyor, yani gönderici yeni bir karar vermiyor,
taşıyor. `/var/log/journal` var — kalıcı; imleç yeniden başlatmadan
sonra kaldığı yerden devam ediyor.

⚠ **Bedel:** `systemd-journal` grubu BÜTÜN sistem journal'ını okur.
Kabul edildi: sürecin dışarıyla tek teması Telegram API'si ve yanıtın
yalnızca HTTP kodu okunuyor. Uzak yedek aynı dengeyi kurmuştu (sırlı
yedekleri okuyan, ağ gören süreç).

### Ölçülen varsayımlar (sunucuda, geçici birimlerle)

```
DynamicUser + systemd-journal   → 8 ALARM satırı okunuyor
DynamicUser, grupsuz (kontrol)  → 0 — hata YOK, SESSİZCE boş
LoadCredential                  → birim anahtarı alıyor
panely kullanıcısı              → anahtar dosyasını OKUYAMIYOR
```

İkinci satır bir teste dönüştü: izinsiz journalctl hata vermiyor,
gönderici "gönderilecek bir şey yok" diye mutlu çıkardı.

```
journalctl --cursor-file  imleci satırları BASTIĞI ANDA ilerletiyor
```

Bu yüzden imlecin geçici bir kopyası ilerletiliyor ve yalnızca gönderim
başarılıysa kalıcı imlecin yerine konuyor (aynı dizinde, atomik).
Telegram'a ulaşılamazsa olay kaybolmuyor, bir sonraki koşuda yeniden
deneniyor. İlk koşu imleci "şimdi"ye koyuyor; geçmiş yağdırılmıyor.

```
dakikada bir koşu, sınırsız        → koşu başına 2 journal satırı
LogLevelMax=notice, başarılı koşu  → 0
LogLevelMax=notice, başarısız koşu → 2 (görünür kalıyor)
LogLevelMax=warning, başarısız     → 1 (bir satır YUTULUYOR — reddedildi)
```

### Uzak yedek arızası: OnFailure

Uzak yedeğin başarısızlığı panelyd'de ALARM üretmiyor. K-098'den beri
her kayıt "arıza kimseye bildirilmiyor" diyordu. `panely-offsite.service`
artık `OnFailure=panely-notify-failure@%n.service` taşıyor. Canlıda:

```
KONTROL  başarılı yedek koşusu            → bildirim birimi tetiklenmedi (0)
DENEY    bilerek bozuk koşu (geçici       → bildirim birimi TETİKLENDİ
         /run drop-in, sonra kaldırıldı)     (243/CREDENTIALS — anahtar
                                             dosyası henüz yok, beklenen)
```

### Sandbox, sahte anahtarla

Birimin kısıtlarıyla geçici bir süreçte, sahte bir anahtarla:

```
izle, ilk koşu   → "imleç şimdiye kondu", çıkış 0
izle, ikinci     → sessiz, çıkış 0
hata             → Telegram http=401 "Unauthorized", çıkış 1
çıktıda anahtar  → 0 kez
```

401, DNS + ağ + TLS yolunun sandbox içinden Telegram'a ulaştığını
kanıtlıyor; K-107'nin DNS dersi baştan uygulandığı için ilk denemede.
Anahtar URL'de ama argv'de değil: `curl -K -` onu standart girdiden
okuyor.

İlk sandbox denemesi 203 (EXEC) ile düştü: betik `/tmp`'ye konmuştu ve
`PrivateTmp` birime ayrı bir `/tmp` veriyor. Kurulum hatası, betik değil.

### Kilitlenenler

- `scripts/check-notify-format.sh` — biçimlendirici, 7 durum, CI'da.
  Elle dört mutasyonla sınandı (tırnaklı ayrıştırma kapatıldı, kapanış
  "uyarı" yapıldı, bilinmeyen durum "düzeldi" yapıldı, kaçışlı tırnak
  çözülmedi); dördü de kırmızı.
- `notifyunit_test.go` — anahtar daemon'a kapalı, journal grubu var,
  DNS istisnası yalnızca çözücü, IPv4+IPv6 açık, OnFailure bağlı,
  LogLevelMax=notice.
- `mutate-units.sh` — 10 yeni mutasyon, toplam 19/19.

### Kapatılmayan

- **Gerçek mesaj henüz gönderilmedi** — kullanıcı botu kurunca
  `panely-notify-failure@deneme.service` ile uçtan uca denenecek.
- **Göndericinin kendisi düşerse** kimse haber almaz. Tek sunucuda bunu
  yakalayacak bir şey yok; dışarıdan bir nabız gerekiyor (ör. Cloudflare
  Worker). Ayrı karar.
- **Çekirdek birimler (panelyd, executor, caddy) OnFailure taşımıyor.**
  `Restart=on-failure` ile varsayılan `RestartMode=normal`'da her
  yeniden başlatma OnFailure'ı tetikleyebilir; davranış ölçülmeden
  eklenmedi.
- `panely bootstrap` ne uzak yedeği ne göndericiyi kuruyor; ikisi de
  elle, belgedeki adımlarla.

### Kurulumda çıkan iki kusur (25 Eyl)

**1. Yapıştırılan anahtarın sonunda boşluk.** Kullanıcının dosyasında
değer 47 karakterdi, son karakter boşluk (değer gösterilmeden, biçim
maskelenerek ölçüldü). Biçim denetimi haklı olarak reddetti ama kullanıcı
sebebi göremedi. Aynı şey `rclone.conf`'ta da olmuştu. Ayrıştırıcı artık
baştaki/sondaki boşluğu ve CR'yi kırpıyor; ortadaki boşluk hâlâ
reddediliyor (kontrol grubu). Test: 5 durum; kırpma kapatılınca 3'ü
kırmızı.

**2. `LogLevelMax=notice` betiğin kendi satırlarını da susturuyordu.**
"İlk koşu" satırı journal'a düşmedi. Ölçüldü: düz satır kayboluyor,
`<5>`/`<3>` önekli satırlar kalıyor. Önek olmasa bir gönderim
başarısız olduğunda SEBEBİ görünmezdi. `log`/`die` artık journal'a
yazarken (`$JOURNAL_STREAM`) önek ekliyor; terminalde eklemiyor. Test:
iki durum; önek kapatılınca kırmızı.

### Uçtan uca — gerçek alarm, gerçek Telegram

Bot kuruldu (@erkanbahcebot), sohbet kimliği `sohbet-bul` ile bulundu,
deneme mesajı gerçek birimle (`panely-notify-failure@deneme`) gitti ve
kullanıcıya ulaştı. Zamanlayıcı açıldı. Tatbikat: yedek dizini `0500`,
panelyd yeniden başlatıldı; sonra geri alındı.

```
ALARM durum=acildi backup_failed:panely.db   → gönderildi, ≤34 sn
ALARM durum=kapandi                          → gönderildi
site                                          → 163/163 yoklama 200
panelyd otomatik yeniden başlatma             → 0
göndericinin başarısız koşusu                 → 0
son durum                                     → dizin 700, etkin alarm 0
```

Kullanıcı iki mesajı (KRİTİK, DÜZELDİ) Telegram'da ekran görüntüsüyle
teyit etti.

---

## K-109 — Dış nabız: gönderici kendi ölümünü bildiremez, Worker bildirir

**Tarih:** 25 Eylül 2026
**Durum:** CANLI — Worker yüklendi, zincir uçtan uca ve kontrol gruplu
ölçüldü (aşağıda "Canlı ölçüm")

K-108'in kapatmadığı satır: alarm göndericisi durursa ya da sunucu
tamamen kapanırsa, bunu bildirecek bir şey sunucuda kalmıyor. Tek
sunucuda çözümü yok; kontrol dışarıda olmak zorunda.

### Tasarım

- **Nabzı gönderici atıyor**, ikinci bir ağ birimi değil. Gönderici zaten
  dakikada bir koşuyor ve ağa çıkabiliyor. Nabız `izle` BAŞARIYLA
  bittikten sonra atıldığı için "sunucu açık" değil "alarm göndericisi
  çalışıyor" kanıtlanıyor — kapatılmak istenen boşluk tam olarak bu.
  Bedeli bağlılık: Telegram'a ulaşılamazsa nabız da kesilir (belgede).
- **Alarm YOKLUKTA çalıyor.** Worker'ın `/ping` yolu yalnızca zamanı
  yazıyor; karar 5 dakikada bir koşan zamanlanmış kontrolde. Alarm ping
  yolunda verilseydi ölü sunucu tam aranan durumda sessiz kalırdı.
- **Eşik 15 dakika**, ölçülen aralıklardan: gönderici 60 sn, nabız ~5 dk
  (seyreltilmiş), kontrol 5 dk. Daha dar bir eşik zamanlayıcı
  oynamasında yanlış alarm üretir.
- **Kenar tetikleme** Worker'da da: `var` / `yok` / `hic` durumları;
  mesaj yalnızca geçişte.

### Ücretsiz katman — belgeden okundu (25 Eyl)

```
Workers   günde 100.000 istek, hesap başına 5 zamanlanmış tetikleyici
KV        günde 1.000 yazma, 100.000 okuma
```

Gönderici dakikada bir koşuyor; her koşuda nabız atsaydı günde 1.440
yazma, sınırın üstü. Nabız 4 dakikadan taze bir nabız varsa atlanıyor:
günde ~300 yazma (canlıda ~320 ölçüldü, aşağıda).

### Kurallar ve testler

- Worker'ın kararı saf bir fonksiyon (`karar.js`), `node --test` ile
  12 durum; Worker'ın kendisi sahte KV ve sahte fetch ile 8 durum. CI'da.
- **Telegram'a ulaşılamazsa durum YAZILMIYOR** — yazılsaydı bir sonraki
  kontrol "zaten alarmda" deyip susardı ve alarm hiç ulaşmazdı.
- Ping anahtarı sabit zamanlı karşılaştırılıyor (iki tarafın SHA-256'sı
  alınıp `timingSafeEqual`). `PING_TOKEN` tanımsızsa hiçbir ping kabul
  edilmiyor (503).
- KV'den bozuk bir damga gelirse (NaN) "taze" sayılmıyor: NaN ile her
  karşılaştırma false döner ve alarm hiç çalmazdı.
- Gönderici tarafında nabız ayarı yarımsa (yalnız URL ya da yalnız
  anahtar, http, kısa anahtar) çalışma reddediliyor — 6 yeni test.

Elle mutasyonlar: karar (sınır `>`→`>=`, kenar tetikleme kaldırma, NaN'ı
taze sayma, ilk gözlemde mesaj) 4/4; Worker (gevşek anahtar
karşılaştırması, PING_TOKEN'sız kabul, durumu hiç yazmama, durumu
Telegram'dan ÖNCE yazma) 4/4.

⚠ Dördüncü Worker mutasyonunun ilk ölçümü GEÇERSİZDİ: satır içi Python,
bash'in `/tmp`'sini göremedi, mutasyon uygulanmadı ve "fail 2" bir
önceki mutasyondan kalmıştı. Yeniden, dosya yerinde değiştirilerek
yapıldı; tam ilgili test düştü. Bu projede tanıdık sınıf (K-096):
mutasyonun uygulandığı ölçülmeden sonucu okunmamalı.

### Canlı ölçüm (25 Eyl)

Saatler UTC. Telegram mesajları kontrol turundan 0–1 dk sonra düştü
(ekranda TR saati, UTC+3). Mesajları kullanıcı Telegram'da gördü;
sunucu tarafı ssh ile, Worker tarafı KV'den okundu.

Önce Worker'ın kapısı, dışarıdan:

```
GET  /                       404
POST /ping, anahtarsız       401
POST /ping, yanlış anahtar   401
```

Sonra zincir. Gönderici zamanlayıcısı elle durduruldu; geri açılması
önceden `systemd-run --on-calendar` ile zamanlandı; aradaki sürede
sunucuya elle müdahale edilmedi:

```
22:35     🟡 NABIZ HİÇ GELMEDİ   Worker yüklü, sunucu henüz nabız atmıyor
22:40     (mesaj yok)            aynı durum, TEKRAR ETMEDİ
22:43:42  ilk nabız              son-nabiz yazıldı (yalnızca HTTP 204'te yazılır)
22:44:11  panely-notify.timer durduruldu
22:45     ✅ NABIZ GERİ GELDİ
22:50     (mesaj yok)            nabız 6 dk bayat, eşiğin altında
22:55     (mesaj yok)            11 dk bayat, eşiğin altında
23:00     🔴 NABIZ YOK           "17 dakikadır haber alınamıyor"
23:05     (mesaj yok)            aynı durum, TEKRAR ETMEDİ
23:07:23  zamanlayıcı kendiliğinden geri açıldı
23:10     ✅ NABIZ GERİ GELDİ
```

- **Eşik karar veriyor, bayatlık tek başına değil:** 6 ve 11 dakikalık
  turlar sustu, 17 dakikalık tur çaldı.
- **Kenar tetikleme iki alarm durumunda da:** 🟡 (`hic`) ve 🔴 (`yok`)
  birer kez geldi; sonraki turlar sustu.
- **Gecikme:** son nabızdan alarma ~17 dk. Tasarım sınırı: eşik 15 dk +
  kontrol aralığı en çok 5 dk.
- **İki taraf aynı nabzı görüyor:** 23:11 nabzı KV'de `23:11:53.980`,
  sunucudaki dosyada `23:11:54`.
- **Kontrol grubu — gönderici çalışırken sessizlik:** 23:15, 23:20 ve
  23:25 turlarında mesaj gelmedi. Bir gece boyunca da (23:10 → ertesi
  gün 10:56, ~140 tur) tek mesaj gelmedi: kullanıcının Telegram ekranı,
  sunucuda 0 başarısız nabız ve 0 başarısız koşu, KV'de `durum=var`.
- **Nabız sıklığı:** 23:11:54 → 23:16:24 → 23:21:03, yani ~4,5 dk.
  Günde ~320 KV yazması; sınır 1000.

Canlıda ölçülMEYEN: Telegram'a ulaşılamadığında durumun yazılmaması —
yalnızca birim testi ve mutasyonla (yukarıda).

### Kalan

- Worker'ın kendisi durursa kimse haber almaz — zincirin son halkası.
  Cloudflare kesintisi ya da hesap sorunu sessiz kalır.
- Çekirdek servislerin çöküşü hâlâ bildirilmiyor; sıradaki iş (→ K-110).

## K-110 — Çekirdek servis çöküşü: OnFailure değil, journal olayı + cgroup

**Tarih:** 26 Eylül 2026
**Durum:** CANLI — kuruldu, gerçek bir çöküşle uçtan uca ölçüldü
(aşağıda "Canlı ölçüm")

panelyd kendi ölümünü bildiremez. panely-exec ve panely-caddy'nin
çöküşünü de kimse bildirmiyordu (K-108 yalnızca panelyd'nin alarmlarını
ve uzak yedeği kapsıyordu).

### Ölçüm önce: OnFailure= ne yapıyor? (systemd 255, `RestartMode=normal`)

Geçici birimler `/run/systemd/system` altında, çekirdek birimlerle aynı
yeniden başlatma ayarıyla (`Restart=on-failure`, `RestartSec=2s`,
varsayılan başlatma sınırı 10 sn'de 5). Çekirdek servislere dokunulmadı.

⚠ İlk koşu GEÇERSİZDİ: `/run` `noexec` bağlı, test betikleri hiç
çalışmadı (203/EXEC) ve her durum "çökme döngüsü" ölçtü. Başlangıç
satırı bunu gösteriyordu ama betik bakmıyordu. Betikler `/bin/sh` ile
çağrıldı ve "başlangıç `running` değilse dur" kapısı eklendi.

```
durum                        OnFailure   sonra
exit 2 (panik benzeri)       1 kez       systemd geri getirdi
SIGKILL (OOM benzeri)        1 kez       systemd geri getirdi
systemctl restart (kontrol)  0           —
systemctl stop (kontrol)     0           —
exit 0                       0           ÖLÜ KALDI — sessiz ölüm
çökme döngüsü, 40 sn         18 kez      döngü SÜRÜYOR (NRestarts=17)
OnFailure hedefi yok         —           birim yine normal yeniden başladı
```

- **Başlatma sınırı hiç dolmuyor.** Her yeniden başlatma ~2,25 sn;
  10 sn'lik pencereye 5 başlatma sığmıyor. Döngü sonsuz. Gerçek hayatta
  da oldu: journal'da panelyd 4 Ağustos 17:57–18:04 arasında **155 kez**
  `core-dump` ile çökmüş (ilk kurulumdaki seccomp SIGSYS). OnFailure'a
  bağlansaydı 2 sn'de bir Telegram mesajı giderdi.
- **`exit 0` sessiz.** `Restart=on-failure` yeniden başlatmıyor,
  OnFailure tetiklenmiyor.

İkisi de OnFailure'ı eledi.

### `systemctl show` de olmuyor

Göndericinin dakikalık turundan birimin durumunu okumak istendi:
`Transport endpoint is not connected`. Sertleştirme satırları tek tek
denendi — hepsi başarısız, **yalnızca `DynamicUser=yes` bile**.
`User=nobody` ile okunuyor. dbus 1.14.10; dbus.service journal'ına ret
satırı düşmüyor. **Sebep kesinleşmedi.** Göndericinin kimlik modelini
(K-108) değiştirip ona bir veri yolu istemcisi vermek yerine başka yol
seçildi.

### Tasarım: yeni yetki yok

- **Çöküş: systemd'nin "Failed with result" olayı**
  (`MESSAGE_ID=d9b373ed55a64feb8242e02dbe79a49c`, `UNIT_RESULT` alanı).
  Sandbox'lı göndericinin kopyasından, bilinen pencerelerle ölçüldü:
  exit 2 → 1, SIGKILL → 1, restart → 0, stop → 0, exit 0 → 0,
  döngü → 18. Birebir.
- **Çalışıyor mu: birimin cgroup'unda süreç var mı.** Sandbox'tan
  okunuyor, root'un gördüğüyle aynı.
- **Kenar tetikleme**, dört durum: `saglam`, `coktu`, `dongu`,
  `calismiyor`. Döngü iki mesaj üretiyor (🟠 ÇÖKTÜ, 🔴 ÇÖKME DÖNGÜSÜ),
  sürdükçe sessiz, bitince ✅. "Çalışmıyor" için iki tur şart:
  yükseltmedeki kısa yeniden başlatma alarm vermesin.
- **En az bir kez teslim**, `izle` ile aynı desen: imleç ve durum ancak
  gönderim başarılıysa ilerliyor.

Ölçümle bulunan iki tuzak:

- **Süzülmüş sorgu imleç YAZMIYOR.** `MESSAGE_ID` ile süzülen sorgu hiç
  eşleşme bulamazsa `--cursor-file` boş kalıyor (ölçüldü). Çöküş geçmişi
  olmayan bir sunucuda her koşu "ilk koşu" olurdu ve ilk çöküş, imleç
  "şimdi"ye konurken yutulurdu. İmleç birimlerin bütün akışında
  tutuluyor, olay sonra süzülüyor.
- **Sahte olay yazılabiliyor.** Sıradan bir süreç `UNIT=` ve
  `MESSAGE_ID=` alanlarını kendisi yazabiliyor (`logger --journald` ile
  ölçüldü). `-u panely-caddy` onu göstermiyor, ama sorgu panelyd'nin
  akışını da okuduğu için ele geçirilen panelyd caddy adına sahte
  çöküş yollayabilirdi. Yalnızca `_PID=1` kabul ediliyor; o alanı
  journald koyuyor.

Mesaj yalnızca systemd'nin alanlarını taşıyor. Servisin kendi çıktısı,
ör. bir panik mesajı, Telegram'a gitmiyor; içinde veri olabilir.

### Testler

`scripts/check-notify-format.sh` (CI): karar 13, ayıklayıcı 5 (satırlar
canlıdan birebir: gerçek, sahte, başka olay, servisin kendi satırı),
mesaj 6, sahte ortamda uçtan uca tur 8 (ilk koşu geçmişi göndermez,
gönderim başarısızsa durum ilerlemez).

Elle mutasyonlar 9/9 yakalandı. Her mutantın tam bir kez uygulandığı ve
`bash -n`'den geçtiği ölçüldü (K-096): iki tur kuralı → bir tur, döngüde
tekrar mesaj, `_PID=1` şartı yok, `MESSAGE_ID` şartı yok, gönderim hatası
yutuldu, ilk koşuda geçmiş okundu, cgroup hep dolu, çöküp kalkmayanın
sayacı başlamadı, düzelme mesajı yok.

Testin kendisinde bir boşluk bulundu: "henüz sessiz" testi mesaj dosyası
hiç yokken de geçiyordu. Sıkılaştırıldı.

### Canlı ölçüm (26 Eyl, UTC)

Kurulumdan önce canlıdaki betik ve birim, önceki commit'le md5 olarak
birebir aynıydı; eski hâlleri `/root/*.yedek-20260926T105947Z`.

```
10:59:48  kuruldu (md5 depoyla aynı)
11:01:53  ilk koşular: servis imleci + durum dosyası, üç birim "saglam",
          mesaj YOK (ilk koşu geçmişi göndermez), başarısız koşu 0
11:03:05  systemctl kill -s SIGKILL panelyd
          → PID 408441 → 545407, NRestarts=1, 4 sn içinde active
          → journal: "Failed with result 'signal'"
11:04:14  🟠 gönderildi (69 sn sonra), durum panelyd=coktu
11:05:24  ✅ gönderildi, durum panelyd=saglam
```

- Site, öldürme dahil 200 saniye boyunca **937/937 200**. Trafik
  caddy'den geçiyor; panelyd'nin ölümü uygulamaları etkilemiyor.
- Gönderici 11:03'ten sonra 0 başarısız koşu.
- Telegram (kullanıcının ekranı, TR saati): 14:04 "🟠 ÇÖKTÜ —
  panelyd.service: 1 kez (signal), systemd yeniden başlattı", 14:05
  "✅ TOPARLANDI — panelyd.service: son kontrolden beri çökmedi". Arada
  ve öncesinde (02:11'den beri) başka mesaj yok.
- `systemctl kill` "auxiliary processes" için `Invalid argument` uyarısı
  verdi; ana süreç yine öldü (PID değişti, NRestarts arttı).

Canlıda ölçülMEYEN: döngü, "çalışmıyor" (iki tur) ve sahte olay yolları —
yalnızca testler ve mutasyonla. caddy'ye bilerek dokunulmadı (trafik
keser).

### Kapsam dışı — açıkça

- **Askıda kalma.** Birimlerde `WatchdogSec` yok. Kilitlenen bir
  panelyd'nin süreci yaşıyor, cgroup'u dolu; "çalışıyor" görünür.
- Gecikme ~1 dakika (gönderici zamanlayıcısı).
- Başlatma sınırının hiç dolmaması çekirdek birimlerin bir özelliği.
  Değiştirmek (ör. `StartLimitIntervalSec`) ayrı karar; bu kayıt
  değiştirmiyor, yalnızca bildiriyor.
