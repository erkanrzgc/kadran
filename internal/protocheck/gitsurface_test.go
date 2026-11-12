package protocheck_test

import (
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
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

// freeTextNames, string alanda yasak olan serbest metin adlarıdır.
var freeTextNames = map[string]bool{
	"error": true, "message": true, "detail": true, "reason": true,
}

// forbiddenField, alanın yasak olup olmadığını ve nedenini döner.
func forbiddenField(fd protoreflect.FieldDescriptor) (string, bool) {
	name := strings.ToLower(strings.ReplaceAll(string(fd.Name()), "_", ""))
	for _, sub := range forbiddenSubstrings {
		if strings.Contains(name, sub) {
			return "name contains " + sub, true
		}
	}
	if fd.Kind() == protoreflect.StringKind && freeTextNames[name] {
		return "free-text string field", true
	}
	return "", false
}

// collectGitHubScope, denetlenecek tüm mesajları toplar: git.proto'daki her
// mesaj ve KadranService'in adında "GitHub" geçen yöntemlerinin girdi ve
// çıktılarından erişilebilen her mesaj.
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

	svcs := kadranv1.File_kadran_v1_api_proto.Services()
	for i := 0; i < svcs.Len(); i++ {
		svc := svcs.Get(i)
		if svc.Name() != "KadranService" {
			continue
		}
		for j := 0; j < svc.Methods().Len(); j++ {
			m := svc.Methods().Get(j)
			if strings.Contains(string(m.Name()), "GitHub") {
				visit(m.Input())
				visit(m.Output())
			}
		}
	}
	return scope
}

func TestGitHubSurfaceHasNoSecretFields(t *testing.T) {
	scope := collectGitHubScope()

	// Boş tarama sessizce geçmemeli.
	for _, want := range []protoreflect.FullName{
		"kadran.v1.GitHubConnection",
		"kadran.v1.FetchArchiveResponse",
		"kadran.v1.ResolveGitHubBranchRequest",
		"kadran.v1.StartGitHubConnectResponse",
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
		// Mesaj tipli "error" serbest metin değildir.
		{"error", msg, false},
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
