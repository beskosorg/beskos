package notify

import (
	"fmt"
	"html"
	"strings"
	"time"
)

// Transactional notices to the customer: account created, subscription, trial, billing, and a new
// sign-in. These are the only contact a person has with us outside the app, so they carry the
// brand, use the recipient's language, and always ship a plain-text version alongside the HTML one
// (clients that reject HTML, screen readers, and spam filters that like seeing both parts).
//
// The language follows the user's: "en" when it starts with en, otherwise Portuguese. We don't
// invent more languages than the app has.

// Msg is a notice ready to send.
type Msg struct {
	Subject string
	HTML    string
	Text    string
}

// Line is a detail line of the notice (label → value), shown in a discreet table.
type Line struct{ Label, Value string }

const brandColor = "#116466"

// isEN reports whether the message goes out in English.
func isEN(lang string) bool { return strings.HasPrefix(strings.ToLower(lang), "en") }

// tr picks between the Portuguese text and the English one.
func tr(lang, pt, en string) string {
	if isEN(lang) {
		return en
	}
	return pt
}

// build assembles the branded HTML + the text version. Empty `cta`/`ctaURL` = a notice with no button.
func build(lang, subject, intro string, lines []Line, cta, ctaURL, footer string) Msg {
	var rows, txt strings.Builder
	for _, l := range lines {
		fmt.Fprintf(&rows, `<tr><td style="padding:7px 0;color:#6C7370;font-size:14px">%s</td>`+
			`<td style="padding:7px 0;text-align:right;font-size:14px;font-weight:600;color:#10161A">%s</td></tr>`,
			html.EscapeString(l.Label), html.EscapeString(l.Value))
		txt.WriteString(l.Label + ": " + l.Value + "\n")
	}
	button := ""
	if cta != "" && ctaURL != "" {
		button = fmt.Sprintf(
			`<a href="%s" style="display:inline-block;margin-top:22px;padding:13px 26px;background:%s;`+
				`color:#fff;border-radius:10px;text-decoration:none;font-weight:600;font-size:15px">%s</a>`,
			html.EscapeString(ctaURL), brandColor, html.EscapeString(cta))
	}
	body := fmt.Sprintf(`<!doctype html>
<html><body style="margin:0;padding:0;background:#FBFAF8">
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="background:#FBFAF8;padding:32px 16px">
<tr><td align="center">
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="max-width:480px;background:#fff;border:1px solid #E7E3DC;border-radius:16px;padding:32px;font-family:-apple-system,Segoe UI,Roboto,sans-serif">
<tr><td>
<div style="font-size:18px;font-weight:700;color:#10161A;letter-spacing:-0.01em">Beskos</div>
<h1 style="margin:22px 0 10px;font-size:21px;line-height:1.3;color:#10161A;letter-spacing:-0.02em">%s</h1>
<p style="margin:0;color:#4A5250;font-size:15px;line-height:1.6">%s</p>
%s
%s
<p style="margin:26px 0 0;color:#9BA29F;font-size:12px;line-height:1.6">%s</p>
</td></tr></table>
</td></tr></table>
</body></html>`,
		html.EscapeString(subject), intro,
		func() string {
			if rows.Len() == 0 {
				return ""
			}
			return `<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin-top:20px;border-top:1px solid #E7E3DC">` + rows.String() + `</table>`
		}(),
		button, html.EscapeString(footer))

	plain := stripTags(intro) + "\n\n" + txt.String()
	if ctaURL != "" {
		plain += "\n" + cta + ": " + ctaURL + "\n"
	}
	plain += "\n" + footer + "\n"
	return Msg{Subject: subject, HTML: body, Text: plain}
}

// stripTags removes the little HTML the texts use (just <b>), for the plain-text part.
func stripTags(s string) string {
	r := strings.NewReplacer("<b>", "", "</b>", "", "<br>", "\n")
	return r.Replace(s)
}

func footerFor(lang string) string {
	return tr(lang,
		"Recebeu esta mensagem porque tem uma conta Beskos. Não responda a este endereço.",
		"You're getting this because you have a Beskos account. Please don't reply to this address.")
}

