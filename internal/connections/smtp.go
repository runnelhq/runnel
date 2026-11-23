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
	Host     string `json:"host" doc:"Name or address of the mail server" schema:"required,example=smtp.example.com,group=Server"`
	Port     string `json:"port" doc:"TCP port; empty: 587 with starttls, 465 with tls, 25 with none" schema:"example=587,group=Server"`
	TLS      string `json:"tls" doc:"Encryption: starttls (port 587), tls (from the start, port 465) or none (only for a relay in a trusted network); the certificate is always checked" schema:"enum=starttls|tls|none,default=starttls,group=Security"`
	Auth     string `json:"auth" doc:"Sign-in: password (user and password), oauth2 (an application registered in Microsoft Entra, e.g. for Office 365) or none (e.g. a relay that knows the sender by its address); empty: password if user is set" schema:"enum=password|oauth2|none,group=Sign-in"`
	User     string `json:"user" doc:"User name for the sign-in, often the mail address; with oauth2 the mailbox that sends" schema:"example=runnel@example.com,group=Sign-in"`
	Password string `json:"password" doc:"Password of the user; use ${secret:NAME}" schema:"format=runnel-secret,group=Sign-in"`
	From     string `json:"from" doc:"Sender of the mails if the action names none, e.g. Runnel <runnel@example.com>" schema:"example=runnel@example.com,group=Mail"`
	Timeout  string `json:"timeout" doc:"Longest time to connect and send one mail, including the token request of oauth2" schema:"format=runnel-duration,default=1m,group=More"`

	Tenant       string `json:"tenant" doc:"Tenant in Microsoft Entra (ID or domain, e.g. contoso.onmicrosoft.com); gives the token address" schema:"example=contoso.onmicrosoft.com,only=auth:oauth2,group=Sign-in"`
	ClientID     string `json:"clientId" doc:"Application (client) ID of the registered application" schema:"example=00000000-0000-0000-0000-000000000000,only=auth:oauth2,group=Sign-in"`
	ClientSecret string `json:"clientSecret" doc:"Client secret of the application; use ${secret:NAME}" schema:"format=runnel-secret,only=auth:oauth2,group=Sign-in"`
	Scope        string `json:"scope" doc:"Scope of the token" schema:"default=https://outlook.office365.com/.default,only=auth:oauth2,group=Sign-in"`
	TokenURL     string `json:"tokenUrl" doc:"Token address of another provider (https://); empty: that of the tenant at Microsoft" schema:"example=https://login.example.com/oauth2/token,only=auth:oauth2,group=Sign-in"`
}

// DefaultSMTPScope is the scope of the token for sending mails through Office 365.
const DefaultSMTPScope = "https://outlook.office365.com/.default"
