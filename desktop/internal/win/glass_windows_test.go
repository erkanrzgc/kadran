package win

import "testing"

func TestBuildIsReadable(t *testing.T) {
	if Build() < 10240 {
		t.Fatalf("Windows yapısı okunamadı: %d", Build())
	}
}

func TestConfirmLayoutConstants(t *testing.T) {
	// TASKDIALOGCONFIG pshpack1 ile 160, TASKDIALOG_BUTTON 12 bayt (amd64).
	if taskDialogConfigSize != 160 || taskDialogButtonSize != 12 {
		t.Fatal("görev kutusu yerleşimi değişti")
	}
}
