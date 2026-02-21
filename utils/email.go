package utils

import (
	"crypto/tls"
	"errors"
	"net/smtp"
	"os"
	"strings"

	"github.com/jordan-wright/email"
)

// SendActivateMail sends activation email.
func SendActivateMail(to, link string) error {
	host := os.Getenv("SMTP_HOST")
	port := os.Getenv("SMTP_PORT")
	user := os.Getenv("SMTP_USER")
	pass := os.Getenv("SMTP_PASS")
	from := os.Getenv("SMTP_FROM")
	if host == "" || port == "" || user == "" || pass == "" || from == "" {
		return errors.New("smtp config missing")
	}

	e := email.NewEmail()
	e.From = from
	e.To = []string{to}
	e.Subject = "Account Activation"
	e.HTML = []byte(`
		<h2>Welcome</h2>
		<p>Please click the link below to activate your account:</p>
		<a href="` + link + `">Activate account</a>
		<p>The link is valid for 10 minutes.</p>
	`)

	addr := host + ":" + port
	auth := smtp.PlainAuth("", user, pass, host)
	// tls STARTTLS 可选
	tlsConfig := &tls.Config{ServerName: host}
	useTLS := strings.EqualFold(os.Getenv("SMTP_TLS"), "true") ||
		os.Getenv("SMTP_TLS") == "1" ||
		port == "465"
	useStartTLS := strings.EqualFold(os.Getenv("SMTP_STARTTLS"), "true") ||
		os.Getenv("SMTP_STARTTLS") == "1"

	if useTLS {
		return e.SendWithTLS(addr, auth, tlsConfig)
	}
	if useStartTLS {
		return e.SendWithStartTLS(addr, auth, tlsConfig)
	}
	return e.Send(addr, auth)
}
