package protocheck_test

import (
	"slices"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// Bu test GitHub şema yüzeyinin değişmezini denetler (K-167): kadrand'ye ve
// istemcilere giden hiçbir GitHub mesajında jeton, OAuth cihaz kodu, sır,
// imzalı indirme adresi ya da serbest metin hata alanı TEMSİL EDİLEMEZ.
// Temsil edilemeyen şey kazara sızdırılamaz.
//
// Tarama kaynak metin üzerinde değil, ÜRETİLMİŞ betimleyiciler üzerindedir:
// tek satırlık mesaj, Allman parantezi, bölünmüş bildirim ya da blok yorum
// gibi yazım biçimleri taramayı atlatamaz.

// forbiddenSubstrings, normalize edilmiş (küçük harf, alt çizgisiz) alan
// adında geçmesi yasak parçalardır.
var forbiddenSubstrings = []string{
	"token", "secret", "signed", "devicecode", "download", "password", "privatekey",
}

// freeTextSubstrings, string ve bytes alanların normalize edilmiş adında
// geçmesi yasak serbest metin parçalarıdır (error_message, last_error,
// errorDetail gibi bileşik adlar da yakalanır).
var freeTextSubstrings = []string{"error", "message", "detail", "reason"}

// forbiddenField, alanın yasak olup olmadığını ve nedenini döner.
func forbiddenField(fd protoreflect.FieldDescriptor) (string, bool) {
	name := strings.ToLower(strings.ReplaceAll(string(fd.Name()), "_", ""))
	for _, sub := range forbiddenSubstrings {
		if strings.Contains(name, sub) {
			return "name contains " + sub, true
		}
	}
	if fd.Kind() != protoreflect.StringKind && fd.Kind() != protoreflect.BytesKind {
		return "", false
	}
	for _, sub := range freeTextSubstrings {
		if strings.Contains(name, sub) {
			return "free-text field contains " + sub, true
		}
	}
	return "", false
}

// gitScoped, yöntemin GitHub/Git yüzeyinde olup olmadığını söyler: servis ya
// da yöntem adında "git" geçiyorsa (büyük/küçük harfe duyarsız). Fazla
// kapsamak zararsızdır (tarama büyür), eksik kapsamak bir alanı kaçırır.
func gitScoped(service, method string) bool {
	return strings.Contains(strings.ToLower(service), "git") || strings.Contains(strings.ToLower(method), "git")
}

// collectGitHubScope, denetlenecek tüm mesajları toplar: git.proto'daki her
// mesaj ve kadran.v1'in HER servisinde GitHub/Git yöntemlerinin (gitScoped)
// girdi ve çıktılarından erişilebilen her mesaj.
func collectGitHubScope() map[protoreflect.FullName]protoreflect.MessageDescriptor {
	scope := map[protoreflect.FullName]protoreflect.MessageDescriptor{}
	var visit func(md protoreflect.MessageDescriptor)
	visit = func(md protoreflect.MessageDescriptor) {
		if _, seen := scope[md.FullName()]; seen {
			return
		}
		scope[md.FullName()] = md
		for i := 0; i < md.Fields().Len(); i++ {
			if sub := md.Fields().Get(i).Message(); sub != nil {
				visit(sub)
			}
		}
		for i := 0; i < md.Messages().Len(); i++ {
			visit(md.Messages().Get(i))
		}
	}

	msgs := kadranv1.File_kadran_v1_git_proto.Messages()
	for i := 0; i < msgs.Len(); i++ {
		visit(msgs.Get(i))
	}

	for _, m := range gitMethods() {
		visit(m.Input())
		visit(m.Output())
	}
	return scope
}

// gitMethods, kadran.v1 paketindeki her dosyanın her servisinden gitScoped
// yöntemleri döner.
func gitMethods() []protoreflect.MethodDescriptor {
	var out []protoreflect.MethodDescriptor
	protoregistry.GlobalFiles.RangeFilesByPackage("kadran.v1", func(f protoreflect.FileDescriptor) bool {
		for i := 0; i < f.Services().Len(); i++ {
			svc := f.Services().Get(i)
			for j := 0; j < svc.Methods().Len(); j++ {
				if m := svc.Methods().Get(j); gitScoped(string(svc.Name()), string(m.Name())) {
					out = append(out, m)
				}
			}
		}
		return true
	})
	return out
}

func TestGitHubSurfaceHasNoSecretFields(t *testing.T) {
	scope := collectGitHubScope()

	// Boş tarama sessizce geçmemeli: KadranService'in GitHub yöntemleri ve
	// git.proto'nun iki servisi (GitAdminService, GitFetchService) kapsamda.
	for _, want := range []protoreflect.FullName{
		"kadran.v1.GitHubConnection",
		"kadran.v1.FetchArchiveResponse",
		"kadran.v1.ResolveGitHubBranchRequest",
		"kadran.v1.StartGitHubConnectResponse",
		"kadran.v1.GetGitHubStatusResponse",
		"kadran.v1.DisconnectGitHubResponse",
		"kadran.v1.GetGitStatusResponse",
		"kadran.v1.ResolveBranchResponse",
		"kadran.v1.StartDeviceFlowResponse",
		"kadran.v1.FetchArchiveRequest",
	} {
		if _, ok := scope[want]; !ok {
			t.Fatalf("scope is missing %s; the walk is not covering the schema", want)
		}
	}

	var bad []string
	for _, md := range scope {
		for i := 0; i < md.Fields().Len(); i++ {
			fd := md.Fields().Get(i)
			if why, hit := forbiddenField(fd); hit {
				bad = append(bad, string(md.Name())+"."+string(fd.Name())+" ("+why+")")
			}
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatalf("forbidden fields in GitHub surface:\n  %s", strings.Join(bad, "\n  "))
	}
}

// syntheticField, tek alanlı sahte bir dosyadan gerçek bir FieldDescriptor üretir.
func syntheticField(t *testing.T, name string, typ descriptorpb.FieldDescriptorProto_Type) protoreflect.FieldDescriptor {
	t.Helper()
	field := &descriptorpb.FieldDescriptorProto{
		Name:     proto.String(name),
		Number:   proto.Int32(1),
		Type:     typ.Enum(),
		Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		JsonName: proto.String(name),
	}
	if typ == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE {
		field.TypeName = proto.String(".synthetic.Msg")
	}
	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("synthetic.proto"),
		Package: proto.String("synthetic"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name:  proto.String("Msg"),
			Field: []*descriptorpb.FieldDescriptorProto{field},
		}},
	}
	file, err := protodesc.NewFile(fdp, nil)
	if err != nil {
		t.Fatalf("synthetic file for %q: %v", name, err)
	}
	return file.Messages().Get(0).Fields().Get(0)
}

