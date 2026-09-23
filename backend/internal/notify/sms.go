package notify

// SMS bodies. Plain text, no HTML, but the same rule as everything else in this package: the
// recipient's language, not ours.

// MFACode builds the one-time code sent for TOTP-less login or SMS enrollment.
func MFACode(lang, code string) string {
	pt := "Seu código de acesso Beskos: " + code // pt-BR SMS text, not a translation gap -- lang:ok
	return tr(lang, pt, "Your Beskos access code: "+code)
}

// MFAEnrollCode builds the one-time code sent while a phone is being enrolled for SMS MFA.
func MFAEnrollCode(lang, code string) string {
	pt := "Seu código de verificação Beskos: " + code // pt-BR SMS text -- lang:ok
	return tr(lang, pt, "Your Beskos verification code: "+code)
}
