//go:build !linux

package exec

import "errors"

func readPrivateGitFile(string) ([]byte, error) {
	return nil, errors.New("private Git credential files require the Linux executor")
}
