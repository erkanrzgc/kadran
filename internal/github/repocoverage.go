package github

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"google.golang.org/protobuf/proto"
)

// Decision, bir deponun derlemede hangi yoldan çekileceğidir. Sıfır değer
// DecisionNotConnected'dır: karar verilemezse asla "arşiv" anlamına gelmez.
type Decision int

const (
	DecisionNotConnected Decision = iota
	DecisionNotCovered
	DecisionArchive
)

const (
	pageSize = 100
	// maxRepoPages: kurulum başına en çok 10 sayfa = 1000 depo.
	maxRepoPages = 10
	// maxInstallPages kurulum listesi için sayfa sınırıdır.
	maxInstallPages = 10
	// coverageTTL, kapsam yenilemeleri arasındaki en kısa süredir.
	coverageTTL = time.Minute
	// refreshTimeout tek bir arka plan yenilemesinin üst süresidir.
	refreshTimeout = time.Minute
)

// coverage kurulum listesinin önbelleğidir; covMu ile korunur.
type coverage struct {
	loaded    bool // en az bir yenileme başarıyla bitti
	attempted bool
	attemptAt time.Time // son yenilemenin BİTTİĞİ an (başarılı ya da değil)
	stale     bool      // listedeki bir özel depo çekilemedi: sıradaki Decide yenileyebilir
	lastErr   error     // son yenilemenin hatası (başarıda nil)
	installs  []*kadranv1.GitHubInstallation
	// index küçük harfli full_name -> private.
	index map[string]bool
}

// refreshRun tek uçuşlu arka plan yenilemesinin tutamağıdır.
type refreshRun struct {
	done   chan struct{}
	cancel context.CancelFunc
}

type installationsPage struct {
	TotalCount    int `json:"total_count"`
	Installations []struct {
		ID      int64 `json:"id"`
		Account struct {
			Login string `json:"login"`
		} `json:"account"`
		RepositorySelection string `json:"repository_selection"`
	} `json:"installations"`
}

type reposPage struct {
	TotalCount   int `json:"total_count"`
	Repositories []struct {
		FullName string `json:"full_name"`
		Private  bool   `json:"private"`
	} `json:"repositories"`
}

func coverageKey(owner, repo string) string { return strings.ToLower(owner + "/" + repo) }

// throttledLocked son yenilemenin bitişinden bu yana coverageTTL geçmediyse true döner.
func (s *Service) throttledLocked() bool {
	if !s.cov.attempted {
		return false
	}
	d := s.now().Sub(s.cov.attemptAt)
	return d >= 0 && d < coverageTTL
}

// startRefreshLocked tek uçuşlu yenilemeyi başlatır (covMu tutulurken); sürüyorsa onu döner.
// Yenileme çağıranın ctx'inde DEĞİL, Service ömrüne bağlı ctx'te koşar; ağ covMu dışındadır.
func (s *Service) startRefreshLocked() *refreshRun {
	if s.refresh != nil {
		return s.refresh
	}
	if s.bgClosed {
		return nil
	}
	ctx, cancel := context.WithTimeout(s.baseCtx, refreshTimeout)
	rr := &refreshRun{done: make(chan struct{}), cancel: cancel}
	s.refresh = rr
	s.bg.Add(1)
	go s.runRefresh(ctx, rr, s.gen)
	return rr
}

func (s *Service) runRefresh(ctx context.Context, rr *refreshRun, gen uint64) {
	defer s.bg.Done()
	defer close(rr.done)
	defer rr.cancel()
	installs, index, err := s.fetchCoverage(ctx)
	s.commitRefresh(rr, gen, installs, index, err)
}

// commitRefresh sonucu önbelleğe yazar. Kuşak (gen) değiştiyse (Disconnect ya da yeni bağlantı)
// sonuç ve deneme zamanı atılır: bayat sonuç asla kurulmaz.
func (s *Service) commitRefresh(rr *refreshRun, gen uint64, installs []*kadranv1.GitHubInstallation, index map[string]bool, err error) {
	s.covMu.Lock()
	defer s.covMu.Unlock()
	if s.refresh == rr {
		s.refresh = nil
	}
	if gen != s.gen {
		return
	}
	s.cov.attempted, s.cov.attemptAt, s.cov.lastErr = true, s.now(), err
	if err != nil {
		return
	}
	s.cov.loaded, s.cov.stale = true, false
	s.cov.installs, s.cov.index = installs, index
}

// invalidateCoverage kuşağı artırır, önbelleği siler ve sürmekte olan yenilemeyi iptal edip
// bitmesini bekler (rewarm true ise yenisini başlatır, beklemez).
func (s *Service) invalidateCoverage(rewarm bool) {
	s.covMu.Lock()
	s.gen++
	old := s.refresh
	s.refresh = nil
	s.cov = coverage{}
	if rewarm {
		s.startRefreshLocked()
	}
	s.covMu.Unlock()
	if old != nil {
		old.cancel()
		if !rewarm {
			<-old.done
		}
	}
}

// markStaleIfListed, listede özel görünen bir depo çekilemediğinde önbelleği bayat işaretler.
func (s *Service) markStaleIfListed(owner, repo string) {
	s.covMu.Lock()
	defer s.covMu.Unlock()
	if d, ok := s.lookupLocked(owner, repo); ok && d == DecisionArchive {
		s.cov.stale = true
	}
}

