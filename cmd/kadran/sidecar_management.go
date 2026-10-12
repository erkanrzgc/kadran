package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/erkanrzgc/kadran/internal/client"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

const maxDesktopOutput = 512 << 10

// lifecycleTimeout, geri alma ve silmenin istemci süresi. İş sunucuda
// önce uygulamanın kuyruğuna girer, sonra çalışır. Kuyruktayken dolan
// süre işi temiz iptal eder (koordinatör iptale dayanıklı); ÇALIŞIRKEN
// dolan süre ise geri almayı boşaltmanın ortasında kesebilir. Bu yüzden
// sunucunun en kötü çalışma süresinden büyük olmalı:
//
//	sağlık kapısı            90 sn  (deploy.DefaultGate.Timeout)
//	boşaltma + durdurma      20 sn  (deploy.DefaultDrain: Window + Grace)
//	mühür                    10 sn  (api sealTimeout)
//	konteyner kurma/silme   ~60 sn  (Docker'a bağlı pay)
//	                       ≈ 3 dk;  kısa bir kuyruk beklemesi için +1 dk
//
// Sunucu sabitleri istemciye içe aktarılmıyor (istemci ikilisine sunucu
// kodu girmesin); biri değişirse bu hesap da güncellenmeli.
const lifecycleTimeout = 4 * time.Minute

