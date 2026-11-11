# Windows uygulama simgesi

`kadran-icon.svg`, seçilen KΛDRΛN yazı logosunun Bahnschrift Light SemiCondensed fontundaki K harfidir (`wght=300`, `wdth=87.5`). Harf path olarak saklanır; uygulama font dosyasına ihtiyaç duymaz. Fontun özgün oranı korunur: 256 × 256 saydam tuval üzerinde ortalanmış harf yaklaşık 121 × 186 pikseldir. `#5B96BA` rengi açık ve koyu tema zeminlerinde görünürlük sağlar.

`../public/kadran-icon.png`, bu SVG'nin Electron/Chromium ile saydam RGBA PNG olarak çizilmiş halidir. Electron artık bağımlılık değil (K-165): SVG değişirse PNG, saydamlığı koruyan herhangi bir Chromium ile (Edge ya da Chrome'un başsız kipi) aynı tuval boyutunda yeniden çizilmeli; çizim `docs/branding/render-wordmark.cjs`'in yaptığıyla aynı olmalı.


`kadran.ico`, bu PNG'den yalnız RGBA biçimine dönüşüm ve LANCZOS ile 16, 24, 32, 48, 64, 128 ve 256 piksel boyutlarına küçültme yapılarak üretilir. Kaynak PNG değiştirilmez. ICO içindeki her kare saydamlığı koruyan PNG'dir.

ICO'yu tekrar üretmek için Python 3 ve Pillow 12.2.0 ile depo kökünden `python desktop/scripts/make-windows-icon.py` çalıştır. `icon-provenance.json`, SVG, PNG ve ICO'nun SHA-256 özetlerini, font eksenlerini ve kullanılan kodlayıcıyı kaydeder. `node --test desktop/test/packaging-icon.test.ts` dosya biçimini, boyutları, saydamlık kanalını ve bu özetleri doğrular.

ICO depoda saklandığı için normal masaüstü derlemesi veya paketlemesi Python/Pillow gerektirmez. ICO ve `kadran.manifest`, `../rsrc_windows_amd64.syso` nesnesine gömülür ve `go build` onu ikiliye bağlar; ICO ya da manifest değişince `KADRAN_UPDATE_SYSO=1 go test ./internal/winres` ile yenilenir. macOS ICNS üretimi bu değişikliğin kapsamına dahil değildir.
