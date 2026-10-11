package api

import (
	"errors"

	"github.com/erkanrzgc/kadran/internal/deploy"
)

// unrelatedSkippedRoutes accepts only an error tree made entirely of skipped
// routes for other apps. errors.As alone finds one warning and would conceal
// our own skipped route or a joined drain/transport failure.
func unrelatedSkippedRoutes(err error, appID string) (deploy.Result, bool) {
	out := deploy.Result{Skipped: map[string]string{}}
	var visit func(error) bool
	visit = func(err error) bool {
		// Inspect every branch before matching a leaf; errors.As on the
		// joined parent would accept one warning and hide a sibling failure.
		if e, ok := err.(interface{ Unwrap() []error }); ok {
			children := e.Unwrap()
			if len(children) == 0 {
				return false
			}
			for _, child := range children {
				if !visit(child) {
					return false
				}
			}
			return true
		}
		if e, ok := err.(interface{ Unwrap() error }); ok {
			return visit(e.Unwrap())
		}
		var skipped deploy.SkippedError
		if !errors.As(err, &skipped) {
			var pointer *deploy.SkippedError
			if !errors.As(err, &pointer) || pointer == nil {
				return false
			}
			skipped = *pointer
		}
		if len(skipped.Result.Skipped) == 0 {
			return false
		}
		for id, why := range skipped.Result.Skipped {
			if id == appID {
				return false
			}
			out.Skipped[id] = why
		}
		return true
	}
	return out, visit(err)
}
