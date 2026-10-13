# Özel GitHub deposundan derleme

Destek isteğe bağlıdır. Token masaüstüne, uygulama tanımına veya daemon'a verilmez. Root executor sabit GitHub API adresinden **tam commit SHA** arşivini alır, doğrular ve Docker'a kimlik bilgisi taşımayan yerel tar bağlamı gönderir. Yapılandırılmış token ile erişim başarısızsa anonim derlemeye düşmez. Token tanımlanmayan, izin listesindeki herkese açık depolar mevcut uzak Git yolunu kullanır.

## Sunucu yapılandırması

GitHub'da yalnız gereken depo için `Contents: read` izinli fine-grained token oluştur. [GitHub arşiv API'si](https://docs.github.com/en/rest/repos/contents#download-a-repository-archive-tar) bu izni ve özel depolar için kısa ömürlü indirme bağlantısını belgeler. Token'ı root dosyasına bir editörle koy; komut satırına, shell geçmişine veya Git deposuna yazma.

```sh
sudo install -d -m 0700 /etc/kadran/private-git
sudo install -m 0600 /dev/null /etc/kadran/private-git/credentials.json
sudoedit /etc/kadran/private-git/credentials.json
```

Dosya içeriği; örnek token'ı kendi token'ınla değiştir:

```json
{"repositories": [{"repository": "YOUR_ORG/YOUR_REPO", "token": "YOUR_FINE_GRAINED_TOKEN"}]}
```

Dosya root'a ait, normal dosya ve tam `0600` olmalı; üst dizinler root'a ait ve başkalarınca yazılamaz olmalı. Symlink kabul edilmez. `ProtectHome=yes` nedeniyle dosyayı `/root` altına koyma. Oluşturma komutları mevcut dosyayı boşaltır: yalnız ilk kurulumda kullan; sonrasında editörle değiştir.

`sudo systemctl edit kadran-exec.service` ile drop-in ekle. Mevcut özel bayrakların varsa bunları da yeni `ExecStart` içine taşı:

```ini
[Service]
ExecStart=
ExecStart=/usr/local/lib/kadran/kadran-exec --socket /run/kadran-exec/exec.sock --journal /var/lib/kadran-exec/exec-audit.log --allow-user kadran --owner-group kadran --allow-repo YOUR_ORG/YOUR_REPO --git-credentials /etc/kadran/private-git/credentials.json
RestrictAddressFamilies=
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
# Bulut meta veri servisi root executor'dan ERİŞİLEMEZ olmalı: GCP/AWS/Azure'da
# 169.254.169.254 örnek kimlik belirteci demektir. Link-local (IPv4 ve IPv6) ve
# AWS'nin IPv6 meta veri adresi kapatılır.
IPAddressDeny=169.254.0.0/16 fe80::/10 fd00:ec2::254/128
```

Kısıtı uygulamadan ÖNCE ölç: drop-in'i kaydetmeden, aynı kısıtla DNS çözümlemesinin hâlâ çalıştığını doğrula. `/etc/resolv.conf` dosyası meta veri sunucusunu (ör. GCP'de `169.254.169.254`) çözümleyici olarak gösteriyorsa kısıt DNS'i de keser; `systemd-resolved` ya da yerel bir çözümleyici kullan (kısıtta `127.0.0.0/8` kapatılmaz, böylece `127.0.0.53` çalışır).

```sh
sudo systemd-run --wait --pipe -p IPAddressDeny='169.254.0.0/16 fe80::/10' getent hosts api.github.com
```

Loopback ve özel ağ aralıkları (`127.0.0.0/8`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `100.64.0.0/10`) bu varsayılanda KAPATILMAZ: executor yalnızca `api.github.com` ve `codeload.github.com` ile konuşur, ama bulut çözümleyicileri (ör. AWS VPC DNS) çoğu zaman özel aralıkta durur. Kapatmak istersen, önce çözümleyici adresini `IPAddressAllow=` ile açıkça izin ver (Allow, Deny'ı geçer).

```sh
sudo systemctl daemon-reload
sudo systemctl restart kadran-exec
sudo systemctl status kadran-exec --no-pager
```

`--allow-repo` zorunludur; yalnız token'ın kapsamı yeterli değildir. Birden çok repo virgülle ayrılır, dosyada repo başına token bulunur. Varsayılan birim `AF_UNIX` kalır. Drop-in root executor'ın ağ erişimini genişletir; uygulama girdisi URL belirleyemez. DNS/HTTPS erişimi gerekir. Token dosyası yalnız açılışta okunur; rotasyondan sonra executor'ı yeniden başlat.

## Dağıtım ve sınırlar

Yerel güvenilir checkout'ta `git rev-parse HEAD` ile tam 40 haneli commit'i al. Masaüstü dağıtım kutusuna bunu yaz veya `kadran deploy -commit FULL_COMMIT_SHA APP` kullan. CLI bayrakları uygulama adından önce gelir. İş istasyonundaki anonim branch çözümleyicisi token'a erişmez; özel branch/tag adını otomatik çözmez.

Arşiv en fazla 128 MiB sıkıştırılmış, 512 MiB açılmış ve 100.000 girdidir; indirme iki dakikayla sınırlıdır. Tek redirect yalnız beklenen HTTPS `codeload.github.com` repo/commit yoluna kabul edilir. Kalıcı bearer token ikinci isteğe taşınmaz; kısa ömürlü imzalı URL loglanmaz. Arşiv üst klasörü kaldırılır; mutlak/geçişli yollar, tekrar eden yollar, symlink/hardlink ve aygıt girdileri reddedilir. Geçici tar `0600` dosyası build çağrısı bitince silinir.

Bu bir Git clone değildir: `.git` geçmişi ve submodule checkout yoktur. Git LFS içeriği [GitHub arşiv ayarına](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/managing-repository-settings/managing-git-lfs-objects-in-archives-of-your-repository) bağlıdır; ayrı LFS fetch yapılmaz. Symlink gerektiren kaynak ağacı desteklenmez. Dockerfile'ın başka özel paket indirmeleri için bu token kullanılmaz; build argümanları sır saklama yöntemi değildir.

Sentetik HTTP + gerçek Unix Docker API taşımasıyla token ayrımı, başarısız erişim, arşiv limitleri ve dosya izinleri sınandı. Gerçek özel GitHub token'ıyla canlı Docker/systemd dağıtımı yapılmadı; ilk kullanımda kontrollü bir deneme gerekir.
