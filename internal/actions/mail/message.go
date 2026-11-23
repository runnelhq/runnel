package mail

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxAttachments limits the size of all attachments of a mail together; mail servers
// accept 25 to 35 MB per mail, and the message is built in memory.
const maxAttachments = 25 << 20

// message is a mail ready to be written in the format of RFC 5322 with MIME parts.
type message struct {
	From        *mail.Address
	To, Cc, Bcc []*mail.Address
	ReplyTo     []*mail.Address
	Subject     string
	Text, HTML  string
	Attachments []string // paths of files
	Date        time.Time
}

// recipients returns the addresses of the envelope: To, Cc and Bcc, each once.
func (m *message) recipients() []string {
	var out []string
	seen := map[string]bool{}
	for _, list := range [][]*mail.Address{m.To, m.Cc, m.Bcc} {
		for _, a := range list {
			if key := strings.ToLower(a.Address); !seen[key] {
				seen[key] = true
				out = append(out, a.Address)
			}
		}
	}
	return out
}

// build writes the mail with its headers; Bcc is not among them. It returns the mail and
// its Message-ID.
func (m *message) build() ([]byte, string, error) {
	var b bytes.Buffer
	id := messageID(m.From.Address)
	header := func(name, value string) { fmt.Fprintf(&b, "%s: %s\r\n", name, value) }
	header("From", m.From.String())
	if len(m.To) > 0 {
		header("To", addressList(m.To))
	}
	if len(m.Cc) > 0 {
		header("Cc", addressList(m.Cc))
	}
	if len(m.ReplyTo) > 0 {
		header("Reply-To", addressList(m.ReplyTo))
	}
	header("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	header("Date", m.Date.Format(time.RFC1123Z))
	header("Message-ID", id)
	header("MIME-Version", "1.0")
	// Replies such as out-of-office notices are not sent to automatic mails (RFC 3834).
	header("Auto-Submitted", "auto-generated")
	head, writeContent := m.content()
	if len(m.Attachments) == 0 {
		for _, k := range []string{"Content-Type", "Content-Transfer-Encoding"} {
			if v := head.Get(k); v != "" {
				header(k, v)
			}
		}
		b.WriteString("\r\n")
		if err := writeContent(&b); err != nil {
			return nil, "", err
		}
		return b.Bytes(), id, nil
	}
	w := multipart.NewWriter(&b)
	header("Content-Type", mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": w.Boundary()}))
	b.WriteString("\r\n")
	part, err := w.CreatePart(head)
	if err != nil {
		return nil, "", err
	}
	if err := writeContent(part); err != nil {
		return nil, "", err
	}
	var total int64
	for _, file := range m.Attachments {
		n, err := attach(w, file, maxAttachments-total)
		if err != nil {
			return nil, "", err
		}
		total += n
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return b.Bytes(), id, nil
}

// content returns the headers of the content and a function writing it: text, HTML or
// both as alternatives.
func (m *message) content() (textproto.MIMEHeader, func(io.Writer) error) {
	if m.HTML == "" || m.Text == "" {
		typ, text := "text/plain", m.Text
		if m.HTML != "" {
			typ, text = "text/html", m.HTML
		}
		return textproto.MIMEHeader{
			"Content-Type":              {typ + "; charset=utf-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		}, func(w io.Writer) error { return writeQP(w, text) }
	}
	boundary := multipart.NewWriter(io.Discard).Boundary()
	head := textproto.MIMEHeader{"Content-Type": {mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": boundary})}}
	return head, func(dst io.Writer) error {
		w := multipart.NewWriter(dst)
		if err := w.SetBoundary(boundary); err != nil {
			return err
		}
		for _, p := range []struct{ typ, text string }{{"text/plain", m.Text}, {"text/html", m.HTML}} {
			part, err := w.CreatePart(textproto.MIMEHeader{
				"Content-Type":              {p.typ + "; charset=utf-8"},
				"Content-Transfer-Encoding": {"quoted-printable"},
			})
			if err != nil {
				return err
			}
			if err := writeQP(part, p.text); err != nil {
				return err
			}
		}
		return w.Close()
	}
}

// writeQP writes text as quoted-printable with CRLF line ends.
func writeQP(w io.Writer, text string) error {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
	qp := quotedprintable.NewWriter(w)
	if _, err := io.WriteString(qp, text); err != nil {
		return err
	}
	return qp.Close()
}

// attach adds a file as a part in base64 and returns its size; files larger than limit
// are an error.
func attach(w *multipart.Writer, file string, limit int64) (int64, error) {
	f, err := os.Open(file)
	if err != nil {
		return 0, fmt.Errorf("attachment: %w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return 0, fmt.Errorf("attachment: %w", err)
	}
	if info.IsDir() {
		return 0, fmt.Errorf("attachment %s is a folder, not a file", file)
	}
	if info.Size() > limit {
		return 0, tooLarge()
	}
	name := filepath.Base(file)
	typ, params, err := mime.ParseMediaType(mime.TypeByExtension(filepath.Ext(name)))
	if err != nil {
		typ, params = "application/octet-stream", map[string]string{}
	}
	params["name"] = name
	part, err := w.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {mime.FormatMediaType(typ, params)},
		"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": name})},
		"Content-Transfer-Encoding": {"base64"},
	})
	if err != nil {
		return 0, err
	}
	enc := base64.NewEncoder(base64.StdEncoding, &lineWriter{w: part})
	n, err := io.Copy(enc, io.LimitReader(f, limit+1))
	if err == nil {
		err = enc.Close()
	}
	if err != nil {
		return 0, fmt.Errorf("attachment %s: %w", file, err)
	}
	if n > limit {
		return 0, tooLarge()
	}
	return n, nil
}

func tooLarge() error {
	return fmt.Errorf("the attachments are larger than %d MB together; mail servers do not accept larger mails", maxAttachments>>20)
}

// lineWriter breaks base64 into lines of 76 characters (RFC 2045).
type lineWriter struct {
	w   io.Writer
	col int
}

func (l *lineWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		chunk := min(76-l.col, len(p))
		if _, err := l.w.Write(p[:chunk]); err != nil {
			return 0, err
		}
		p, l.col = p[chunk:], l.col+chunk
		if l.col == 76 {
			if _, err := l.w.Write([]byte("\r\n")); err != nil {
				return 0, err
			}
			l.col = 0
		}
	}
	return n, nil
}

func addressList(list []*mail.Address) string {
	parts := make([]string, len(list))
	for i, a := range list {
		parts[i] = a.String()
	}
	return strings.Join(parts, ", ")
}

// messageID returns a new Message-ID in the domain of the sender.
func messageID(from string) string {
	domain := "runnel.local"
	if _, d, ok := strings.Cut(from, "@"); ok && d != "" {
		domain = d
	}
	var r [16]byte
	_, _ = rand.Read(r[:])
	return fmt.Sprintf("<%x.%d@%s>", r, time.Now().UnixNano(), domain)
}
