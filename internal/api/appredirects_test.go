package api

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/kadran/internal/audit"
	"github.com/erkanrzgc/kadran/internal/deploy"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

func TestValidateRedirects(t *testing.T) {
	nine := make([]string, 0, 9)
	eight := make([]string, 0, 8)
	for i := range 9 {
		n := fmt.Sprintf("r%d.example.net", i)
		nine = append(nine, n)
		if i < 8 {
			eight = append(eight, n)
		}
	}

	cases := []struct {
		name    string
		domain  string
		names   []string
		wantErr string // boş = hata beklenmez
	}{
		{"büyük harf", "example.com", []string{"Old.Example.net"}, "invalid domain"},
		{"IP adresi", "example.com", []string{"203.0.113.7"}, "invalid domain"},
		{"kendi domaini", "example.com", []string{"example.com"}, "same as the app domain"},
		{"tekrar", "example.com", []string{"a.example.net", "a.example.net"}, "duplicate"},
		{"dokuz ad", "example.com", nine, "too many redirect names"},
		{"domain boş, ad var", "", []string{"a.example.net"}, "redirect names need a domain"},
		{"sekiz geçerli ad", "example.com", eight, ""},
		{"boş ad", "example.com", []string{""}, "must not be empty"},
		{"geçerli + boş ad", "example.com", []string{"a.com", ""}, "must not be empty"},
		{"hiç ad yok", "example.com", nil, ""},
		{"domain boş, ad yok", "", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRedirects(tc.domain, tc.names)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("beklenmeyen hata: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("hata = %v, %q içermeliydi", err, tc.wantErr)
			}
		})
	}
}

func TestCreateAppEchoesWWW(t *testing.T) {
	srv, _ := newUpdateServer(t, &fakeReconciler{})
	spec := testSpec()
	spec.Domain = "example.com"

	resp, err := srv.CreateApp(context.Background(), &kadranv1.CreateAppRequest{Spec: spec})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	www := resp.GetApp().GetSpec().Www
	if www == nil || *www {
		t.Fatalf("Www = %v, dolu ve false olmalıydı (yeni sunucunun işareti)", www)
	}
}

func TestCreateAppNameConflictIsAlreadyExists(t *testing.T) {
	srv, _ := newUpdateServer(t, &fakeReconciler{})
	a := testSpec()
	a.AppId = "blog"
	a.Domain = "example.com"
	a.Redirects = []string{"old.example.net"}
	mustCreateApp(t, srv, a)

	b := testSpec()
	b.AppId = "shop"
	b.Domain = "shop.example.com"
	b.Redirects = []string{"old.example.net"}
	_, err := srv.CreateApp(context.Background(), &kadranv1.CreateAppRequest{Spec: b})
	if got := status.Code(err); got != codes.AlreadyExists {
		t.Fatalf("kod = %v (%v), AlreadyExists bekleniyordu", got, err)
	}
	if !strings.Contains(err.Error(), "blog") {
		t.Errorf("hata sahibi uygulamayı söylemiyor: %v", err)
	}
}

func TestUpdateAppNamesOnlyReconciles(t *testing.T) {
	rec := &fakeReconciler{}
	srv, _ := newUpdateServer(t, rec)
	spec := testSpec()
	spec.Domain = "example.com"
	mustCreateApp(t, srv, spec)

	resp := update(t, srv, &kadranv1.UpdateAppRequest{
		AppId:       "blog",
		RedirectAdd: []string{"b.example.net", "a.example.net"},
	})

	if rec.calls != 1 {
		t.Fatalf("uzlaştırma %d kez çağrıldı, 1 bekleniyordu", rec.calls)
	}
	if resp.GetProxyDetail() != "" {
		t.Errorf("ProxyDetail dolu: %q", resp.GetProxyDetail())
	}
	d := resp.GetRedirectDetail()
	for _, want := range []string{"a.example.net", "b.example.net", "example.com"} {
		if !strings.Contains(d, want) {
			t.Errorf("RedirectDetail %q içinde %q yok", d, want)
		}
	}
}

func TestUpdateAppReportsSkippedRedirects(t *testing.T) {
	rec := &fakeReconciler{result: deploy.Result{SkippedRedirects: map[string]string{"a.example.net": "no tls"}}}
	srv, _ := newUpdateServer(t, rec)
	spec := testSpec()
	spec.Domain = "example.com"
	mustCreateApp(t, srv, spec)

	resp := update(t, srv, &kadranv1.UpdateAppRequest{
		AppId:       "blog",
		RedirectAdd: []string{"a.example.net"},
	})
	if !strings.Contains(resp.GetRedirectDetail(), "redirect a.example.net NOT applied (no tls)") {
		t.Errorf("atlanan ad bildirilmedi: %q", resp.GetRedirectDetail())
	}
}

func TestUpdateAppNamesReconcileFailureSaysSaved(t *testing.T) {
	rec := &fakeReconciler{err: fmt.Errorf("caddy down")}
	srv, _ := newUpdateServer(t, rec)
	spec := testSpec()
	spec.Domain = "example.com"
	mustCreateApp(t, srv, spec)

	_, err := srv.UpdateApp(context.Background(), &kadranv1.UpdateAppRequest{
		AppId: "blog", RedirectAdd: []string{"a.example.net"},
	})
	if status.Code(err) != codes.Unavailable || !strings.Contains(err.Error(), "SAVED") {
		t.Fatalf("hata = %v, Unavailable + SAVED bekleniyordu", err)
	}
	if got := mustGetSpec(t, srv, "blog").GetRedirects(); len(got) != 1 {
		t.Errorf("ad kaydedilmemiş: %v", got)
	}
}

