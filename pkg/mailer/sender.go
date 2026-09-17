package mailer

type Sender interface {
	SendOTP(to, code, appName string) error
}
