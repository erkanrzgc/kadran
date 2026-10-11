# Windows uygulama simgesi

`kadran.ico`, kullanıcının verdiği özgün `../public/kadran-logo.png` dosyasından üretilir. Özgün dosya değiştirilmez; yeni bir görsel çizilmez. Yalnız RGBA biçimine dönüşüm ve LANCZOS ile 16, 24, 32, 48, 64, 128 ve 256 piksel boyutlarına küçültme yapılır. ICO içindeki her kare saydamlığı koruyan PNG'dir.

Tekrar üretim: Python 3 ve Pillow 12.3.0 ile depo kökünden `python desktop/scripts/make-windows-icon.py` çalıştır. `icon-provenance.json`, özgün PNG ve üretilen ICO'nun SHA-256 özetlerini ve kullanılan kodlayıcıyı kaydeder. Testler dosya biçimini, boyutları, saydamlık kanalını ve bu özetleri doğrular.

ICO depoda saklandığı için normal masaüstü derlemesi veya paketlemesi Python/Pillow gerektirmez. `scripts/dist.mjs`, Windows paketleyicisine bu ICO dosyasını verir. macOS ICNS üretimi bu değişikliğin kapsamına dahil değildir.
