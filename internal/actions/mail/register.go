// Package mail contains the actions of the namespace "mail" (ADR 0022, section 7):
// mail.send sends mails through the mail server of an smtp connection (S62a).
//
// Each action lives in a file of its own (<name>.go) with its tests next to it;
// message.go builds the mail (MIME), smtp.go speaks to the mail server.
package mail

import "github.com/runnelhq/runnel/internal/action"

// Register adds all actions of the namespace "mail" to a registry.
func Register(r *action.Registry) {
	r.Register(sendDefinition)
}
