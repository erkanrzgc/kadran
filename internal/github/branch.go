package github

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxBranchLen = 255
	// maxSHABody sha yanıtı için üst sınırdır (40 hane + boşluk payı).
	maxSHABody = 128
)

var errBadBranch = errors.New("github: invalid branch name")

// validBranch, git check-ref-format kurallarının güvenli bir alt kümesidir.
// Dönen ad yola güvenle girer (her bölüm ayrıca kaçırılır).
func validBranch(name string) error {
	if name == "" || len(name) > maxBranchLen || name == "@" || !utf8.ValidString(name) {
		return errBadBranch
	}
	if strings.HasPrefix(name, "-") || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.HasSuffix(name, ".") {
		return errBadBranch
	}
	if strings.Contains(name, "..") || strings.Contains(name, "//") || strings.Contains(name, "@{") {
		return errBadBranch
	}
	for _, r := range name {
		if r == ' ' || unicode.IsControl(r) || strings.ContainsRune("~^:?*[\\", r) {
			return errBadBranch
		}
	}
	for _, comp := range strings.Split(name, "/") {
		if strings.HasPrefix(comp, ".") || strings.HasSuffix(comp, ".lock") {
			return errBadBranch
		}
	}
	return nil
}

// branchPath her bölümü ayrı kaçırır; "/" ayırıcıları korunur.
func branchPath(owner, repo, branch string) string {
	segs := strings.Split(branch, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return "/repos/" + owner + "/" + repo + "/commits/" + strings.Join(segs, "/")
}

// ResolveBranch dalın tam commit özetini çözer. Yol yalnız doğrulanmış parçalardan kurulur.
func (s *Service) ResolveBranch(ctx context.Context, owner, repo, branch string) (string, error) {
	if err := validRepoRef(owner, repo); err != nil {
		return "", err
	}
	if err := validBranch(branch); err != nil {
		return "", err
	}
	path := branchPath(owner, repo, branch)
	var sha string
	err := s.withToken(ctx, func(token string) error {
		raw, err := s.c.getRaw(ctx, path, token, "application/vnd.github.sha", maxSHABody)
		if err != nil {
			return err
		}
		sha = strings.TrimSpace(string(raw))
		if !validSHA(sha) {
			return errBadSHA
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return sha, nil
}
