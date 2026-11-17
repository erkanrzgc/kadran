package exec

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"testing"
)

func gitArchive(t *testing.T, entries ...*tar.Header) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		if err := tw.WriteHeader(entry); err != nil {
			t.Fatal(err)
		}
		if entry.Typeflag == tar.TypeReg && entry.Size > 0 {
			if _, err := tw.Write(bytes.Repeat([]byte("x"), int(entry.Size))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func goodGitArchive(t *testing.T) []byte {
	return gitArchive(t, &tar.Header{Name: "octocat-Hello-World-7fd1a60/", Typeflag: tar.TypeDir, Mode: 0o755}, &tar.Header{Name: "octocat-Hello-World-7fd1a60/Dockerfile", Typeflag: tar.TypeReg, Mode: 0o644, Size: 12})
}
func TestPrivateArchiveBoundsAndCorruption(t *testing.T) {
	archive := goodGitArchive(t)
	for _, tc := range []struct {
		name   string
		body   []byte
		limits archiveLimits
	}{
		{"compressed", archive, archiveLimits{int64(len(archive) - 1), 1 << 20, 100}},
		{"expanded", archive, archiveLimits{1 << 20, 600, 100}},
		{"entries", archive, archiveLimits{1 << 20, 1 << 20, 1}},
		{"truncated gzip", archive[:len(archive)-5], archiveLimits{1 << 20, 1 << 20, 100}},
		{"duplicate path", gitArchive(t, &tar.Header{Name: "root/file", Typeflag: tar.TypeReg}, &tar.Header{Name: "root/file", Typeflag: tar.TypeReg}), archiveLimits{1 << 20, 1 << 20, 100}},
		{"multiple roots", gitArchive(t, &tar.Header{Name: "root/file", Typeflag: tar.TypeReg}, &tar.Header{Name: "other/file", Typeflag: tar.TypeReg}), archiveLimits{1 << 20, 1 << 20, 100}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := rewritePrivateArchive(context.Background(), bytes.NewReader(tc.body), io.Discard, tc.limits); err == nil {
				t.Fatal("unbounded or corrupt archive accepted")
			}
		})
	}
}

func TestPrivateArchiveRejectsUnsafeTarEntries(t *testing.T) {
	for _, entry := range []*tar.Header{
		{Name: "root/../outside", Typeflag: tar.TypeReg}, {Name: "/root/outside", Typeflag: tar.TypeReg}, {Name: "root/C:\\outside", Typeflag: tar.TypeReg},
		{Name: "root/link", Typeflag: tar.TypeSymlink, Linkname: "../../etc/passwd"}, {Name: "root/link", Typeflag: tar.TypeLink, Linkname: "../../etc/passwd"}, {Name: "root/device", Typeflag: tar.TypeChar},
	} {
		t.Run(entry.Name, func(t *testing.T) {
			if err := rewritePrivateArchive(context.Background(), bytes.NewReader(gitArchive(t, entry)), io.Discard, defaultArchiveLimits); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

func TestRewriteRejectsCumulativeExpandedSize(t *testing.T) {
	var entries []*tar.Header
	entries = append(entries, &tar.Header{Name: "r/", Typeflag: tar.TypeDir, Mode: 0o755})
	for i := 0; i < 10; i++ {
		entries = append(entries, &tar.Header{Name: fmt.Sprintf("r/f%d", i), Typeflag: tar.TypeReg, Mode: 0o644, Size: 100})
	}
	// Her kayıt sınırın altında (100 < 600) ama toplam (1000) üstünde.
	limits := archiveLimits{1 << 20, 600, 100}
	err := rewritePrivateArchive(context.Background(), bytes.NewReader(gitArchive(t, entries...)), io.Discard, limits)
	if err == nil {
		t.Fatal("cumulative expansion accepted")
	}
}

// paxRecord, "<uzunluk> anahtar=değer\n" biçiminde bir PAX kaydıdır.
func paxRecord(k, v string) string {
	body := " " + k + "=" + v + "\n"
	n := len(body) + 1
	for len(fmt.Sprint(n))+len(body) != n {
		n = len(fmt.Sprint(n)) + len(body)
	}
	return fmt.Sprint(n) + body
}

// xHeaderBlock, 'x' (PAX genişletilmiş başlık) kaydını ham bayt olarak kurar.
// tar.Writer GNU.sparse.* kayıtlarını yazmıyor; bu yüzden sıradan bir başlık
// yazılır, tür baytı 'x' yapılır ve toplam yeniden hesaplanır.
func xHeaderBlock(t *testing.T, records string) []byte {
	t.Helper()
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	if err := tw.WriteHeader(&tar.Header{Name: "PaxHeader/x", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(records)), Format: tar.FormatUSTAR}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(records)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Flush(); err != nil {
		t.Fatal(err)
	}
	raw := b.Bytes()
	raw[156] = tar.TypeXHeader
	for i := 148; i < 156; i++ {
		raw[i] = ' '
	}
	sum := 0
	for _, c := range raw[:512] {
		sum += int(c)
	}
	copy(raw[148:156], fmt.Sprintf("%06o\x00 ", sum))
	return raw
}

// sparseArchive, her adı PAX 1.0 seyrek kaydı olarak yazar: gövde yalnız boş bir
// harita taşır, okuyucu ise h.Size'ı realsize olarak bildirir ve o kadar sıfır
// üretir.
func sparseArchive(t *testing.T, names []string, realSize int) []byte {
	t.Helper()
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	if err := tw.WriteHeader(&tar.Header{Name: "r/", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if err := tw.Flush(); err != nil {
			t.Fatal(err)
		}
		recs := paxRecord("GNU.sparse.major", "1") + paxRecord("GNU.sparse.minor", "0") +
			paxRecord("GNU.sparse.name", n) + paxRecord("GNU.sparse.realsize", fmt.Sprint(realSize))
		raw.Write(xHeaderBlock(t, recs))
		tw = tar.NewWriter(&raw)
		if err := tw.WriteHeader(&tar.Header{Name: n, Typeflag: tar.TypeReg, Mode: 0o644, Size: 512, Format: tar.FormatUSTAR}); err != nil {
			t.Fatal(err)
		}
		block := make([]byte, 512)
		copy(block, "0\n")
		if _, err := tw.Write(block); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Flush(); err != nil {
		t.Fatal(err)
	}
	raw.Write(make([]byte, 1024))
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	_, _ = gz.Write(raw.Bytes())
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestRewriteRejectsSparseExpansion(t *testing.T) {
	// Üç seyrek kayıt, her biri 600000 bayta açılır (tek tek 1 MiB sınırının
	// altında); toplam 1,8 MB sınırı aşar. Girdi birkaç KB'tır.
	archive := sparseArchive(t, []string{"r/a", "r/b", "r/c"}, 600000)
	limits := archiveLimits{1 << 20, 1 << 20, 100}
	var out bytes.Buffer
	err := rewritePrivateArchive(context.Background(), bytes.NewReader(archive), &out, limits)
	if err == nil {
		t.Fatal("sparse expansion accepted")
	}
	if int64(out.Len()) > limits.expanded+int64(limits.entries)*tarOverheadPerEntry {
		t.Fatalf("wrote %d bytes past the cap", out.Len())
	}
	// Karşı kontrol: tek kayıt sınırın altında, kabul edilmeli (fikstür geçerli).
	one := sparseArchive(t, []string{"r/a"}, 600000)
	if err := rewritePrivateArchive(context.Background(), bytes.NewReader(one), io.Discard, limits); err != nil {
		t.Fatalf("single sparse entry under the limit rejected: %v", err)
	}
}

// Toplam h.Size sınırı, capWriter payından (entries*2048) AYRI korunur:
// iki 600000'lik kayıt 1,2 MB üretir; bu expanded (1 MiB) sınırını aşar ama
// capWriter'ın 1 MiB + 200 KiB payının İÇİNDE kalır. Yalnız capWriter kalırsa
// arşiv kabul edilirdi.
func TestRewriteRejectsSparseTotalBelowWriterCap(t *testing.T) {
	archive := sparseArchive(t, []string{"r/a", "r/b"}, 600000)
	limits := archiveLimits{1 << 20, 1 << 20, 100}
	var out bytes.Buffer
	err := rewritePrivateArchive(context.Background(), bytes.NewReader(archive), &out, limits)
	if err != errArchiveTooLarge {
		t.Fatalf("err = %v, want errArchiveTooLarge", err)
	}
}

// capWriter, toplam h.Size denetiminin ARKASINDAKİ son sınırdır: seyrek
// kayıtlar o denetimde düşer, bu yüzden yeniden yazım üzerinden yalnız
// capWriter'ı düşüren bir arşiv kurulamaz. Hata yolu burada tek başına
// sınanır: sınırı aşan yazma reddedilir ve alttaki yazıcıya hiçbir bayt
// geçmez; sınıra tam oturan yazma geçer.
func TestCapWriterRejectsWritesBeyondItsBudget(t *testing.T) {
	var out bytes.Buffer
	c := &capWriter{w: &out, n: 10}
	if n, err := c.Write([]byte("123456")); n != 6 || err != nil {
		t.Fatalf("write within budget = %d, %v", n, err)
	}
	if n, err := c.Write([]byte("12345")); n != 0 || err != errArchiveTooLarge {
		t.Fatalf("write beyond budget = %d, %v; want 0, errArchiveTooLarge", n, err)
	}
	if out.String() != "123456" {
		t.Fatalf("over-budget bytes reached the writer: %q", out.String())
	}
	if n, err := c.Write([]byte("abcd")); n != 4 || err != nil {
		t.Fatalf("write that exactly fills the budget = %d, %v", n, err)
	}
	if _, err := c.Write([]byte("x")); err != errArchiveTooLarge {
		t.Fatalf("write after the budget is spent = %v", err)
	}
}
