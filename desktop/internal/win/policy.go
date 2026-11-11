package win

// Camın yükü her sistemde derlenir: Linux'taki mutasyon işi de ölçsün.
const (
	accentDisabled                = 0
	accentEnableAcrylicBlurBehind = 4
	accentFlagGradientColor       = 2
)

// AccentPolicy, SetWindowCompositionAttribute'un yükü (ACCENT_POLICY).
type AccentPolicy struct {
	State       int32
	Flags       int32
	Color       uint32
	AnimationID int32
}

// Policy, cam açıkken Akrilik, ton temaya göre (AABBGGRR); kapalıyken efekt
// yok. Düz bulanıklık (3) Electron'un penceresinde arkayı siyah çiziyordu
// (K-163); kullanıcı Akriliği seçti.
func Policy(on bool, tint uint32) AccentPolicy {
	state := int32(accentDisabled)
	if on {
		state = accentEnableAcrylicBlurBehind
	}
	return AccentPolicy{State: state, Flags: accentFlagGradientColor, Color: tint}
}
