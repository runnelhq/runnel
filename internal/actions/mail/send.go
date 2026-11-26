package mail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/paths"
)

// mail.send sends a mail through the mail server of an smtp connection (ADR 0022,
// section 7): text, HTML or both, with attachments from files. Output: messageId,
// recipients and size.
var sendDefinition = action.Definition{
	Type:        "mail.send",
	Category:    action.CategoryNetwork,
	Label:       "Send Mail",
	Description: "Sends a mail through a mail server, with text or HTML and attachments",
	Example:     "connection: mail\nto: ${ADMIN_MAIL}\nsubject: \"Import failed: {{ run.id }}\"\nbody: \"{{ run.lastError }}\"\n",
	Config:      sendAction{},
	New:         newSend,
	Reference: action.Reference{
		Outputs: []action.OutputField{
			{Name: "output.messageId", Type: "string", Description: "Message-ID of the mail"},
			{Name: "output.recipients", Type: "integer", Description: "Number of recipients in to, cc and bcc"},
			{Name: "output.size", Type: "integer", Description: "Size of the mail in bytes"},
		},
		Errors: "Fails when the mail has no sender or recipient, an address or attachment is invalid, the attachments are too large, the server cannot be reached within the timeout of the connection, refuses the sign-in or a recipient, or accepts no mail of this size.",
		Limits: []string{
			"Attachments may be at most 25 MB together.",
			"The timeout of the connection limits connecting and sending (default 1m).",
		},
	},
}

type sendAction struct {
	Connection  string   `json:"connection" doc:"Connection of the mail server (a file in connections/)" schema:"required,connection=smtp,example=mail"`
	From        string   `json:"from" doc:"Sender, e.g. Runnel <runnel@example.com>; empty: from of the connection" schema:"example=runnel@example.com"`
	To          string   `json:"to" doc:"Recipients, separated by commas, e.g. ops@example.com, Ada <ada@example.com>" schema:"example=ops@example.com"`
	Cc          string   `json:"cc" doc:"Recipients of a copy, separated by commas" schema:"example=team@example.com"`
	Bcc         string   `json:"bcc" doc:"Recipients of a blind copy, separated by commas; they do not appear in the mail" schema:"example=audit@example.com"`
	ReplyTo     string   `json:"replyTo" doc:"Address for replies, if not the sender" schema:"example=support@example.com"`
	Subject     string   `json:"subject" doc:"Subject; line breaks become spaces" schema:"required,example=Import finished"`
	Body        string   `json:"body" doc:"Text of the mail"`
	HTML        string   `json:"html" doc:"Text of the mail as HTML; with body, mail programs show the HTML and keep the text as an alternative"`
	Attachments []string `json:"attachments" doc:"Files to attach, relative to the project folder; at most 25 MB together"`
}

func newSend(with map[string]any) (action.Action, error) {
	a := &sendAction{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	if !dynamic(a.To) && !dynamic(a.Cc) && !dynamic(a.Bcc) && strings.TrimSpace(a.To+a.Cc+a.Bcc) == "" {
		return nil, errors.New("a mail needs at least one recipient in to, cc or bcc")
	}
	for field, value := range map[string]string{"from": a.From, "to": a.To, "cc": a.Cc, "bcc": a.Bcc, "replyTo": a.ReplyTo} {
		if !dynamic(value) {
			if _, err := addresses(field, value); err != nil {
				return nil, err
			}
		}
	}
	return a, nil
}

func dynamic(s string) bool {
	return strings.Contains(s, "{{") || strings.Contains(s, "${")
}

// addresses reads a list of addresses separated by commas; empty is none.
func addresses(field, list string) ([]*mail.Address, error) {
	if strings.TrimSpace(list) == "" {
		return nil, nil
	}
	out, err := mail.ParseAddressList(list)
	if err != nil {
		return nil, fmt.Errorf("%s: %q is not a list of mail addresses, e.g. ops@example.com, Ada <ada@example.com>", field, list)
	}
	return out, nil
}

func (a *sendAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	if ec.Connection == nil {
		return action.Result{}, connection.ErrNotAvailable
	}
	conn, err := ec.Connection(ctx, a.Connection)
	if err != nil {
		return action.Result{}, err
	}
	if conn.Type != connections.TypeSMTP {
		return action.Result{}, fmt.Errorf("connection %s has type %s, not smtp", conn.Name, conn.Type)
	}
	var cfg connections.SMTP
	if err := connections.Builtin().Decode(conn, &cfg); err != nil {
		return action.Result{}, err
	}
	srv, err := newServer(conn.Name, cfg)
	if err != nil {
		return action.Result{}, fmt.Errorf("connection %s: %w", conn.Name, err)
	}
	msg, err := a.message(ctx, cfg.From)
	if err != nil {
		return action.Result{}, err
	}
	data, id, err := msg.build()
	if err != nil {
		return action.Result{}, err
	}
	to := msg.recipients()
	ec.Logf(slog.LevelDebug, "sending mail %s to %s", id, strings.Join(to, ", "))
	if err := srv.send(ctx, msg.From.Address, to, data); err != nil {
		return action.Result{}, err
	}
	ec.Logf(slog.LevelInfo, "sent mail to %d %s", len(to), plural(len(to), "recipient"))
	return action.Result{Output: map[string]any{"messageId": id, "recipients": int64(len(to)), "size": int64(len(data))}}, nil
}

// message reads the fields of the action into a mail; defaultFrom is from of the connection.
func (a *sendAction) message(ctx context.Context, defaultFrom string) (*message, error) {
	m := &message{Subject: oneLine(a.Subject), Text: a.Body, HTML: a.HTML, Date: time.Now()}
	from := a.From
	if strings.TrimSpace(from) == "" {
		from = defaultFrom
	}
	senders, err := addresses("from", from)
	switch {
	case err != nil:
		return nil, err
	case len(senders) == 0:
		return nil, errors.New("the mail has no sender: set from in the action or in the connection")
	case len(senders) > 1:
		return nil, fmt.Errorf("from: %q names more than one sender", from)
	}
	m.From = senders[0]
	for _, f := range []struct {
		name, value string
		list        *[]*mail.Address
	}{{"to", a.To, &m.To}, {"cc", a.Cc, &m.Cc}, {"bcc", a.Bcc, &m.Bcc}, {"replyTo", a.ReplyTo, &m.ReplyTo}} {
		if *f.list, err = addresses(f.name, f.value); err != nil {
			return nil, err
		}
	}
	if len(m.recipients()) == 0 {
		return nil, errors.New("the mail has no recipient: to, cc and bcc are empty")
	}
	for _, file := range a.Attachments {
		if strings.TrimSpace(file) == "" {
			return nil, errors.New("attachments: empty file name")
		}
		m.Attachments = append(m.Attachments, paths.Resolve(ctx, file))
	}
	return m, nil
}

// oneLine replaces line breaks, so that a subject from an expression (e.g. an error
// message) cannot add headers.
func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("\r", " ", "\n", " ").Replace(s)), " ")
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
