package connections

import "github.com/runnelhq/runnel/internal/connection"

// TypeSFTP is the connection type of an SFTP server for transfer.* (ADR 0022, section 6).
const TypeSFTP = "sftp"

var sftpDefinition = connection.Definition{
	Type:        TypeSFTP,
	Label:       "SFTP",
	Description: "A server for file transfer over SSH",
	Config:      SFTP{},
	Example: example("partner-sftp", "SFTP server of the partner", TypeSFTP,
		"  host: sftp.example.com\n  hostKey: SHA256:ExampleFingerprintOfTheServerHostKeyAbcdefg\n  user: runnel\n"+
			"  privateKey: ${secret:partner-sftp-key}\n"),
	Errors: "Fails without hostKey, naming the fingerprint of the server to check with its administrator, and when " +
		"the host key does not match hostKey; Runnel never trusts a host key on first use. Also fails when the " +
		"server does not answer within timeout, the sign-in is refused (user, password or key), neither password " +
		"nor privateKey is set, privateKey is no PEM key or needs passphrase, or the server offers no SFTP.",
	Test: "Connects, checks the host key against hostKey, signs in and lists the start folder; without hostKey it " +
		"names the fingerprint of the server. Nothing is written.",
	Warn: func(with map[string]any) map[string]string {
		// Without the fingerprint no run connects; Test connection shows the one of the server.
		if v, _ := with["hostKey"].(string); v == "" {
			return map[string]string{"hostKey": "without hostKey runs cannot connect; Test connection shows the fingerprint of the server to check"}
		}
		return nil
	},
}

// SFTP are the fields of a connection of type sftp.
type SFTP struct {
	Host       string `json:"host" doc:"Name or address of the server" schema:"required,example=sftp.example.com,group=Server"`
	Port       string `json:"port" doc:"TCP port; empty: 22" schema:"example=22,group=Server"`
	HostKey    string `json:"hostKey" doc:"Fingerprint of the host key as ssh-keygen -l shows it, e.g. SHA256:…; several separated by spaces. Without it Runnel does not connect and names the fingerprint of the server to check" schema:"example=SHA256:ExampleFingerprintOfTheServerHostKeyAbcdefg,group=Security"`
	User       string `json:"user" doc:"User name for the sign-in" schema:"required,example=runnel,group=Sign-in"`
	Password   string `json:"password" doc:"Password of the user; use ${secret:NAME}" schema:"format=runnel-secret,group=Sign-in"`
	PrivateKey string `json:"privateKey" doc:"Private key in PEM format (OpenSSH, PKCS#1 or PKCS#8), instead of or together with password; use ${secret:NAME}" schema:"format=runnel-secret,group=Sign-in"`
	Passphrase string `json:"passphrase" doc:"Passphrase of a protected privateKey; use ${secret:NAME}" schema:"format=runnel-secret,group=Sign-in"`
	Timeout    string `json:"timeout" doc:"Longest time to connect and sign in" schema:"format=runnel-duration,default=15s,group=More"`
}
