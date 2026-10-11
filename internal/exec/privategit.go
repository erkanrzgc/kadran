package exec

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// PrivateGit owns credentials inside the executor. It accepts only configured
// GitHub repositories and immutable commits, and yields a credential-free tar.
type PrivateGit struct {
	client  *http.Client
	tokens  map[string]string
	allowed []string
	limits  archiveLimits
}
type archiveLimits struct {
	compressed, expanded int64
	entries              int
}

var privateTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func (p *PrivateGit) hasCredential(src *kadranv1.GitSource) bool {
	return src.GetHost() == DefaultGitHost && p.tokens[strings.ToLower(src.GetOwner()+"/"+src.GetRepo())] != ""
}

func LoadPrivateGitCredentials(file string, allowed []string) (*PrivateGit, error) {
	if file == "" {
		return nil, nil
	}
	data, err := readPrivateGitFile(file)
	if err != nil {
		return nil, err
	}
	return parsePrivateGit(data, allowed)
}

func parsePrivateGit(data []byte, allowed []string) (*PrivateGit, error) {
	if len(allowed) == 0 || len(data) > 64<<10 {
		return nil, errors.New("private Git requires an explicit repository allowlist and a bounded credential file")
	}
	var cfg struct {
		Repositories []struct {
			Repository string `json:"repository"`
			Token      string `json:"token"`
		} `json:"repositories"`
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&cfg) != nil || d.Decode(&struct{}{}) != io.EOF || len(cfg.Repositories) == 0 || len(cfg.Repositories) > 100 {
		return nil, errors.New("invalid private Git credential configuration")
	}
	p := &PrivateGit{tokens: map[string]string{}, allowed: append([]string(nil), allowed...), limits: archiveLimits{128 << 20, 512 << 20, 100000}}
	for _, r := range cfg.Repositories {
		owner, repo, ok := strings.Cut(r.Repository, "/")
		key := strings.ToLower(r.Repository)
		if !ok || validatePathSegment("owner", owner) != nil || validatePathSegment("repo", repo) != nil || !repoAllowed(owner, repo, allowed) || len(r.Token) > 4096 || !privateTokenPattern.MatchString(r.Token) || p.tokens[key] != "" {
			return nil, errors.New("invalid, duplicate or unlisted private Git credential entry")
		}
		p.tokens[key] = r.Token
	}
	p.client = &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second}}
	return p, nil
}

func (p *PrivateGit) Context(ctx context.Context, src *kadranv1.GitSource) (_ *os.File, err error) {
	if err := validateGitSource(src, []string{DefaultGitHost}, p.allowed); err != nil {
		return nil, err
	}
	token := p.tokens[strings.ToLower(src.GetOwner()+"/"+src.GetRepo())]
	if token == "" {
		return nil, errors.New("private Git repository has no configured credential; public fallback is disabled")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	u := "https://api.github.com/repos/" + src.GetOwner() + "/" + src.GetRepo() + "/tarball/" + src.GetCommitSha()
	resp, err := p.get(ctx, u, token)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusFound {
		location := resp.Header.Get("Location")
		_ = resp.Body.Close()
		if !validArchiveRedirect(location, src) {
			return nil, errors.New("private Git archive redirect rejected")
		}
		// The only URL credential accepted is GitHub's short-lived signed
		// codeload query. Never forward the permanent API bearer credential.
		resp, err = p.get(ctx, location, "")
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("private Git archive request failed (HTTP %d)", resp.StatusCode)
	}
	if resp.ContentLength > p.limits.compressed {
		return nil, errors.New("private Git compressed archive exceeds limit")
	}
	f, err := os.CreateTemp("", "kadran-context-*.tar")
	if err != nil {
		return nil, errors.New("could not create private build context")
	}
	defer func() {
		if err != nil {
			closePrivateContext(f)
		}
	}()
	if err = rewritePrivateArchive(ctx, resp.Body, f, p.limits); err != nil {
		return nil, err
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return nil, errors.New("could not rewind private build context")
	}
	return f, nil
}

func (p *PrivateGit) get(ctx context.Context, u, token string) (*http.Response, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// URL errors and server bodies may contain a bearer or signed query.
		return nil, errors.New("private Git archive transport failed")
	}
	return resp, nil
}

func validArchiveRedirect(raw string, src *kadranv1.GitSource) bool {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 8192 || u.Scheme != "https" || u.Hostname() != "codeload.github.com" || (u.Port() != "" && u.Port() != "443") || u.User != nil || u.Fragment != "" || u.RawPath != "" || u.Opaque != "" {
		return false
	}
	expected := "/" + src.GetOwner() + "/" + src.GetRepo() + "/legacy.tar.gz/" + src.GetCommitSha()
	if !strings.EqualFold(u.Path, expected) {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	return err == nil && (len(q) == 0 || (len(q) == 1 && len(q["token"]) == 1 && q.Get("token") != ""))
}

func rewritePrivateArchive(ctx context.Context, r io.Reader, w io.Writer, limits archiveLimits) error {
	compressed := &io.LimitedReader{R: r, N: limits.compressed + 1}
	gz, err := gzip.NewReader(compressed)
	if err != nil {
		return errors.New("private Git archive is not valid gzip")
	}
	defer gz.Close()
	expanded := &io.LimitedReader{R: gz, N: limits.expanded + 1}
	tr, tw := tar.NewReader(expanded), tar.NewWriter(w)
	root, seen, count := "", map[string]bool{}, 0
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.New("private Git archive is corrupt or exceeds expanded limit")
		}
		count++
		name := strings.TrimSuffix(h.Name, "/")
		first, relative, _ := strings.Cut(name, "/")
		if count > limits.entries || path.IsAbs(name) || path.Clean(name) != name || strings.ContainsAny(name, "\\:") || first == "." || first == ".." || (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir) || h.Size < 0 || h.Size > limits.expanded {
			return errors.New("unsafe or oversized private Git archive entry")
		}
		if root == "" {
			root = first
		}
		if first != root {
			return errors.New("private Git archive has multiple roots")
		}
		if relative == "" {
			if h.Typeflag != tar.TypeDir {
				return errors.New("private Git archive has no root directory")
			}
			continue
		}
		if seen[relative] {
			return errors.New("private Git archive has duplicate paths")
		}
		seen[relative] = true
		if err := tw.WriteHeader(&tar.Header{Name: relative, Mode: h.Mode & 0o777, Typeflag: h.Typeflag, Size: h.Size, ModTime: h.ModTime}); err != nil {
			return errors.New("could not write private build context")
		}
		if _, err := io.CopyN(tw, tr, h.Size); err != nil {
			return errors.New("private Git archive body is corrupt or context storage failed")
		}
	}
	// Validate gzip checksum/truncation and bound padding after tar's EOF.
	if _, err := io.Copy(io.Discard, expanded); err != nil || expanded.N <= 0 || compressed.N <= 0 || len(seen) == 0 {
		return errors.New("private Git archive is corrupt, empty or oversized")
	}
	if err := tw.Close(); err != nil {
		return errors.New("could not complete private build context")
	}
	return nil
}
func closePrivateContext(f *os.File) {
	if f != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}
}
