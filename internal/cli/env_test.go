package cli

import "regexp"

// ── Env kancaları (Konsol, Görev 2) ───────────────────────────────────

// totpEnableGolden, kanca yokken `totp enable` stdout'unun bayt bayt
// hâli; yalnız iki zaman damgası TIME ile değiştirilmiş.
const totpEnableGolden = "add this server to your authenticator app (confirm before TIME):\n  1. scan this QR code with the app:\n\n                                         \n                                         \n    █▀▀▀▀▀█  ▀▄▄█  ██  █▄  █▄ █▀▀▀▀▀█    \n    █ ███ █ ▀██▄▀▄▄▀▀ ██▀ ▄ ▀ █ ███ █    \n    █ ▀▀▀ █  ▄▀  ▄ ▄ ▄██▄  █▄ █ ▀▀▀ █    \n    ▀▀▀▀▀▀▀ █▄█ █▄▀▄▀▄█▄█▄█ █ ▀▀▀▀▀▀▀    \n    █▀█▄▀█▀▀█ ██  ▄▄▄█▄ ██▄▀ █▀   ▀ ▄    \n    █▀▀▄▄▀▀ ▄▀ ▄ ▄▀▄▀▄   ▄▄ █ ▄▀▄▀▀▀▀    \n    ▀█▄▄ ▀▀▄█  ▄█▄▄██▀█ ▄█  ▄██▄ ▀ ▄█    \n    ▀ █▀█▄▀ ▄█▄▀█▄▀▄▄▄▄ █▄▄ ▀ ▀▄▀ ▀█▀    \n     ▄█  ▀▀▀  ██  ▀▄█   ▀█▄ ▄██▀ ▄▄ █    \n    ▄▄ ▀▄█▀▀▄ ▀▄ ▀ ▄▀█▀ ██▀  █▀ ▀▄▀█▀    \n    ▀█▀███▀ ▀█▀▄█▄ ▄▄▄ ▀█▀█ ▄▀█▀ █▄▀█    \n    ▀▄▀ █▀▀▄  ▄▀█▄█▄██▀▄ ▄▄ ▀▄█▀▄▄ ▀█    \n    ▀ ▀  ▀▀▀▄▄▀█  █▄█▄▄ █  ▀█▀▀▀█ ▄█▄    \n    █▀▀▀▀▀█ █  ▄ ▀   ██ ▀████ ▀ ██ ▀█    \n    █ ███ █ ▀ ▄▄█▄   ▄▄ ▄█ ▄█▀▀██ ▄▀▄    \n    █ ▀▀▀ █ █ █▀█▄█▄ █▄▄█▄██▄▄ █▀▀ ▄▀    \n    ▀▀▀▀▀▀▀ ▀▀▀▀  ▀▀    ▀▀ ▀▀▀▀▀▀▀ ▀▀    \n                                         \n                                         \n\n     if the app does not scan it (some cannot read light-on-dark codes),\n     add an account by hand, time based, with this key:\n       JBSWY3DPEHPK3PXP\n     link: otpauth://totp/Kadran:srv?algorithm=SHA1&digits=6&issuer=Kadran&period=30&secret=JBSWY3DPEHPK3PXP\n  2. type the 6-digit code the app shows (not the key)\nTOTP is on.\nrecovery codes (shown once, store them offline):\n  AAAAA-BBBBB\n  CCCCC-DDDDD\nsigned in until TIME\n"

var (
	enrollZaman = regexp.MustCompile(`confirm before [^)]*\)`)
	girisZaman  = regexp.MustCompile(`signed in until [^\n]*`)
)

func zamansiz(s string) string {
	s = enrollZaman.ReplaceAllString(s, "confirm before TIME)")
	return girisZaman.ReplaceAllString(s, "signed in until TIME")
}
