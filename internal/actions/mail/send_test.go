package mail

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"maps"
	"mime"
	"mime/multipart"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions/actiontest"
	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/paths"
)

// send runs mail.send with the connection "mail" of the given fields.
func send(ctx context.Context, t *testing.T, conn map[string]any, with map[string]any) (map[string]any, []string, error) {
	t.Helper()
	with["connection"] = "mail"
	a, err := newSend(with)
	if err != nil {
		return nil, nil, err
	}
	var logs []string
	ec := &action.ExecContext{
		Log: func(_ slog.Level, msg string) { logs = append(logs, msg) },
		Connection: func(_ context.Context, name string) (connection.Resolved, error) {
			if name != "mail" {
				return connection.Resolved{}, errors.New("unknown")
			}
			return connection.Resolved{Name: name, Type: connections.TypeSMTP, With: conn}, nil
		},
	}
	res, err := a.Execute(ctx, ec)
	out, _ := res.Output.(map[string]any)
	return out, logs, err
}

func TestSendStartTLSWithAttachment(t *testing.T) {
	srv := &testServer{tls: certificate(t), ext: []string{"STARTTLS", "AUTH PLAIN LOGIN", "SIZE 1000000"}}
	srv.start(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "report.csv"), []byte("id;name\r\n1;Ada\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := paths.WithBase(context.Background(), dir)
	conn := map[string]any{"host": "127.0.0.1", "port": srv.port(), "user": "runnel@example.com", "password": "secret-pw", "from": "Runnel <runnel@example.com>"}
	out, logs, err := send(ctx, t, conn, map[string]any{
		"to": "ops@example.com, Ada Lovelace <ada@example.com>", "cc": "team@example.com", "bcc": "audit@example.com, OPS@example.com",
		"subject": "Bericht für März\r\nBcc: evil@example.com", "body": "Hallo,\nanbei der Bericht.\n", "attachments": []any{"report.csv"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if logs[len(logs)-1] != "sent mail to 4 recipients" || out["recipients"] != int64(4) || !strings.HasPrefix(out["messageId"].(string), "<") {
		t.Errorf("%v %v", logs, out)
	}
	from, rcpt, data := srv.received()
	if from != "runnel@example.com" || strings.Join(rcpt, " ") != "ops@example.com ada@example.com team@example.com audit@example.com" {
		t.Errorf("envelope %s %v", from, rcpt)
	}
	if !srv.tlsUsed || srv.mech != "PLAIN" || srv.user != "runnel@example.com" {
		t.Errorf("tls %v, auth %s %s", srv.tlsUsed, srv.mech, srv.user)
	}
	msg, err := mail.ReadMessage(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	subject, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if subject != "Bericht für März Bcc: evil@example.com" || msg.Header.Get("Bcc") != "" || msg.Header.Get("Auto-Submitted") != "auto-generated" {
		t.Errorf("headers %v", msg.Header)
	}
	if msg.Header.Get("To") != `<ops@example.com>, "Ada Lovelace" <ada@example.com>` || msg.Header.Get("Message-Id") != out["messageId"] {
		t.Errorf("To %q, Message-ID %q", msg.Header.Get("To"), msg.Header.Get("Message-Id"))
	}
	parts := readParts(t, msg)
	if len(parts) != 2 || parts[0].body != "Hallo,\r\nanbei der Bericht.\r\n" || parts[1].body != "id;name\r\n1;Ada\r\n" {
		t.Fatalf("parts %q", parts)
	}
	if parts[1].filename != "report.csv" || !strings.HasPrefix(parts[1].typ, "text/csv") {
		t.Errorf("attachment %+v", parts[1])
	}
}

type part struct{ typ, filename, body string }

// readParts returns the parts of a multipart mail, decoded.
func readParts(t *testing.T, msg *mail.Message) []part {
	t.Helper()
	typ, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(typ, "multipart/") {
		t.Fatalf("content type %s %v", typ, err)
	}
	var out []part
	r := multipart.NewReader(msg.Body, params["boundary"])
	for {
		p, err := r.NextPart()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(p)
		if err != nil {
			t.Fatal(err)
		}
		if p.Header.Get("Content-Transfer-Encoding") == "base64" {
			if body, err = base64.StdEncoding.DecodeString(strings.ReplaceAll(string(body), "\r\n", "")); err != nil {
				t.Fatal(err)
			}
		}
		out = append(out, part{typ: p.Header.Get("Content-Type"), filename: p.FileName(), body: string(body)})
	}
}

func TestSendTextAndHTMLOverTLSWithLogin(t *testing.T) {
	srv := &testServer{tls: certificate(t), implicit: true, ext: []string{"AUTH LOGIN"}}
	srv.start(t)
	conn := map[string]any{"host": "127.0.0.1", "port": srv.port(), "tls": "tls", "user": "runnel", "password": "secret-pw"}
	_, _, err := send(context.Background(), t, conn, map[string]any{
		"from": "runnel@example.com", "to": "ops@example.com", "subject": "Done", "body": "Plain", "html": "<p>Grüße</p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !srv.tlsUsed || srv.mech != "LOGIN" || srv.user != "runnel" || srv.password != "secret-pw" {
		t.Errorf("tls %v, auth %s %s", srv.tlsUsed, srv.mech, srv.user)
	}
	_, _, data := srv.received()
	msg, err := mail.ReadMessage(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	parts := readParts(t, msg)
	if !strings.HasPrefix(msg.Header.Get("Content-Type"), "multipart/alternative") || len(parts) != 2 ||
		parts[0].body != "Plain" || parts[1].body != "<p>Grüße</p>" || !strings.HasPrefix(parts[1].typ, "text/html") {
		t.Errorf("%s %+v", msg.Header.Get("Content-Type"), parts)
	}
}

func TestSendRelayWithoutTLS(t *testing.T) {
	srv := &testServer{}
	srv.start(t)
	conn := map[string]any{"host": "127.0.0.1", "port": srv.port(), "tls": "none", "from": "runnel@example.com"}
	_, _, err := send(context.Background(), t, conn, map[string]any{"to": "ops@example.com", "subject": "Hi", "html": "<b>Hi</b>"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, data := srv.received()
	msg, _ := mail.ReadMessage(strings.NewReader(data))
	if srv.tlsUsed || srv.mech != "" || msg.Header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("tls %v, auth %q, %v", srv.tlsUsed, srv.mech, msg.Header)
	}
}

func TestSendRejections(t *testing.T) {
	tlsConfig := certificate(t)
	cases := []struct {
		name   string
		srv    *testServer
		conn   map[string]any
		with   map[string]any
		want   string
		noData bool
	}{
		{"no STARTTLS", &testServer{}, nil, nil, "does not offer STARTTLS", true},
		{"wrong password", &testServer{tls: tlsConfig, ext: []string{"STARTTLS", "AUTH PLAIN"}}, map[string]any{"user": "u", "password": "wrong"}, nil, "sign-in failed: 535", true},
		{"no sign-in offered", &testServer{tls: tlsConfig, ext: []string{"STARTTLS"}}, map[string]any{"user": "u", "password": "secret-pw"}, nil, "does not offer a sign-in", true},
		{"recipient refused", &testServer{tls: tlsConfig, ext: []string{"STARTTLS"}, rejectRcp: "nobody@example.com"}, nil, map[string]any{"to": "nobody@example.com"}, "recipient nobody@example.com: 550", true},
		{"too large", &testServer{tls: tlsConfig, ext: []string{"STARTTLS", "SIZE 100"}}, nil, nil, "the server accepts at most 0 KB", true},
		{"no answer", &testServer{silent: true}, map[string]any{"timeout": "200ms"}, nil, "no answer from 127.0.0.1:", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.srv.start(t)
			conn := map[string]any{"host": "127.0.0.1", "port": c.srv.port(), "from": "runnel@example.com"}
			maps.Copy(conn, c.conn)
			with := map[string]any{"to": "ops@example.com", "subject": "Hi", "body": "text"}
			maps.Copy(with, c.with)
			_, _, err := send(context.Background(), t, conn, with)
			if err == nil || !strings.Contains(err.Error(), c.want) || !strings.HasPrefix(err.Error(), "connection mail: ") {
				t.Fatalf("error %v, want %q", err, c.want)
			}
			if strings.Contains(err.Error(), "secret-pw") {
				t.Error("password in the message")
			}
			if _, _, data := c.srv.received(); c.noData && data != "" {
				t.Error("mail was sent")
			}
		})
	}
}

func TestSendUntrustedCertificate(t *testing.T) {
	srv := &testServer{tls: certificate(t), ext: []string{"STARTTLS"}}
	srv.start(t)
	rootCAs = nil // the system does not know the certificate of the test
	conn := map[string]any{"host": "127.0.0.1", "port": srv.port(), "from": "runnel@example.com"}
	_, _, err := send(context.Background(), t, conn, map[string]any{"to": "ops@example.com", "subject": "Hi"})
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("error %v", err)
	}
}

func TestSendCancelled(t *testing.T) {
	srv := &testServer{silent: true}
	srv.start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	conn := map[string]any{"host": "127.0.0.1", "port": srv.port(), "from": "runnel@example.com"}
	start := time.Now()
	_, _, err := send(ctx, t, conn, map[string]any{"to": "ops@example.com", "subject": "Hi"})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 5*time.Second {
		t.Fatalf("error %v after %s", err, time.Since(start))
	}
}

func TestSendConfigErrors(t *testing.T) {
	actiontest.ConfigError(t, sendDefinition, map[string]any{"connection": "mail", "subject": "Hi"}, "at least one recipient")
	actiontest.ConfigError(t, sendDefinition, map[string]any{"connection": "mail", "subject": "Hi", "to": "not an address"}, `to: "not an address" is not a list of mail addresses`)
	actiontest.ConfigError(t, sendDefinition, map[string]any{"connection": "mail", "to": "a@example.com"}, `missing required field "subject"`)
	actiontest.New(t, sendDefinition, map[string]any{"connection": "mail", "subject": "Hi", "to": "${ADMIN_MAIL}"})
}

func TestSendRunErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "big.bin"), make([]byte, maxAttachments+1), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := paths.WithBase(context.Background(), dir)
	local := map[string]any{"host": "127.0.0.1", "port": "1", "from": "runnel@example.com"}
	cases := []struct {
		conn, with map[string]any
		want       string
	}{
		{map[string]any{"host": "127.0.0.1"}, map[string]any{"to": "a@example.com"}, "the mail has no sender"},
		{local, map[string]any{"to": "a@example.com", "from": "a@example.com, b@example.com"}, "names more than one sender"},
		{local, map[string]any{"to": "{{ x }}"}, `to: "{{ x }}" is not a list`},
		{local, map[string]any{"to": "a@example.com", "attachments": []any{"missing.csv"}}, "attachment: open"},
		{local, map[string]any{"to": "a@example.com", "attachments": []any{"big.bin"}}, "larger than 25 MB together"},
		{map[string]any{"host": "smtp.example.com", "tls": "none", "user": "u", "password": "p"}, map[string]any{"to": "a@example.com"}, "the password would be sent unencrypted"},
		{map[string]any{"host": "smtp.example.com", "auth": "basic"}, map[string]any{"to": "a@example.com"}, "auth basic needs a user"},
		// password is basic since R5b, as in the http connection.
		{map[string]any{"host": "smtp.example.com", "auth": "password", "user": "u"}, map[string]any{"to": "a@example.com"}, `invalid auth "password" (expected basic, oauth2 or none)`},
		{map[string]any{"host": "smtp.example.com", "auth": "plain", "user": "u"}, map[string]any{"to": "a@example.com"}, `invalid auth "plain" (expected basic, oauth2 or none)`},
		{map[string]any{"host": "smtp.example.com", "port": "x"}, map[string]any{"to": "a@example.com"}, `port "x" is not a number`},
	}
	for _, c := range cases {
		c.with["subject"] = "Hi"
		if _, _, err := send(ctx, t, c.conn, c.with); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v %v: error %v, want %q", c.conn, c.with, err, c.want)
		}
	}
}

func TestConnectionWarning(t *testing.T) {
	r := connections.Builtin()
	if w := r.Warnings(connections.TypeSMTP, map[string]any{"host": "relay", "tls": "none"}); !strings.Contains(w["tls"], "unencrypted") {
		t.Errorf("%v", w)
	}
	if !r.SecretField(connections.TypeSMTP, "password") {
		t.Error("password is not a secret field")
	}
}

// Check signs in without sending a mail (test of the editor, S39c1).
func TestCheck(t *testing.T) {
	srv := &testServer{tls: certificate(t), ext: []string{"STARTTLS", "AUTH PLAIN"}}
	srv.start(t)
	conn := func(pw string) connection.Resolved {
		return connection.Resolved{Name: "mail", Type: connections.TypeSMTP, With: map[string]any{"host": "127.0.0.1", "port": srv.port(), "user": "runnel", "password": pw}}
	}
	msg, err := Check(context.Background(), conn("secret-pw"))
	if err != nil || msg != "signed in to 127.0.0.1:"+srv.port()+" as runnel (starttls)" {
		t.Fatalf("%q %v", msg, err)
	}
	if from, rcpt, data := srv.received(); from != "" || rcpt != nil || data != "" {
		t.Errorf("a mail was sent: %s %v", from, rcpt)
	}
	if _, err := Check(context.Background(), conn("wrong")); err == nil || !strings.Contains(err.Error(), "sign-in failed: 535") {
		t.Errorf("error %v", err)
	}
}
