package exec

import (
	"fmt"

	"github.com/erkanrzgc/panely/internal/audit"
)

// ════════════════════════════════════════════════════════════════════
//  DENETİM KAYDI: NE ZAMAN, HANGİ SIRAYLA
// ════════════════════════════════════════════════════════════════════
//
// # Sıra: önce YAP, sonra YAZ
//
// İki seçenek vardı ve ikisi de bir şey kaybediyor:
//
//   yaz→yap : işlem başarısız olursa zincirde OLMAMIŞ bir olayın kaydı
//             kalır. Denetim günlüğünün tek değeri "burada yazan oldu"
//             güvencesidir; bu seçenek onu doğrudan çürütür.
//   yap→yaz : yazma başarısız olursa yapılmış bir işlem kayıtsız kalır.
//
// İkincisi seçildi, çünkü birincisinin kaybı SESSİZDİR (kimse sahte
// kaydı fark etmez) ama ikincisininki GÜRÜLTÜLÜ yapılabilir: kayıt
// yazılamazsa çağırana hata döner, işlem başarılı olsa bile. Operatör
// "işlem oldu ama kaydedilemedi" durumunu öğrenir.
//
// Bu, container.go'daki mevcut duruşun devamı: hiçbir konteynere
// dokunmayan bir çağrı zincire yazılmaz.
//
// # Reddedilenler NEDEN yazılıyor?
//
// Doğrulamada reddedilen istek ayrıcalıklı hiçbir şey YAPMAZ — yani
// yukarıdaki kurala göre yazılmaması gerekirdi. Yine de yazılıyor, çünkü
// reddedilen istek güvenlik modelinin DEVREYE GİRDİĞİ andır ve tam olarak
// izlenmesi gereken şey odur: ele geçirilmiş bir panelyd'nin şemayı
// zorlama denemeleri ancak böyle görünür olur (AUDIT_OUTCOME_DENIED).
//
// Zincire "olmamış olay" da koymaz: reddetme GERÇEKTEN olmuştur.

// record, executor günlüğüne bir kayıt yazar.
//
// params ÇAĞIRAN tarafından redakte edilmiş olmalıdır (audit.RedactEnv /
// audit.RedactSensitive). Bu fonksiyon redaksiyon YAPMAZ: burada yapsaydı,
// hangi alanın hassas olduğunu bilmeyen genel bir yer karar veriyor olurdu.
func (s *Server) record(action, target string, params map[string]string, outcome audit.Outcome, detail string) error {
	paramsJSON := ""
	if len(params) > 0 {
		var err error
		if paramsJSON, err = audit.MarshalParams(params); err != nil {
			return fmt.Errorf("denetim parametreleri kodlanamadı: %w", err)
		}
	}

	_, err := s.journal.Append(audit.Record{
		// Aktör executor'ın kendisidir. Çağıranın kimliği panelyd'nin
		// zincirinde durur; buradaki zincir "ayrıcalıklı süreç ne yaptı"
		// sorusunu yanıtlar ve ikisinin karşılaştırılması farkı ortaya
		// çıkarır.
		Actor:      audit.SystemActor("executor"),
		Action:     action,
		Target:     target,
		ParamsJSON: paramsJSON,
		Outcome:    outcome,
		Detail:     detail,
	})
	if err != nil {
		return fmt.Errorf("denetim kaydı yazılamadı: %w", err)
	}
	return nil
}

// denied, doğrulaması başarısız olan isteği kaydeder ve hatayı döndürür.
//
// Kayıt yazılamazsa ASIL hata yine de döndürülür: reddetme sebebi
// çağıranın öğrenmesi gereken şeydir ve onu bir günlük sorunuyla
// değiştirmek istemciyi yanlış yöne bakmaya iter.
func (s *Server) denied(action, target string, params map[string]string, cause error) error {
	// Reddetme sebebi bizim ürettiğimiz doğrulama mesajıdır — kullanıcı
	// verisi değil — bu yüzden detaya yazılabilir.
	_ = s.record(action, target, params, audit.OutcomeDenied, cause.Error())
	return invalidArgument(cause)
}

// completed, gerçekleşmiş bir işlemi kaydeder.
//
// opErr nil ise başarı, değilse başarısızlık yazılır. Kayıt yazılamazsa
// işlem başarılı olsa bile HATA DÖNER: sessizce kayıtsız kalmasındansa
// çağıranın bunu bilmesi yeğdir.
func (s *Server) completed(action, target string, params map[string]string, opErr error) error {
	outcome, detail := audit.OutcomeSuccess, ""
	if opErr != nil {
		outcome = audit.OutcomeFailure
		// ⚠ opErr'in METNİ kayda GİRMEZ. Docker'ın hata mesajı kullanıcının
		// imajından/deposundan gelen metni taşıyabilir ve zincir
		// ekle-sadece'dir: bir kez yazılan sır geri alınamaz.
		detail = "işlem başarısız (ayrıntı çağırana döndü, kayda yazılmadı)"
	}
	if err := s.record(action, target, params, outcome, detail); err != nil {
		return err
	}
	return opErr
}
