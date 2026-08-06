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
