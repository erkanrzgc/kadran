package gitclient

import (
	"errors"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Sizinti metni: hicbir sekilde cikisa girmemeli.
const leak = "ghu_SECRET https://objects.example/signed?sig=abc"

func TestMapErrorTable(t *testing.T) {
	cases := []struct {
		in       codes.Code
		wantCode codes.Code
		wantText string
	}{
		{codes.Unavailable, codes.FailedPrecondition, "the kadran-git service is not running"},
		{codes.DeadlineExceeded, codes.Unavailable, "the kadran-git service did not answer"},
		{codes.Unimplemented, codes.FailedPrecondition, "upgrade the server"},
		{codes.NotFound, codes.NotFound, "repository or branch not found (renamed or moved on GitHub? update the app's repository URL)"},
		{codes.PermissionDenied, codes.PermissionDenied, "lacks access"},
		{codes.FailedPrecondition, codes.FailedPrecondition, "needs attention"},
		{codes.ResourceExhausted, codes.ResourceExhausted, "rate limit"},
		{codes.InvalidArgument, codes.InvalidArgument, "invalid repository or branch"},
		{codes.Canceled, codes.Canceled, "canceled"},
		{codes.Internal, codes.Internal, "kadran-git failed"},
		{codes.Unknown, codes.Unavailable, "GitHub request failed, retry later"},
	}
	for _, c := range cases {
		got := mapError(status.Error(c.in, leak))
		if status.Code(got) != c.wantCode || !strings.Contains(got.Error(), c.wantText) {
			t.Errorf("%v -> %v", c.in, got)
		}
		if strings.Contains(got.Error(), "ghu_") || strings.Contains(got.Error(), "https://") {
			t.Errorf("%v: metin iletildi: %v", c.in, got)
		}
	}
	// Durum olmayan hata (baglanti kurulamadi): calismiyor sayilir.
	if got := mapError(errors.New(leak)); status.Code(got) != codes.FailedPrecondition ||
		strings.Contains(got.Error(), "ghu_") {
		t.Errorf("durumsuz hata -> %v", got)
	}
}

// kadran-git ayakta ama GitHub istegi basarisiz (ornek: gercek GitHub'in 422'si
// eskiden genel hataya dusuyordu): kod Unknown gelir ve kadrand "kadran-git
// calismiyor" DEMEMELI. Unavailable ise gercek arama/tasima hatasidir.
func TestUnknownFromKadranGitIsNotNotRunning(t *testing.T) {
	got := mapError(status.Error(codes.Unknown, "GitHub request failed"))
	if strings.Contains(got.Error(), MsgNotRunning) {
		t.Fatalf("Unknown -> %v: a running kadran-git reported as not running", got)
	}
	if status.Code(got) != codes.Unavailable || !strings.Contains(got.Error(), MsgGitHubFailed) {
		t.Fatalf("Unknown -> %v, want retryable Unavailable %q", got, MsgGitHubFailed)
	}
	if got := mapError(status.Error(codes.Unavailable, "connection refused")); !strings.Contains(got.Error(), MsgNotRunning) {
		t.Fatalf("Unavailable -> %v, want %q", got, MsgNotRunning)
	}
}