var desktopAppID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
var desktopRef = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,254}$`)

type managementParams struct {
	Target       string          `json:"target"`
	AppID        string          `json:"app_id,omitempty"`
	Ref          string          `json:"ref,omitempty"`
	Confirmation string          `json:"confirmation,omitempty"`
	Tail         uint32          `json:"tail,omitempty"`
	Spec         json.RawMessage `json:"spec,omitempty"`
	Update       json.RawMessage `json:"update,omitempty"`
}

// Every method has its own field whitelist; unknown fields never reach SSH.
func decodeManagement(req rpcRequest) (managementParams, error) {
	var p managementParams
	if len(req.Params) == 0 || bytes.Equal(bytes.TrimSpace(req.Params), []byte("null")) {
		return p, errors.New("params object is required")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(req.Params, &fields); err != nil {
		return p, err
	}
	allowed := map[string]bool{"target": true}
	switch req.Method {
	case "apps.get":
		allowed["app_id"] = true
	case "apps.create":
		allowed["spec"] = true
	case "apps.update":
		allowed["app_id"] = true
		allowed["update"] = true
	case "apps.deploy":
		allowed["app_id"] = true
		allowed["ref"] = true
	case "apps.delete", "apps.rollback":
		allowed["app_id"] = true
		allowed["confirmation"] = true
	case "apps.logs":
		allowed["app_id"] = true
		allowed["tail"] = true
	}
	for field := range fields {
		if !allowed[field] {
			return p, fmt.Errorf("unknown parameter %q", field)
		}
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return p, err
	}
	if strings.TrimSpace(p.Target) == "" {
		return p, errors.New("target is required")
	}
	if req.Method != "apps.list" && req.Method != "apps.create" && strings.HasPrefix(req.Method, "apps.") && !desktopAppID.MatchString(p.AppID) {
		return p, errors.New("invalid app_id")
	}
	if req.Method == "apps.deploy" && p.Ref != "" && !desktopRef.MatchString(p.Ref) {
		return p, errors.New("invalid branch, tag or full commit")
	}
	if (req.Method == "apps.delete" || req.Method == "apps.rollback") && p.Confirmation != p.AppID {
		return p, errors.New("confirmation must match app_id exactly")
	}
	if req.Method == "apps.logs" && (p.Tail < 1 || p.Tail > 10000) {
		return p, errors.New("tail must be 1-10000")
	}
	if req.Method == "apps.create" {
		var spec kadranv1.AppSpec
		if len(p.Spec) == 0 || string(p.Spec) == "null" {
			return p, errors.New("spec is required")
		}
		if err := protojson.Unmarshal(p.Spec, &spec); err != nil {
			return p, errors.New("invalid app spec")
		}
		if !desktopAppID.MatchString(spec.AppId) {
			return p, errors.New("invalid app_id")
		}
	}
	if req.Method == "apps.update" {
		var update kadranv1.UpdateAppRequest
		if len(p.Update) == 0 || string(p.Update) == "null" {
			return p, errors.New("update is required")
		}
		if err := protojson.Unmarshal(p.Update, &update); err != nil {
			return p, errors.New("invalid app update")
		}
		if update.AppId != "" {
			return p, errors.New("app_id must be outside update")
		}
	}
	return p, nil
}

func (s *sidecar) dispatchManagement(ctx context.Context, req rpcRequest) (any, *rpcError) {
	p, err := decodeManagement(req)
	if err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: err.Error()}
	}
	timeout := s.callTimeout
	switch req.Method {
	case "apps.deploy":
		timeout = 10 * time.Minute
	case "apps.rollback", "apps.delete":
		timeout = lifecycleTimeout
	case "backups.create":
		timeout = 90 * time.Second
	}
	return s.withConnTimeout(ctx, req.Params, timeout, func(ctx context.Context, conn *client.Client) (any, error) {
		rpc := conn.RPC()
		switch req.Method {
		case "apps.list":
			resp, err := rpc.ListApps(ctx, &kadranv1.ListAppsRequest{})
			if err != nil {
				return nil, err
			}
			return protoToJSON(resp)
		case "apps.get":
			resp, err := rpc.GetApp(ctx, &kadranv1.GetAppRequest{AppId: p.AppID, ReleaseLimit: 100})
			if err != nil {
				return nil, err
			}
			return protoToJSON(resp)
		case "apps.create":
			var spec kadranv1.AppSpec
			if err := protojson.Unmarshal(p.Spec, &spec); err != nil {
				return nil, errors.New("invalid app spec")
			}
			resp, err := rpc.CreateApp(ctx, &kadranv1.CreateAppRequest{Spec: &spec})
			if err != nil {
				return nil, err
			}
			return protoToJSON(resp)
		case "apps.update":
			var update kadranv1.UpdateAppRequest
			if err := protojson.Unmarshal(p.Update, &update); err != nil {
				return nil, errors.New("invalid app update")
			}
			update.AppId = p.AppID
			resp, err := rpc.UpdateApp(ctx, &update)
			if err != nil {
				return nil, err
			}
			return protoToJSON(resp)
		case "apps.delete":
			resp, err := rpc.DeleteApp(ctx, &kadranv1.DeleteAppRequest{AppId: p.AppID})
			if err != nil {
				return nil, err
			}
			return protoToJSON(resp)
		case "apps.rollback":
			resp, err := rpc.Rollback(ctx, &kadranv1.RollbackRequest{AppId: p.AppID})
			if err != nil {
				return nil, err
			}
			return protoToJSON(resp)
		case "apps.deploy":
			commit, branch := "", p.Ref
			if fullSHA.MatchString(p.Ref) {
				commit, branch = p.Ref, ""
			}
			sha, err := s.cli.resolveCommit(ctx, rpc, p.AppID, commit, branch)
			if err != nil {
				return nil, err
			}
			stream, err := rpc.Deploy(ctx, &kadranv1.DeployRequest{AppId: p.AppID, CommitSha: sha})
			if err != nil {
				return nil, err
			}
			return collectDesktopDeploy(stream.Recv)
		case "apps.logs":
			stream, err := rpc.StreamLogs(ctx, &kadranv1.StreamLogsRequest{AppId: p.AppID, TailLines: p.Tail, Follow: false})
			if err != nil {
				return nil, err
			}
			var output boundedOutput
			for {
				msg, err := stream.Recv()
				if errors.Is(err, io.EOF) {
					return map[string]any{"output": output.String(), "truncated": output.truncated}, nil
				}
				if err != nil {
					return nil, err
				}
				output.Write(msg.Data)
			}
		case "alarms.list":
			resp, err := rpc.ListAlarms(ctx, &kadranv1.ListAlarmsRequest{})
			if err != nil {
				return nil, err
			}
			return protoToJSON(resp)
		case "backups.list":
			resp, err := rpc.ListBackups(ctx, &kadranv1.ListBackupsRequest{})
			if err != nil {
				return nil, err
			}
			return protoToJSON(resp)
		case "backups.create":
			resp, err := rpc.CreateBackup(ctx, &kadranv1.CreateBackupRequest{})
			if err != nil {
				return nil, err
			}
			return protoToJSON(resp)
		}
		return nil, errors.New("unknown management method")
	})
}

type desktopDeployResult struct {
	ReleaseID string `json:"release_id"`
	ImageID   string `json:"image_id"`
	Output    string `json:"output"`
	Truncated bool   `json:"truncated"`
}

func collectDesktopDeploy(recv func() (*kadranv1.DeployResponse, error)) (desktopDeployResult, error) {
	var result desktopDeployResult
	var output boundedOutput
	accepted, succeeded := false, false
	for {
		msg, err := recv()
		if errors.Is(err, io.EOF) {
			if !succeeded {
				return result, errors.New("deploy ended without final success; check release history before retrying")
			}
			result.Output, result.Truncated = output.String(), output.truncated
			return result, nil
		}
		if err != nil {
			return result, err
		}
		if msg == nil || succeeded {
			return result, errors.New("invalid deploy event sequence")
		}
		switch event := msg.Event.(type) {
		case *kadranv1.DeployResponse_Accepted:
			if accepted || event.Accepted == nil || event.Accepted.ReleaseId == "" {
				return result, errors.New("invalid deploy acceptance")
			}
			accepted = true
			result.ReleaseID = event.Accepted.ReleaseId
		case *kadranv1.DeployResponse_Output:
			if !accepted || event.Output == nil {
				return result, errors.New("deploy output before acceptance")
			}
			output.Write(event.Output.Data)
		case *kadranv1.DeployResponse_Succeeded:
			if !accepted || event.Succeeded == nil || event.Succeeded.ReleaseId != result.ReleaseID || event.Succeeded.ImageId == "" {
				return result, errors.New("invalid deploy success")
			}
			succeeded = true
			result.ImageID = event.Succeeded.ImageId
		default:
			return result, errors.New("unknown deploy event")
		}
	}
}

type boundedOutput struct {
	buf       []byte
	truncated bool
}

func (out *boundedOutput) Write(data []byte) {
	remaining := maxDesktopOutput - len(out.buf)
	if len(data) > remaining {
		data = data[:remaining]
		out.truncated = true
	}
	out.buf = append(out.buf, data...)
}
func (out *boundedOutput) String() string {
	data := out.buf
	for len(data) > 0 && !utf8.Valid(data) { // Strip a split trailing rune; replace other invalid bytes without growing the wire unbounded.
		if len(data) > maxDesktopOutput-utf8.UTFMax {
			data = data[:len(data)-1]
			continue
		}
		return strings.ToValidUTF8(string(data), "?")
	}
	return string(data)
}
