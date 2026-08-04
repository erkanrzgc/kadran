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