func TestForbiddenFieldCatchesNames(t *testing.T) {
	str := descriptorpb.FieldDescriptorProto_TYPE_STRING
	msg := descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
	bytes := descriptorpb.FieldDescriptorProto_TYPE_BYTES
	cases := []struct {
		name string
		typ  descriptorpb.FieldDescriptorProto_Type
		want bool
	}{
		{"token", str, true},
		{"access_token", str, true},
		{"my_token", str, true},
		{"accessToken", str, true},
		{"refresh_token", str, true},
		{"client_secret", str, true},
		{"signed_url", str, true},
		{"device_code", str, true},
		{"download_url", str, true},
		{"password", str, true},
		{"private_key", str, true},
		{"error", str, true},
		{"message", str, true},
		{"detail", str, true},
		{"reason", str, true},
		// Bileşik adlar ve büyük harf de serbest metindir (Ruling 9).
		{"error_message", str, true},
		{"last_error", str, true},
		{"errorDetail", str, true},
		{"failure_reason", str, true},
		{"Message", str, true},
		// bytes alanlar da metin taşır.
		{"error", bytes, true},
		{"status_detail", bytes, true},
		// Mesaj tipli "error" serbest metin değildir.
		{"error", msg, false},
		{"last_error", msg, false},
		{"chunk", bytes, false},
		{"user_code", str, false},
		{"verification_uri", str, false},
		{"install_url", str, false},
		{"login", str, false},
		{"commit_sha", str, false},
		{"full_name", str, false},
		{"owner", str, false},
		{"repo", str, false},
		{"branch", str, false},
		{"failure", str, false},
		{"connected_at", msg, false},
		{"prompt", msg, false},
		{"pending", msg, false},
	}
	for _, tc := range cases {
		fd := syntheticField(t, tc.name, tc.typ)
		_, got := forbiddenField(fd)
		if got != tc.want {
			t.Errorf("forbiddenField(%s, %v) = %v, want %v", tc.name, tc.typ, got, tc.want)
		}
	}
}

// Yöntem süzgeci büyük/küçük harfe duyarsızdır ve yalnız KadranService'e
// bakmaz.
func TestGitScopedIsCaseInsensitiveAcrossServices(t *testing.T) {
	cases := []struct {
		svc, method string
		want        bool
	}{
		{"KadranService", "GetGitHubStatus", true},
		{"KadranService", "GetGITHUBStatus", true},
		{"KadranService", "resolvegithubbranch", true},
		{"GitAdminService", "StartDeviceFlow", true},
		{"GitFetchService", "FetchArchive", true},
		{"OtherService", "DisconnectGit", true},
		{"KadranService", "Deploy", false},
		{"ExecService", "ImageBuild", false},
	}
	for _, tc := range cases {
		if got := gitScoped(tc.svc, tc.method); got != tc.want {
			t.Errorf("gitScoped(%s, %s) = %v, want %v", tc.svc, tc.method, got, tc.want)
		}
	}
	// Taranan yöntemlerin listesi: beklenmedik bir yöntem kapsama girerse görünür.
	var names []string
	for _, m := range gitMethods() {
		names = append(names, string(m.FullName()))
	}
	sort.Strings(names)
	t.Logf("git-scoped methods: %s", strings.Join(names, ", "))
	for _, want := range []string{
		"kadran.v1.KadranService.StartGitHubConnect",
		"kadran.v1.GitAdminService.StartDeviceFlow",
		"kadran.v1.GitFetchService.FetchArchive",
	} {
		if !slices.Contains(names, want) {
			t.Errorf("%s is not in scope", want)
		}
	}
}
