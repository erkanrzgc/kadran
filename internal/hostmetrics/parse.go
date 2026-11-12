// Package hostmetrics, hostun kaynak sayaçlarını /proc ve statfs'ten okur
// (K-166).
//
// # Neden kadrand'de, executor'da değil?
//
// Okunan her dosya herhangi bir yerel kullanıcının okuyabildiği bir dosya:
// root gerekmiyor. Ayrıcalıklı yüzeye (K-002) bir satır eklemek yerine
// yetkisiz süreç okuyor; bunun için kadrand.service'ten yalnız
// `ProcSubset=pid` kalktı, `ProtectProc=invisible` duruyor (başka
// kullanıcıların süreçleri yine görünmez).
//
// # Neden kümülatif sayaç, hız değil?
//
// Hız iki ölçümün farkı. Farkı sunucu tutsaydı durum tutardı ve iki
// istemci aynı anda baktığında birbirinin aralığını bozardı. Sunucu ham
// sayacı ve ölçüm anını döner; hızı istemci hesaplar.
package hostmetrics

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

// sectorBytes: /proc/diskstats sektörü her zaman 512 bayt (çekirdek
// belgesi), diskin fiziksel sektöründen bağımsız.
const sectorBytes = 512

// cpuTimes, /proc/stat'ın toplam `cpu` satırı.
type cpuTimes struct {
	total, idle, iowait, steal uint64
	cores                      uint32
}

// parseStat: ilk sekiz sütun user nice system idle iowait irq softirq
// steal. guest ve guest_nice zaten user'ın İÇİNDE sayılıyor; eklemek
// sanal makine çalıştıran hostta toplamı şişirirdi.
func parseStat(r io.Reader) (cpuTimes, bool) {
	var out cpuTimes
	found := false
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		switch {
		case fields[0] == "cpu":
			if len(fields) < 9 {
				return cpuTimes{}, false
			}
			var v [8]uint64
			for i := range v {
				n, err := strconv.ParseUint(fields[i+1], 10, 64)
				if err != nil {
					return cpuTimes{}, false
				}
				v[i] = n
				out.total += n
			}
			out.idle, out.iowait, out.steal = v[3]+v[4], v[4], v[7]
			found = true
		case strings.HasPrefix(fields[0], "cpu"):
			out.cores++
		}
	}
	return out, found
}

// parseMeminfo, "Ad:  1234 kB" satırlarını bayta çevirir.
func parseMeminfo(r io.Reader) map[string]uint64 {
	out := map[string]uint64{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		key, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		n, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		if len(fields) > 1 && fields[1] == "kB" {
			n *= 1024
		}
		out[key] = n
	}
	return out
}

// parseLoadavg: "0.52 0.38 0.30 3/412 98765". Dördüncü alanın paydası
// çekirdeğin zamanladığı toplam görev (iş parçacıkları dahil).
func parseLoadavg(s string) (l1, l5, l15 float64, tasks uint32, ok bool) {
	fields := strings.Fields(s)
	if len(fields) < 4 {
		return 0, 0, 0, 0, false
	}
	var loads [3]float64
	for i := range loads {
		v, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return 0, 0, 0, 0, false
		}
		loads[i] = v
	}
	_, total, found := strings.Cut(fields[3], "/")
	if !found {
		return 0, 0, 0, 0, false
	}
	n, err := strconv.ParseUint(total, 10, 32)
	if err != nil {
		return 0, 0, 0, 0, false
	}
	return loads[0], loads[1], loads[2], uint32(n), true
}

// virtualInterface: hostun dışarıyla trafiği fiziksel arayüzden geçiyor.
// Konteynerin veth'i ve Docker köprüleri aynı baytı ikinci kez sayardı.
func virtualInterface(name string) bool {
	return name == "lo" || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "br-")
}

// parseNetDev, fiziksel arayüzlerin alınan ve gönderilen baytlarını toplar.
// İlk iki satır başlık. Ad ile sayaç arasında boşluk olmayabilir
// ("vethabc123:13000"), bu yüzden ':'den bölünür.
func parseNetDev(r io.Reader) (rx, tx uint64, ok bool) {
	sc := bufio.NewScanner(r)
	for line := 0; sc.Scan(); line++ {
		if line < 2 {
			continue
		}
		name, rest, found := strings.Cut(sc.Text(), ":")
		if !found {
			continue
		}
		ok = true
		if virtualInterface(strings.TrimSpace(name)) {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 9 {
			continue
		}
		in, err1 := strconv.ParseUint(fields[0], 10, 64)
		out, err2 := strconv.ParseUint(fields[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		rx += in
		tx += out
	}
	return rx, tx, ok
}

// parseDiskstats, isDisk'in kabul ettiği aygıtların okunan ve yazılan
// baytlarını toplar. Bölümler ve aygıt eşleyici (dm-*) dışarıda kalmalı:
// aynı G/Ç'yi altlarındaki diskle birlikte iki kez sayarlardı.
func parseDiskstats(r io.Reader, isDisk func(string) bool) (read, written uint64, ok bool) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 10 {
			continue
		}
		ok = true
		if !isDisk(fields[2]) {
			continue
		}
		rs, err1 := strconv.ParseUint(fields[5], 10, 64)
		ws, err2 := strconv.ParseUint(fields[9], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		read += rs * sectorBytes
		written += ws * sectorBytes
	}
	return read, written, ok
}

// parsePressure, PSI dosyasının "some avg10" değeri: son 10 saniyede en
// az bir görevin bu kaynağı bekleyerek geçirdiği zamanın yüzdesi.
func parsePressure(r io.Reader) (float64, bool) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || fields[0] != "some" {
			continue
		}
		v, found := strings.CutPrefix(fields[1], "avg10=")
		if !found {
			return 0, false
		}
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	}
	return 0, false
}

// parseUptime, /proc/uptime'ın ilk alanı (saniye, kesirli) tam saniyeye.
func parseUptime(s string) (uint64, bool) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0, false
	}
	whole, _, _ := strings.Cut(fields[0], ".")
	n, err := strconv.ParseUint(whole, 10, 64)
	return n, err == nil
}
