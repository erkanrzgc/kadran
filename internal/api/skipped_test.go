package api

import (
	"context"
	"errors"
	"testing"

	"github.com/erkanrzgc/kadran/internal/audit"
	"github.com/erkanrzgc/kadran/internal/deploy"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

func TestRolloutSkippedErrorSemantics(t *testing.T) {
	own := deploy.SkippedError{Result: deploy.Result{Skipped: map[string]string{"blog": "no upstream"}}}
	other := deploy.SkippedError{Result: deploy.Result{Skipped: map[string]string{"shop": "no upstream"}}}
	for _, tc := range []struct {
		name        string
		err         error
		wantFailure bool
	}{
		{"own route", own, true},
		{"unrelated route", other, false},
		{"unrelated joined with shutdown failure", errors.Join(other, deploy.DrainError{AppID: "blog", Err: errors.New("stop failed")}), true},
		{"unrelated joined with own route", errors.Join(other, own), true},
	} {
		for _, operation := range []string{"deploy", "rollback"} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				ro := &fakeRollout{}
				srv := newRollbackServer(t, ro)
				deployN(t, srv, 2)
				var err error
				if operation == "deploy" {
					ro.err = tc.err
					stream := newDeployStream(context.Background())
					err = srv.Deploy(&kadranv1.DeployRequest{AppId: "blog", CommitSha: apiSHA}, stream)
					if tc.wantFailure && stream.succeeded() != nil {
						t.Error("failed rollout sent DeploySucceeded")
					}
				} else {
					ro.rollbackErr = tc.err
					_, err = srv.Rollback(context.Background(), &kadranv1.RollbackRequest{AppId: "blog"})
				}
				if (err != nil) != tc.wantFailure {
					t.Errorf("failure = %v, want %v (err=%v)", err != nil, tc.wantFailure, err)
				}
				records := auditActions(t, srv.store)
				last := records[len(records)-1]
				if tc.wantFailure && last.Outcome != audit.OutcomeFailure {
					t.Errorf("failed rollout audit outcome = %v", last.Outcome)
				}
			})
		}
	}
}
