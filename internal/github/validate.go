package github

// ValidRepoRef owner ve repo adının güvenli olup olmadığını söyler. RPC
// katmanı (kadran-git) istekleri Service'e vermeden önce bununla eler.
func ValidRepoRef(owner, repo string) bool { return validRepoRef(owner, repo) == nil }

// ValidSHA tam (40 hane, küçük harf) commit özetini doğrular.
func ValidSHA(sha string) bool { return validSHA(sha) }

// ValidBranch dal adının güvenli olup olmadığını söyler.
func ValidBranch(name string) bool { return validBranch(name) == nil }
