package mailer

import (
	"bytes"
	"fmt"
	"html"
	"net/smtp"
	"starter-kit/utils"
	"strconv"
	"strings"
	"time"
)

const defaultSMTPPort = 587

type BrevoSender struct {
	Host         string
	Port         int
	User         string
	Pass         string
	From         string
	Subject      string
	ResetSubject string
	TTL          time.Duration
	AppName      string
}

func NewBrevoSenderFromEnv() (*BrevoSender, error) {
	port := defaultSMTPPort
	if value := utils.GetEnv("SMTP_PORT", ""); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			port = parsed
		}
	}

	host := utils.GetEnv("SMTP_HOST", "")
	user := utils.GetEnv("SMTP_USER", "")
	pass := utils.GetEnv("SMTP_PASS", "")
	from := utils.GetEnv("SMTP_FROM", "")
	if host == "" || pass == "" || from == "" {
		return nil, fmt.Errorf("smtp credentials not configured")
	}
	if user == "" {
		user = "apikey"
	}

	subject := utils.GetEnv("SMTP_SUBJECT", "")
	if subject == "" {
		subject = "Your Registration OTP"
	}

	resetSubject := utils.GetEnv("RESET_SUBJECT", "")
	if resetSubject == "" {
		resetSubject = "Reset Your Password"
	}

	appName := utils.GetEnv("AUTH_EMAIL_APP_NAME", "")
	if appName == "" {
		appName = "Account Verification"
	}

	return &BrevoSender{
		Host:         host,
		Port:         port,
		User:         user,
		Pass:         pass,
		From:         from,
		Subject:      subject,
		ResetSubject: resetSubject,
		TTL:          utils.DurationFromEnv([]string{"OTP_TTL", "OTP_TTL_SECONDS"}, 5*time.Minute),
		AppName:      appName,
	}, nil
}

func (s *BrevoSender) SendOTP(to, code, appName string) error {
	if strings.TrimSpace(appName) == "" {
		appName = s.AppName
	}
	msg := buildOTPMessage(s.From, to, s.Subject, appName, code, s.TTL)
	return s.send(to, msg)
}

func (s *BrevoSender) SendPasswordReset(to, token, appName, resetURL string, ttl time.Duration) error {
	if strings.TrimSpace(appName) == "" {
		appName = s.AppName
	}
	subject := s.ResetSubject
	if subject == "" {
		subject = "Reset Your Password"
	}
	msg := buildPasswordResetMessage(s.From, to, subject, appName, token, resetURL, ttl)
	return s.send(to, msg)
}

func (s *BrevoSender) send(to string, msg []byte) error {
	addr := fmt.Sprintf("%s:%d", s.Host, s.Port)
	auth := smtp.PlainAuth("", s.User, s.Pass, s.Host)
	return smtp.SendMail(addr, auth, extractEmail(s.From), []string{to}, msg)
}

func buildOTPMessage(from, to, subject, appName, code string, ttl time.Duration) []byte {
	minutes := int(ttl.Minutes())
	if minutes <= 0 {
		minutes = 5
	}

	safeAppName := html.EscapeString(appName)
	textBody := fmt.Sprintf("Your registration verification code is: %s\nThis code expires in %d minutes.\nIf you did not register, ignore this email.\n", code, minutes)
	htmlBody := fmt.Sprintf(`<p>Hello,</p><p>Use this code to complete your %s registration:</p><h1 style="letter-spacing:6px">%s</h1><p>This code expires in %d minutes.</p><p>If you did not register, ignore this email.</p>`, safeAppName, html.EscapeString(code), minutes)
	return buildMultipartMessage(from, to, subject, textBody, htmlBody)
}

func buildPasswordResetMessage(from, to, subject, appName, token, resetURL string, ttl time.Duration) []byte {
	minutes := int(ttl.Minutes())
	if minutes <= 0 {
		minutes = 15
	}

	safeAppName := html.EscapeString(appName)
	textBody := fmt.Sprintf("We received a password reset request for %s.\n", safeAppName)
	actionHTML := fmt.Sprintf(`<p>Use this reset token:</p><p style="word-break:break-all">%s</p>`, html.EscapeString(token))
	if strings.TrimSpace(resetURL) != "" {
		textBody += fmt.Sprintf("Reset link: %s\n", resetURL)
		actionHTML = fmt.Sprintf(`<p><a href="%s">Reset Password</a></p><p>If the link does not work, copy this URL:<br><span style="word-break:break-all">%s</span></p>`, html.EscapeString(resetURL), html.EscapeString(resetURL))
	} else {
		textBody += fmt.Sprintf("Reset token: %s\n", token)
	}
	textBody += fmt.Sprintf("This request expires in %d minutes.\nIf you did not request this, ignore this email.\n", minutes)
	htmlBody := fmt.Sprintf(`<p>We received a password reset request for %s.</p>%s<p>This request expires in %d minutes.</p><p>If you did not request this, ignore this email.</p>`, safeAppName, actionHTML, minutes)
	return buildMultipartMessage(from, to, subject, textBody, htmlBody)
}

func buildMultipartMessage(from, to, subject, textBody, htmlBody string) []byte {
	boundary := "starter-kit-mail-boundary"

	var buf bytes.Buffer
	buf.WriteString("From: " + from + "\r\n")
	buf.WriteString("To: " + to + "\r\n")
	buf.WriteString("Subject: " + subject + "\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Content-Type: multipart/alternative; boundary=" + boundary + "\r\n\r\n")
	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	buf.WriteString(textBody + "\r\n")
	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
	buf.WriteString(htmlBody + "\r\n")
	buf.WriteString("--" + boundary + "--")
	return buf.Bytes()
}

func extractEmail(from string) string {
	start := strings.IndexByte(from, '<')
	end := strings.IndexByte(from, '>')
	if start >= 0 && end > start {
		return from[start+1 : end]
	}
	return from
}