func TestUpdateAppAddAndRemoveSameRedirect(t *testing.T) {
	srv, _ := newUpdateServer(t, &fakeReconciler{})
	spec := testSpec()
	spec.Domain = "example.com"
	mustCreateApp(t, srv, spec)

	_, err := srv.UpdateApp(context.Background(), &kadranv1.UpdateAppRequest{
		AppId:          "blog",
		RedirectAdd:    []string{"a.example.net"},
		RedirectRemove: []string{"a.example.net"},
	})
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("kod = %v (%v), InvalidArgument bekleniyordu", got, err)
	}
	if !strings.Contains(err.Error(), "both added and removed") {
		t.Errorf("mesaj: %v", err)
	}
}

func TestUpdateAppAuditCarriesRedirects(t *testing.T) {
	srv, db := newUpdateServer(t, &fakeReconciler{})
	spec := testSpec()
	spec.Domain = "example.com"
	mustCreateApp(t, srv, spec)

	update(t, srv, &kadranv1.UpdateAppRequest{
		AppId:          "blog",
		Www:            boolp(true),
		RedirectAdd:    []string{"a.example.net"},
		RedirectRemove: []string{"z.example.net"},
	})

	var rec *audit.Record
	for _, r := range auditActions(t, db) {
		if r.Action == "app.update" {
			rec = &r
			break
		}
	}
	if rec == nil {
		t.Fatal("app.update zincire yazılmadı")
		return
	}
	for _, want := range []string{"redirect_add", "a.example.net", "redirect_remove", "z.example.net", "www"} {
		if !strings.Contains(rec.ParamsJSON, want) {
			t.Errorf("denetimde %q yok: %s", want, rec.ParamsJSON)
		}
	}
}

func TestCreateAppAuditCarriesWWWAndRedirects(t *testing.T) {
	srv, db := newUpdateServer(t, &fakeReconciler{})
	spec := testSpec()
	spec.Domain = "example.com"
	spec.Www = boolp(true)
	spec.Redirects = []string{"b.example.net", "a.example.net"}
	mustCreateApp(t, srv, spec)

	var rec *audit.Record
	for _, r := range auditActions(t, db) {
		if r.Action == "app.create" {
			rec = &r
			break
		}
	}
	if rec == nil {
		t.Fatal("app.create zincire yazılmadı")
		return
	}
	for _, want := range []string{`"www":"true"`, `"redirects":"b.example.net,a.example.net"`} {
		if !strings.Contains(rec.ParamsJSON, want) {
			t.Errorf("denetimde %q yok: %s", want, rec.ParamsJSON)
		}
	}
}

func TestAppRedirectsRoundTripThroughProto(t *testing.T) {
	spec := testSpec()
	spec.Www = boolp(true)
	spec.Redirects = []string{"a.example.net"}

	app := appFromProto(spec)
	if !app.WWW || len(app.Redirects) != 1 {
		t.Fatalf("proto → depo çevriminde alanlar kayboldu: %+v", app)
	}
	back := appToProto(app).GetSpec()
	if back.Www == nil || !*back.Www || len(back.GetRedirects()) != 1 {
		t.Fatalf("depo → proto çevriminde alanlar kayboldu: %+v", back)
	}
}

func boolp(v bool) *bool { return &v }

func TestUpdateAppRejectsEmptyRedirectName(t *testing.T) {
	rec := &fakeReconciler{}
	srv, _ := newUpdateServer(t, rec)
	spec := testSpec()
	spec.Domain = "example.com"
	mustCreateApp(t, srv, spec)

	_, err := srv.UpdateApp(context.Background(), &kadranv1.UpdateAppRequest{
		AppId: "blog", RedirectAdd: []string{""},
	})
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("kod = %v (%v), InvalidArgument bekleniyordu", got, err)
	}
	if got := mustGetSpec(t, srv, "blog").GetRedirects(); len(got) != 0 {
		t.Errorf("boş ad depoya yazıldı: %q", got)
	}
	if rec.calls != 0 {
		t.Errorf("reddedilen istek uzlaştırma tetikledi: %d", rec.calls)
	}
}

func TestUpdateAppNamesOnlyOnUnroutedAppIsNotReportedAsApplied(t *testing.T) {
	rec := &fakeReconciler{result: deploy.Result{Skipped: map[string]string{"blog": "no healthy release"}}}
	srv, _ := newUpdateServer(t, rec)
	spec := testSpec()
	spec.Domain = "example.com"
	mustCreateApp(t, srv, spec)

	resp, err := srv.UpdateApp(context.Background(), &kadranv1.UpdateAppRequest{
		AppId: "blog", RedirectAdd: []string{"a.example.net"},
	})
	if err != nil {
		t.Fatalf("kayıt başarılıydı, hata dönmemeliydi: %v", err)
	}
	if resp.GetProxyDetail() != "" {
		t.Errorf("ProxyDetail dolu: %q", resp.GetProxyDetail())
	}
	d := resp.GetRedirectDetail()
	for _, want := range []string{"SAVED", "a.example.net", "example.com", "NOT applied",
		"no healthy release", "kadran deploy blog"} {
		if !strings.Contains(d, want) {
			t.Errorf("RedirectDetail %q içinde %q yok", d, want)
		}
	}
	if strings.Contains(d, "reverse proxy updated") {
		t.Errorf("rotalanmamış uygulama için yalan başarı: %q", d)
	}
}
