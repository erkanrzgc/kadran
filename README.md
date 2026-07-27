# Panely

Tek veya çok sunuculu Linux altyapısını yöneten, **masaüstü + CLI** tabanlı bir PaaS kontrol düzlemi.

> **Durum:** Faz 0 (temel) geliştiriliyor. Henüz üretime hazır değil.

---

## Neden bir tane daha panel?

Coolify, Dokploy, CapRover ve benzerleri işlerini iyi yapıyor. Panely'nin varlık sebebi tek bir tasarım kararı:

**Panelin kendisi sunucuda ayrıcalıklı çalışmaz.**

Mevcut panellerin tamamı kendilerine Docker soketini verir. Docker soketine erişim pratikte root yetkisidir — panelde bulunan herhangi bir uzaktan kod çalıştırma açığı, doğrudan sunucunun tamamının ele geçirilmesi demektir. Panely bu yüzden ayrıcalıklı yüzeyi, şema doğrulamalı ve denetlenebilir küçük bir binary'ye hapseder.

---

## Mimari

```
┌─ İŞ İSTASYONU ────────────────┐        ┌─ SUNUCU ──────────────────────────────────┐
│                               │        │                                           │
│  Electron GUI                 │        │  panelyd          kullanıcı: panely       │
│      ↕ stdio JSON-RPC         │        │    • iş mantığı, zamanlayıcı, SQLite      │
│  panely (Go CLI / sidecar) ───┼──SSH───┼──► • api.sock (0660, grup panely)         │
│                               │        │    • Docker'a ERİŞEMEZ                    │
└───────────────────────────────┘        │          ↕ exec.sock — tipli RPC          │
                                         │  panely-exec      kullanıcı: root         │
                                         │    • yalnızca beyaz listeli şemalar       │
                                         │    • değişmezleri zorlar                  │
                                         │    • Docker + dosya sistemi               │
                                         └───────────────────────────────────────────┘
```

### Üç binary, üç yetki seviyesi

| Binary | Kullanıcı | Yetki | Sorumluluk |
|---|---|---|---|
| `panelyd` | `panely` | Yetkisiz. Docker grubunda değil, yetenek (capability) yok | İş mantığı, SQLite, zamanlayıcı, sağlık denetçisi, denetim günlüğü |
| `panely-exec` | `root` | Ayrıcalıklı, ama yalnızca tipli şemaları kabul eder | Docker Engine API, kısıtlı dosya sistemi yazma |
| `panely` | (iş istasyonu) | — | CLI ve Electron için sidecar |

### Neden bu ayrım gerçek?

`panelyd` ele geçirilse bile `--privileged` konteyner çalıştıramaz, host dizinini bağlayamaz veya rastgele komut koşamaz. Bunlar protokolde **temsil edilemez** — reddedilen bir istek değil, ifade edilemeyen bir istek.

Executor'ın uyguladığı değişmezler:

- `Privileged` derleme zamanında `false`; `CapAdd` daima boş; host ağı ve host PID yasak
- `SecurityOpt` zorunlu olarak `no-new-privileges` + Docker varsayılan seccomp profili
- **Bind-mount'ta host yolu kabul edilmez.** RPC yalnızca uygulama kapsamlı bir *hacim adı* alır; yolu executor kendisi kurar. Yol doğrulamak TOCTOU'ya açıktır — girdiyi hiç almamak sınıfın tamamını siler
- Dosya yazma `openat2` + `RESOLVE_BENEATH` ile sembolik bağ kaçışına kapalı
- Serbest argv alan hiçbir RPC yok. Hiçbir yerde `sh -c` yok
- Çağıran süreç `SO_PEERCRED` ile doğrulanır — soket izinlerine güvenilmez

---

## Açık port yok

| Yüzey | Dinlediği yer |
|---|---|
| panelyd API | `/run/panely/api.sock` (unix soketi) |
| Executor | `/run/panely/exec.sock` (unix soketi) |
| Caddy admin | `/run/caddy/admin.sock` (unix soketi) |
| GUI ↔ sidecar | stdio (süreç boruları) |

Kontrol düzlemine ulaşmanın tek yolu **sshd**. Bu, şartnamedeki "gerçek IP'yi gizle" gereksinimini bedavaya karşılar: gizlenecek bir port yoktur.

