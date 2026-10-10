package api

import "github.com/erkanrzgc/kadran/internal/deploy"

// unrelatedSkippedRoutes accepts only an error tree made entirely of skipped
// routes for other apps. errors.As alone finds one warning and would conceal
// our own skipped route or a joined drain/transport failure.
func unrelatedSkippedRoutes(err error, appID string) (deploy.Result, bool) {
	out := deploy.Result{Skipped: map[string]string{}}
	var visit func(error) bool
	visit = func(err error) bool {
		switch e := err.(type) {
		case deploy.SkippedError:
			if len(e.Result.Skipped) == 0 {
				return false
			}
			for id, why := range e.Result.Skipped {
				if id == appID {
					return false
				}
				out.Skipped[id] = why
			}
			return true
		case *deploy.SkippedError:
			return e != nil && visit(*e)
		case interface{ Unwrap() []error }:
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
		case interface{ Unwrap() error }:
			return visit(e.Unwrap())
		default:
			return false
		}
	}
	return out, visit(err)
}