// AccountCreated: the account exists and the address is already the person's own.
func AccountCreated(lang, email, domain, appURL string) Msg {
	subject := tr(lang, "A sua conta Beskos está pronta", "Your Beskos account is ready")
	intro := tr(lang,
		"Criámos a sua organização e a sua caixa de correio. Entre e defina a sua palavra-passe.",
		"We've created your organisation and your mailbox. Sign in and set your password.")
	lines := []Line{{tr(lang, "Endereço", "Address"), email}, {tr(lang, "Domínio", "Domain"), domain}}
	footer := tr(lang,
		"Para receber e-mail de fora, publique os registos de DNS do domínio — a app mostra-lhe quais. "+footerFor(lang),
		"To receive outside email, publish the domain's DNS records — the app shows you which. "+footerFor(lang))
	return build(lang, subject, intro, lines, tr(lang, "Entrar no Beskos", "Sign in to Beskos"), appURL, footer)
}

// TrialStarted: card on file, trial running, and the date it starts being charged.
func TrialStarted(lang, plan string, days int, endsAt time.Time, amount, appURL string) Msg {
	subject := tr(lang,
		fmt.Sprintf("O seu teste de %d dias começou", days),
		fmt.Sprintf("Your %d-day trial has started", days))
	intro := tr(lang,
		"Está tudo a funcionar. Não cobramos nada até ao fim do teste, e pode cancelar antes sem pagar.",
		"Everything is running. We charge nothing until the trial ends, and you can cancel before without paying.")
	lines := []Line{
		{tr(lang, "Plano", "Plan"), plan},
		{tr(lang, "Teste até", "Trial until"), endsAt.Format("02/01/2006")},
		{tr(lang, "Depois", "Then"), amount},
	}
	return build(lang, subject, intro, lines, tr(lang, "Abrir o Beskos", "Open Beskos"), appURL, footerFor(lang))
}

// SubscriptionActive: the subscription became active (checkout completed).
func SubscriptionActive(lang, plan, cycle, amount, appURL string) Msg {
	subject := tr(lang, "Subscrição confirmada", "Subscription confirmed")
	intro := tr(lang,
		"A sua subscrição está ativa. A fatura segue para o e-mail de faturação que nos indicou.",
		"Your subscription is active. The invoice goes to the billing email you gave us.")
	lines := []Line{
		{tr(lang, "Plano", "Plan"), plan},
		{tr(lang, "Periodicidade", "Billing period"), cycle},
		{tr(lang, "Valor", "Amount"), amount},
	}
	return build(lang, subject, intro, lines, tr(lang, "Ver a faturação", "View billing"), appURL, footerFor(lang))
}

// ChargeStarted: first charge (end of trial) or a paid renewal.
func ChargeStarted(lang, plan, amount, invoiceURL, appURL string) Msg {
	subject := tr(lang, "Pagamento recebido", "Payment received")
	intro := tr(lang,
		"O teste terminou e a subscrição passou a ser cobrada. Obrigado por continuar connosco.",
		"The trial has ended and the subscription is now being charged. Thanks for staying with us.")
	lines := []Line{{tr(lang, "Plano", "Plan"), plan}, {tr(lang, "Valor", "Amount"), amount}}
	url, label := appURL, tr(lang, "Ver a faturação", "View billing")
	if invoiceURL != "" {
		url, label = invoiceURL, tr(lang, "Ver a fatura", "View the invoice")
	}
	return build(lang, subject, intro, lines, label, url, footerFor(lang))
}

// NewLogin reports a sign-in, with what lets the recipient recognize it (or not): when, where,
// and from which device. It is the notice that lets someone realize their password was stolen.
func NewLogin(lang, email, ip, location, device string, when time.Time, appURL string) Msg {
	subject := tr(lang, "Novo início de sessão na sua conta", "New sign-in to your account")
	intro := tr(lang,
		"Houve um início de sessão na sua conta Beskos. Se foi você, não tem de fazer nada.",
		"Someone signed in to your Beskos account. If it was you, there's nothing to do.")
	if location == "" {
		location = tr(lang, "desconhecida", "unknown")
	}
	lines := []Line{
		{tr(lang, "Conta", "Account"), email},
		{tr(lang, "Data e hora", "Date and time"), when.Format("02/01/2006 15:04") + " UTC"},
		{tr(lang, "Endereço IP", "IP address"), ip},
		{tr(lang, "Localização", "Location"), location},
		{tr(lang, "Aparelho", "Device"), device},
	}
	footer := tr(lang,
		"Se não foi você, mude a palavra-passe já e ative a verificação em dois passos. "+footerFor(lang),
		"If this wasn't you, change your password now and turn on two-step verification. "+footerFor(lang))
	return build(lang, subject, intro, lines, tr(lang, "Rever a segurança", "Review security"), appURL, footer)
}

