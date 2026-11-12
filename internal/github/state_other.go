//go:build !linux

package github

import (
	"io"
	"os"
)

// Bu dosya YALNIZ geliştirme ve testler içindir: kadran-git yalnız Linux için
// yayınlanır, bu yüzden Linux dışında sahiplik, kip ve dizin denetimi yoktur.

// readStateFile en çok 64 KiB'lık dosyayı sınırlı okur.
func readStateFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxStateFile+1))
	if err != nil || len(data) > maxStateFile {
		return nil, errStateTooLarge
	}
	return data, nil
}

// syncDir Linux dışında yapılacak bir şey değildir.
func syncDir(string) error { return nil }
