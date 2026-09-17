package api

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/store"
)

// ListAlarms, etkin arıza koşullarını döndürür.
//
// Salt okunur; denetim zincirine girmiyor (record.go'daki gerekçe:
// durum okuma gürültüsü, durum değiştiren işlemleri görünmez kılardı).
//
// ⚠ Alarmların AÇILIP KAPANMASI zincire girmiyor — bu bilinçli. Zincir
// kasıtlı operatör eylemlerinin kaydı; alarm ise sistemin kendi
// gözlemi. İkisini karıştırmak, "kim ne yaptı" sorusunu makine
// gürültüsüyle doldururdu.
func (s *Server) ListAlarms(
	ctx context.Context, _ *panelyv1.ListAlarmsRequest,
) (*panelyv1.ListAlarmsResponse, error) {
	alarms, err := s.store.ListAlarms(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "alarmlar okunamadı: %v", err)
	}

	out := make([]*panelyv1.AlarmInfo, 0, len(alarms))
	for _, a := range alarms {
		out = append(out, &panelyv1.AlarmInfo{
			Id:        a.ID,
			Kind:      a.Kind,
			Target:    a.Target,
			Severity:  severityToProto(a.Severity),
			SinceUnix: a.Since.Unix(),
			Detail:    a.Detail,
		})
	}
	return &panelyv1.ListAlarmsResponse{
		Alarms: out,
		// Sabit true. Teslimat yolu yok: panelyd'nin systemd birimi
		// IPAddressDeny=any taşıyor ve dışarı çıkamıyor (ölçüldü).
		DeliveryIsLocalOnly: true,
	}, nil
}

// severityToProto, depo ciddiyetini şema karşılığına çevirir.
//
// Bilinmeyen değer UNSPECIFIED'a düşüyor, sessizce "uyari" sayılmıyor:
// tanınmayan bir ciddiyeti daha düşük bir seviyeye indirmek, gerçekten
// kritik bir koşulu görünmez kılabilirdi.
func severityToProto(s string) panelyv1.AlarmSeverity {
	switch s {
	case store.SeverityCritical:
		return panelyv1.AlarmSeverity_ALARM_SEVERITY_CRITICAL
	case store.SeverityWarning:
		return panelyv1.AlarmSeverity_ALARM_SEVERITY_WARNING
	default:
		return panelyv1.AlarmSeverity_ALARM_SEVERITY_UNSPECIFIED
	}
}
