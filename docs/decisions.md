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