package connections

import (
	"fmt"

	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/httpx"
)

// TypeSMTP is the connection type of a mail server for sending (ADR 0022, section 7).
const TypeSMTP = "smtp"

var smtpDefinition = connection.Definition{
	Type:        TypeSMTP,
	Label:       "SMTP",
	Description: "A mail server for sending mails",
	Config:      SMTP{},
	Warn: func(with map[string]any) map[string]string {
		// A mail server on the own computer (e.g. a test server) needs no encryption.
		if v, _ := with["tls"].(string); v == "none" && !httpx.IsLoopback(fmt.Sprint(with["host"])) {
			return map[string]string{"tls": "mails are sent unencrypted; use it only for a mail relay in a trusted network"}
		}
		return nil
	},
}

// SMTP are the fields of a connection of type smtp.
type SMTP struct {
	Host     string `json:"host" doc:"Name or address of the mail server" schema:"required,example=smtp.example.com"`
	Port     string `json:"port" doc:"TCP port; empty: 587 with starttls, 465 with tls, 25 with none" schema:"example=587"`
	TLS      string `json:"tls" doc:"Encryption: starttls (port 587), tls (from the start, port 465) or none (only for a relay in a trusted network); the certificate is always checked" schema:"enum=starttls|tls|none,default=starttls"`
	Auth     string `json:"auth" doc:"Sign-in: password (user and password) or none (e.g. a relay that knows the sender by its address); empty: password if user is set" schema:"enum=password|none"`
	User     string `json:"user" doc:"User name for the sign-in, often the mail address" schema:"example=runnel@example.com"`
	Password string `json:"password" doc:"Password of the user; use ${secret:NAME}" schema:"format=runnel-secret"`
	From     string `json:"from" doc:"Sender of the mails if the action names none, e.g. Runnel <runnel@example.com>" schema:"example=runnel@example.com"`
	Timeout  string `json:"timeout" doc:"Longest time to connect and send one mail" schema:"format=runnel-duration,default=1m"`
}
