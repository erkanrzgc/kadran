// kadran-vault, kasa anahtarını üretir ve açık anahtarı yazdırır (K-123).
//
// Kurulum root olarak çağırıyor:
//
//	kadran-vault -key /var/lib/kadran-exec/vault.key
//
// Anahtar yoksa üretir (0600), varsa dokunmaz; iki durumda da stdout'a
// yalnız AÇIK anahtarı (`age1…`) yazar. Kurulum onu /etc/kadran/vault.pub'a
// koyuyor, daemon değerleri bununla mühürlüyor.
//
// Ayrı bir ikili, çünkü executor'ın her satırı ayrıcalıklı yüzeye sayılıyor
// (K-040) ve bu kod daemon'dan hiçbir girdi almadan, yalnız kurulumda
// koşuyor. kadrand'de değil: kadrand root koşmamalı.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/erkanrzgc/kadran/internal/vault/vaultkey"
	"github.com/erkanrzgc/kadran/internal/version"
)

func main() {
	key := flag.String("key", "/var/lib/kadran-exec/vault.key", "path of the vault key")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("kadran-vault %s (%s)\n", version.Version, version.Commit)
		return
	}
	if err := vaultkey.Init(*key, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "kadran-vault:", err)
		os.Exit(1)
	}
}
