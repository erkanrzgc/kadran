package gitref

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// FullSHA, tam commit özeti: 40 küçük onaltılık hane.
var FullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// ResolveCommit, dağıtılacak commit'i belirler. commit verilmişse yalnız
// biçimi denetlenir; verilmemişse dal (boşsa uygulamanın varsayılan dalı)
// deponun kendisinden çözülür. progress'e kısa bir ilerleme satırı yazılır;
// nil olabilir.
//
// ── Neden çözümü İSTEMCİ yapıyor? ──────────────────────────────────
//
// kadrand yapamaz. Gerekçe dilim 4c'de DEĞİŞTİ ama sonuç aynı kaldı:
// birim artık AF_INET açıyor (sağlık yoklaması için), ancak
// `IPAddressDeny=any` + `IPAddressAllow=172.16.0.0/12` yalnızca Docker'ın
// özel ağına izin veriyor. GitHub'a bağlanmak hâlâ imkânsız — gerçek
// sunucuda ölçüldü: 1.1.1.1'e bağlanma denemesi engelleniyor, aynı
// deneme cgroup dışında başarılı.
//
// Yani kontrol düzlemi süreci internete hiç çıkmıyor; bu bir eksiklik
// değil, en-az-yetkinin ölçülebilir hâli. `git ls-remote` karşılığı
// burada, iş istasyonunda koşar.
func ResolveCommit(
	ctx context.Context, rpc kadranv1.KadranServiceClient,
	appID, commit, branch string, progress io.Writer,
) (string, error) {
	if commit != "" {
		if !FullSHA.MatchString(commit) {
			return "", fmt.Errorf(
				"-commit must be a full 40-character lowercase hex sha (%q) — "+
					"short shas and branch names are refused: builds must be reproducible", commit)
		}
		return commit, nil
	}

	// Dal çözümü için deponun nerede olduğunu bilmek gerekiyor; onu
	// sunucudaki tanımdan okuyoruz. Böylece kullanıcı depoyu iki kez
	// yazmıyor ve `app create`'te kaydedilen tanım tek gerçek kaynak
	// olarak kalıyor.
	resp, err := rpc.GetApp(ctx, &kadranv1.GetAppRequest{AppId: appID})
	if status.Code(err) == codes.PermissionDenied {
		// Dağıtım anahtarı uygulama tanımını okuyamaz: ortam
		// değişkenlerinin değerlerini taşıyor (K-131). Dal çözülemez.
		return "", fmt.Errorf("this key cannot read the app definition, so the branch cannot be resolved: "+
			"pass the commit with -commit (in GitHub Actions -commit \"$GITHUB_SHA\"): %w", err)
	}
	if err != nil {
		return "", fmt.Errorf("could not get the app definition: %w", err)
	}
	spec := resp.GetApp().GetSpec()

	if branch == "" {
		branch = spec.GetGitBranch()
	}
	if branch == "" {
		return "", errors.New("could not determine the branch: pass -branch or -commit")
	}

	if progress != nil {
		fmt.Fprintf(progress, "resolving branch %[4]q of %[1]s/%[2]s/%[3]s…\n",
			spec.GetGitHost(), spec.GetGitOwner(), spec.GetGitRepo(), branch)
	}

	sha, err := ResolveBranch(ctx,
		spec.GetGitHost(), spec.GetGitOwner(), spec.GetGitRepo(), branch)
	if err != nil {
		return "", err
	}
	return sha, nil
}