### İstemcinin SSH kimliği

`panely bootstrap root@sunucu` **tek seferlik kurulum** komutudur. Günlük kullanımda istemci asla root olarak bağlanmaz — bağlansaydı operatörün kendi kabuğu doğrudan `docker run --privileged` çalıştırabilirdi ve executor ayrımı dekoratif kalırdı.

Bootstrap ayrı, yetkisiz bir SSH kullanıcısı oluşturur ve anahtarını **zorlanmış komuta** (forced command) bağlar:

```
command="/usr/local/lib/panely/panely-connect",restrict ssh-ed25519 AAAA... panely-client
```

`restrict` her şeyi kapatır: pty yok, port yönlendirme yok, ajan yönlendirme yok, X11 yok, user-rc yok. Anahtar yalnızca `panely-connect` binary'sini çalıştırabilir; o da `api.sock`'a bağlanıp stdin/stdout arasında bayt taşımaktan başka bir şey yapmaz.

> **Tasarım notu:** Önceki taslakta `authorized_keys` ile unix soket yönlendirmesine (`direct-streamlocal`) izin verilmesi düşünülmüştü. Zorlanmış komut hem daha basit hem daha sıkı: soket yönlendirmesini açmak `port-forwarding` iznini gerektirir, bu da istemciye sunucudaki **her TCP portuna** tünel açma yetkisi verirdi. `restrict` + zorlanmış komut ile bu sınıf tamamen kapanıyor. Faz 7'deki veritabanı tünelleri, SSH yönlendirmesi yerine denetim günlüğüne yazılan bir RPC üzerinden geçecek.

---

## Depo yapısı

```
proto/panely/v1/     Tek sözleşme kaynağı (api, exec, agent)
cmd/panelyd/         Sunucu daemon'ı
cmd/panely-exec/     Ayrıcalıklı executor — kasıtlı olarak küçük tutulur
cmd/panely-connect/  Zorlanmış komut stdio proxy'si (~50 satır)
cmd/panely/          İş istasyonu CLI + `panely sidecar`
internal/            Uygulama paketleri
desktop/             Electron + React
deploy/              systemd unit dosyaları, kurulum varlıkları
docs/                Mimari kayıtları
```

---

## Geliştirme

Gereksinimler: Go 1.24+, Node 20+, `buf`, Linux hedefi için Docker.

```bash
go test ./...
go build ./...
buf generate            # proto/ değiştiğinde
```

### Güvenlik doğrulaması

Bu komutlar **her fazda** çalıştırılır. İlk üçü başarısız olmak zorundadır:

```bash
sudo -u panely docker ps                    # BAŞARISIZ OLMALI
ssh panely-client@sunucu docker ps          # BAŞARISIZ OLMALI
ssh panely-client@sunucu                    # KABUK VERİLMEMELİ
systemd-analyze security panelyd            # hedef: exposure < 2.0
go test ./internal/exec -run TestInvariant  # kaçış denemeleri reddedilmeli
```

---

## Yol haritası

| Faz | Kapsam | Durum |
|---|---|---|
| 0 | Temel: proto, depo, denetim zinciri, executor iskeleti, SSH taşıma, bootstrap, Electron kabuğu | 🔨 devam ediyor |
| 1 | Çekirdek PaaS: derleme, blue-green dağıtım, Caddy, canlı log, sağlık denetçisi, geri alma | ⏳ |
| 2 | Cloudflare (DNS/WAF/DNS-01), sır kasası, tek tıkla servisler, hacimler, TOTP | ⏳ |
| 3 | Metrikler, alarmlar, PTY köprüsü, dosya yöneticisi, editör | ⏳ |
| 4 | Webhook alıcısı, push'ta dağıtım, cron yöneticisi | ⏳ |
| 5 | R2 yedekleme, Litestream, sıcak yedek düğüm, DNS failover | ⏳ |
| 6 | Çok düğüm: `panelyd --mode=agent`, mTLS gRPC | ⏳ |
| 7 | Octópus entegrasyonu (yerel siber güvenlik LLM'i) | ⏳ |

Ayrıntılı şartname: [`panely-master-spec.md`](panely-master-spec.md)

---

## Lisans

Henüz belirlenmedi.