func (s *Service) fetchCoverage(ctx context.Context) ([]*kadranv1.GitHubInstallation, map[string]bool, error) {
	var installs []*kadranv1.GitHubInstallation
	index := map[string]bool{}
	for p := 1; p <= maxInstallPages; p++ {
		var page installationsPage
		if err := s.getJSONAuth(ctx, "/user/installations?per_page=100&page="+strconv.Itoa(p), &page); err != nil {
			return nil, nil, err
		}
		for _, in := range page.Installations {
			inst, err := s.fetchOne(ctx, in.ID, in.Account.Login, in.RepositorySelection == "all", index)
			if err != nil {
				return nil, nil, err
			}
			if inst != nil {
				installs = append(installs, inst)
			}
		}
		if len(page.Installations) < pageSize {
			return installs, index, nil
		}
	}
	slog.Warn("github installation list truncated at the page cap")
	return installs, index, nil
}

// fetchOne geçerli bir kurulumun depolarını okur; geçersiz kurulumu atlar (nil döner).
func (s *Service) fetchOne(ctx context.Context, id int64, login string, all bool, index map[string]bool) (*kadranv1.GitHubInstallation, error) {
	if id <= 0 {
		return nil, nil
	}
	if !ownerPattern.MatchString(login) {
		slog.Warn("github installation skipped: unexpected account name")
		return nil, nil
	}
	inst, err := s.fetchInstallation(ctx, id, index)
	if err != nil {
		return nil, err
	}
	inst.Account, inst.AllRepositories = login, all
	return inst, nil
}

// fetchInstallation bir kurulumun depolarını (en çok 1000) okur ve index'e işler.
func (s *Service) fetchInstallation(ctx context.Context, id int64, index map[string]bool) (*kadranv1.GitHubInstallation, error) {
	inst := &kadranv1.GitHubInstallation{}
	base := "/user/installations/" + strconv.FormatInt(id, 10) + "/repositories?per_page=100&page="
	for p := 1; p <= maxRepoPages; p++ {
		var page reposPage
		if err := s.getJSONAuth(ctx, base+strconv.Itoa(p), &page); err != nil {
			return nil, err
		}
		for _, r := range page.Repositories {
			if !plausibleFullName(r.FullName) {
				continue
			}
			inst.Repositories = append(inst.Repositories, &kadranv1.GitHubRepository{FullName: r.FullName, Private: r.Private})
			index[strings.ToLower(r.FullName)] = r.Private
		}
		if len(page.Repositories) < pageSize {
			return inst, nil
		}
		if p == maxRepoPages {
			inst.Truncated = page.TotalCount == 0 || page.TotalCount > p*pageSize
		}
	}
	return inst, nil
}

// plausibleFullName owner/repo yarılarına arşiv yolundaki aynı desenleri uygular.
func plausibleFullName(n string) bool {
	owner, repo, ok := strings.Cut(n, "/")
	return ok && validRepoRef(owner, repo) == nil
}

// Decide yalnız kapsamı sorar; tarball isteği yapmaz. Hata, karardan ÖNCEDİR:
// çağıran herkese açık yola düşebilir. Önbellekte varsa hemen yanıtlar; yoksa tek uçuşlu
// arka plan yenilemesini (en çok dakikada bir) bekler; ctx biterse ctx.Err() döner.
func (s *Service) Decide(ctx context.Context, owner, repo string) (Decision, error) {
	if err := validRepoRef(owner, repo); err != nil {
		return DecisionNotConnected, err
	}
	if s.tk.Identity() == nil {
		return DecisionNotConnected, nil
	}
	s.covMu.Lock()
	d, hit := s.lookupLocked(owner, repo)
	if hit && !s.cov.stale {
		s.covMu.Unlock()
		return d, nil
	}
	rr := s.waitableLocked()
	if rr == nil {
		defer s.covMu.Unlock()
		return s.settledLocked(owner, repo)
	}
	s.covMu.Unlock()
	select {
	case <-rr.done:
	case <-ctx.Done():
		return DecisionNotConnected, ctx.Err()
	}
	s.covMu.Lock()
	defer s.covMu.Unlock()
	return s.settledLocked(owner, repo)
}

// waitableLocked sürmekte olan yenilemeyi, ya da kısıt izin veriyorsa yenisini döner.
func (s *Service) waitableLocked() *refreshRun {
	if s.refresh != nil {
		return s.refresh
	}
	if s.throttledLocked() {
		return nil
	}
	return s.startRefreshLocked()
}

// settledLocked, yenileme olmadan (ya da sonrasında) önbellekten karar verir.
func (s *Service) settledLocked(owner, repo string) (Decision, error) {
	if s.tk.Identity() == nil {
		return DecisionNotConnected, nil
	}
	if d, ok := s.lookupLocked(owner, repo); ok {
		return d, nil
	}
	if !s.cov.loaded && s.cov.lastErr != nil {
		if errors.Is(s.cov.lastErr, ErrNotConnected) || errors.Is(s.cov.lastErr, ErrReconnect) {
			return DecisionNotConnected, nil
		}
		return DecisionNotConnected, s.cov.lastErr
	}
	return DecisionNotCovered, nil
}

func (s *Service) lookupLocked(owner, repo string) (Decision, bool) {
	private, listed := s.cov.index[coverageKey(owner, repo)]
	switch {
	case !listed:
		return DecisionNotConnected, false
	case private:
		return DecisionArchive, true
	}
	return DecisionNotCovered, true
}

// cachedInstallations önbellekteki kurulumların kopyasını döner; bir dakikadan eskiyse
// arka plan yenilemesi başlatır ama BEKLEMEZ.
func (s *Service) cachedInstallations() []*kadranv1.GitHubInstallation {
	s.covMu.Lock()
	defer s.covMu.Unlock()
	if s.refresh == nil && !s.throttledLocked() {
		s.startRefreshLocked()
	}
	out := make([]*kadranv1.GitHubInstallation, len(s.cov.installs))
	for i, in := range s.cov.installs {
		out[i] = proto.Clone(in).(*kadranv1.GitHubInstallation)
	}
	return out
}
