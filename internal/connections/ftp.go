package connections

import "github.com/runnelhq/runnel/internal/connection"

// TypeFTPS and TypeFTP are the connection types of an FTP server for transfer.* (ADR 0022,
// section 6): with TLS, or without it only with insecure: true.
const (
	TypeFTPS = "ftps"
	TypeFTP  = "ftp"
)

var ftpsDefinition = connection.Definition{
	Type:        TypeFTPS,
	Label:       "FTPS",
	Description: "A server for file transfer over FTP with TLS",
	Config:      FTPS{},
	Example: example("partner-ftps", "FTPS server of the partner", TypeFTPS,
		"  host: ftp.example.com\n  tls: explicit\n  user: runnel\n  password: ${secret:partner-ftps-password}\n"),
	Errors: "Fails when the certificate of the server is not trusted by the system and fingerprint is not set, " +
		"naming the SHA256 fingerprint of the certificate to check with the administrator of the server, and when " +
		"the certificate does not match fingerprint. Also fails when the server does not answer within timeout, " +
		"does not offer AUTH TLS (tls: explicit) or refuses the sign-in.",
	Test: "Connects, starts TLS, checks the certificate (trusted by the system or matching fingerprint), signs in " +
		"and lists the start folder. Nothing is written.",
}

var ftpDefinition = connection.Definition{
	Type:        TypeFTP,
	Label:       "FTP",
	Description: "A server for file transfer over FTP without encryption; only with insecure: true",
	Config:      FTP{},
	Example: example("legacy-ftp", "Old FTP server in the plant network", TypeFTP,
		"  host: ftp.plant.local\n  insecure: true\n  user: runnel\n  password: ${secret:legacy-ftp-password}\n"),
	Errors: "Fails without insecure: true (use ftps if the server offers TLS), and when the server does not answer " +
		"within timeout or refuses the sign-in (user or password).",
	Test: "Connects without encryption (only with insecure: true), signs in and lists the start folder. Nothing is " +
		"written.",
	Warn: func(with map[string]any) map[string]string {
		if v, _ := with["insecure"].(bool); !v {
			return map[string]string{"insecure": "without insecure: true runs cannot connect; use ftps if the server offers TLS"}
		}
		return map[string]string{"insecure": "the password and all data travel unencrypted and can be read on the way; use ftps if the server offers TLS"}
	},
}

// FTPS are the fields of a connection of type ftps.
type FTPS struct {
	Host        string `json:"host" doc:"Name or address of the server" schema:"required,example=ftp.example.com,group=Server"`
	Port        string `json:"port" doc:"TCP port; empty: 21 with explicit, 990 with implicit" schema:"example=21,group=Server"`
	TLS         string `json:"tls" doc:"How TLS starts: explicit (AUTH TLS on the usual port 21) or implicit (from the start, port 990)" schema:"enum=explicit|implicit,default=explicit,group=Security"`
	ServerName  string `json:"serverName" doc:"Name in the certificate of the server if it differs from host, e.g. when host is an address" schema:"example=ftp.example.com,group=Security"`
	Fingerprint string `json:"fingerprint" doc:"SHA256 fingerprint of the certificate of a server with its own (self-signed) certificate, e.g. AB:CD:…; several separated by spaces. With it Runnel accepts only this certificate; without it the certificate must be trusted by Windows" schema:"example=AB:CD:EF:01:23:45:67:89:AB:CD:EF:01:23:45:67:89:AB:CD:EF:01:23:45:67:89:AB:CD:EF:01:23:45:67:89,group=Security"`
	User        string `json:"user" doc:"User name for the sign-in" schema:"required,example=runnel,group=Sign-in"`
	Password    string `json:"password" doc:"Password of the user; use ${secret:NAME}" schema:"format=runnel-secret,group=Sign-in"`
	Timeout     string `json:"timeout" doc:"Longest time to connect and sign in" schema:"format=runnel-duration,default=15s,group=More"`
}

// FTP are the fields of a connection of type ftp.
type FTP struct {
	Host     string `json:"host" doc:"Name or address of the server" schema:"required,example=ftp.example.com,group=Server"`
	Port     string `json:"port" doc:"TCP port; empty: 21" schema:"example=21,group=Server"`
	Insecure bool   `json:"insecure" doc:"Required: FTP sends the password and all data unencrypted; set it only for a server without TLS in a trusted network. validate warns" schema:"default=false,group=Security"`
	User     string `json:"user" doc:"User name for the sign-in" schema:"required,example=runnel,group=Sign-in"`
	Password string `json:"password" doc:"Password of the user; use ${secret:NAME}" schema:"format=runnel-secret,group=Sign-in"`
	Timeout  string `json:"timeout" doc:"Longest time to connect and sign in" schema:"format=runnel-duration,default=15s,group=More"`
}
