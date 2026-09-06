package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// backupKeep, saklanacak göç-öncesi anlık görüntü sayısıdır.
//
// Üç yeterli: bir yedek en son göçten önceki hâli, üçü son üç göç turunu
// kapsıyor. Sınırsız biriktirmek diski doldururdu ve disk zaten bu
// projenin ölçülmemiş kaynağı.
const backupKeep = 3

// backupSuffix, göç-öncesi anlık görüntülerin ad ekidir.
const backupSuffix = ".pre-"

// snapshotBeforeMigrate, göçler uygulanmadan ÖNCE veritabanının tutarlı
// bir kopyasını alır.
//
// ── Neden var ───────────────────────────────────────────────────────
//
// Göçler ileri yönlüdür: hiçbirinin `.down.sql` karşılığı yok ve
// `0005_deployment_history.sql` yıkıcıdır (`DROP TABLE deployments`).
// Göçler `Open()` içinde, daemon hiçbir şey servis etmeden önce
// OTOMATİK uygulanıyor; başarısız olurlarsa systemd `Restart=on-failure`
// ile mutasyona uğramış bir veritabanının üzerinde çökme döngüsü kurar.
// Yedek olmadan bu durumdan çıkış YOKTUR.
//
// ── Neden `cp` değil `VACUUM INTO` ──────────────────────────────────
//
// Veritabanı WAL modunda. Ham dosya kopyası `-wal` ve `-shm`
// dosyalarını geride bırakır; kopyalanan `.db` en son yazmaları
// İÇERMEYEBİLİR ve sessizce eski bir duruma dönülür. `VACUUM INTO`
// motorun kendi anlık görüntüsünü alır ve TEK, tutarlı, açılmaya hazır
// bir dosya üretir.
//
// ── Neden başarısızlık ÖLÜMCÜL ──────────────────────────────────────
//
// Yedek alınamıyorsa göç de yapılmamalı. Operatör dolu diski
// düzeltebilir; kötü bir göçü geri alamaz. Açılmayı reddetmek, sessizce
// korumasız ilerlemekten iyidir — yüzey denetçisinin "ölçemiyorsak
// onaylamayız" kuralının aynısı.
func snapshotBeforeMigrate(
	ctx context.Context, db *sql.DB, path, version string,
) error {
	// Bellek veritabanının dosyası yok; testler buradan geçer.
	if path == ":memory:" || path == "" {
		return nil
	}

	dest := path + backupSuffix + strings.TrimSuffix(version, ".sql")

	// Zaten varsa DOKUNULMAZ. Var olan kopya ilk denemeden önceki hâli
	// taşıyor; üzerine yazmak, başarısız bir denemeden sonraki durumu
	// "yedek" diye saklamak olurdu.
	if _, err := os.Stat(dest); err == nil {
		return nil
	}

	// VACUUM INTO hedef dosyanın var OLMAMASINI ister; yukarıdaki kontrol
	// bunu zaten garantiliyor.
	//
	// Yol BAĞLI PARAMETRE olarak geçiyor, dizgeye gömülmüyor. İlk hâli
	// tek tırnakları elle kaçırıp birleştiriyordu ve gosec G202 verdi.
	// ÖLÇÜLDÜ: `VACUUM INTO ?` bu sürücüde bağlı parametreyi kabul ediyor
	// (küçük bir programla sınandı). Yani elle kaçırmaya da nolint'e de
	// gerek yoktu — enjeksiyon sınıfı tamamen ortadan kalkıyor.
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?",
		filepath.ToSlash(dest)); err != nil {
		return fmt.Errorf("göç öncesi yedek alınamadı (%s): %w", dest, err)
	}

	pruneBackups(path)
	return nil
}

// pruneBackups, en yeni backupKeep tanesi dışındaki anlık görüntüleri
// siler.
//
// Hata döndürmüyor: budama başarısız olsa bile yedek ALINDI ve göç
// güvenle ilerleyebilir. Burada başarısızlığı ölümcül saymak, disk
// dolduğunda daemon'u açılmaz yapardı — üstelik elimizde taze bir yedek
// varken.
func pruneBackups(path string) {
	matches, err := filepath.Glob(path + backupSuffix + "*")
	if err != nil || len(matches) <= backupKeep {
		return
	}
	// Göç adları sıfır dolgulu (0001, 0002…), dolayısıyla sözlük sırası
	// = kronolojik sıra.
	sort.Strings(matches)
	for _, old := range matches[:len(matches)-backupKeep] {
		_ = os.Remove(old)
	}
}