// EventInvite: calendar invite for somebody OUTSIDE the organization. Somebody inside gets the
// in-app notification and the event shows up in their calendar — sending them e-mail would be noise.
func EventInvite(lang, actor, title, when, location, meetURL string) Msg {
	subject := tr(lang, "Convite: ", "Invitation: ") + title
	intro := tr(lang,
		html.EscapeString(actor)+" convidou-o para um evento.",
		html.EscapeString(actor)+" invited you to an event.")
	lines := []Line{{tr(lang, "Quando", "When"), when}}
	if location != "" {
		lines = append(lines, Line{tr(lang, "Onde", "Where"), location})
	}
	cta, url := "", ""
	if meetURL != "" {
		cta, url = tr(lang, "Entrar na videochamada", "Join the video call"), meetURL
		lines = append(lines, Line{tr(lang, "Videochamada", "Video call"), meetURL})
	}
	footer := tr(lang,
		"Recebeu este convite porque foi indicado como participante.",
		"You're getting this invitation because you were listed as an attendee.")
	return build(lang, subject, intro, lines, cta, url, footer)
}

// FileShared: file shared with somebody OUTSIDE the organization — it carries the public link,
// which is the only way to get there without an account. Inside the organization it's an in-app notification.
func FileShared(lang, actor, fileName, linkURL, expires string) Msg {
	subject := tr(lang, "Partilharam um ficheiro consigo", "A file was shared with you")
	intro := tr(lang,
		html.EscapeString(actor)+" partilhou um ficheiro consigo.",
		html.EscapeString(actor)+" shared a file with you.")
	lines := []Line{{tr(lang, "Ficheiro", "File"), fileName}}
	if expires != "" {
		lines = append(lines, Line{tr(lang, "Link válido até", "Link valid until"), expires})
	}
	footer := tr(lang,
		"O link é pessoal — quem o tiver consegue abrir o ficheiro.",
		"The link is personal — anyone who has it can open the file.")
	return build(lang, subject, intro, lines, tr(lang, "Abrir o ficheiro", "Open the file"), linkURL, footer)
}

// PasswordResetRequested: somebody in the organisation cannot sign in and asked for help. It goes
// to the administrators, who are the ones with the button.
//
// IT CARRIES NO LINK that resets anything: the administrator goes to the panel, where they are
// already signed in. An e-mail that can reset passwords is an e-mail worth stealing.
func PasswordResetRequested(lang, who, name, appURL string) Msg {
	subject := tr(lang, "Pedido de nova palavra-passe", "Password reset requested") // lang:ok
	intro := tr(lang,
		"Alguém da sua organização não consegue entrar e pediu uma palavra-passe nova. Se reconhece o pedido, defina-lha no painel e entregue-a por um canal que já use com essa pessoa.", // lang:ok
		"Somebody in your organisation cannot sign in and asked for a new password. If you recognise the request, set one in the panel and hand it over through a channel you already use with them.")
	lines := []Line{{tr(lang, "Quem pediu", "Who asked"), who}} // lang:ok
	if name != "" {
		lines = append(lines, Line{tr(lang, "Nome", "Name"), name}) // lang:ok
	}
	footer := tr(lang,
		"Se não reconhece este pedido, não faça nada: sem a sua ação, nada muda. "+footerFor(lang), // lang:ok
		"If you do not recognise this request, do nothing: without your action, nothing changes. "+footerFor(lang))
	return build(lang, subject, intro, lines, tr(lang, "Abrir o painel", "Open the panel"), appURL, footer)
}

// PasswordResetLink: the self-service way back, and only for somebody who set an address OUTSIDE
// their own domain -- typically the owner of a small organisation, with no administrator above them.
func PasswordResetLink(lang, account, url string, minutes int) Msg {
	subject := tr(lang, "Repor a palavra-passe do Beskos", "Reset your Beskos password") // lang:ok
	intro := tr(lang,
		"Recebemos um pedido para repor a palavra-passe desta conta. A ligação abaixo serve uma vez.", // lang:ok
		"We received a request to reset the password for this account. The link below works once.")
	lines := []Line{
		{tr(lang, "Conta", "Account"), account},                                   // lang:ok
		{tr(lang, "Válida durante", "Valid for"), fmt.Sprintf("%d min", minutes)}, // lang:ok
	}
	footer := tr(lang,
		"Se não foi você, ignore este e-mail — a palavra-passe atual continua a funcionar. Se a conta tiver segundo fator, ele continua a ser pedido. "+footerFor(lang), // lang:ok
		"If this was not you, ignore this e-mail — the current password still works. If the account has a second factor, it is still required. "+footerFor(lang))
	return build(lang, subject, intro, lines, tr(lang, "Definir uma nova", "Set a new one"), url, footer) // lang:ok
}
