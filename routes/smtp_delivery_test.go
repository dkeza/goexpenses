package routes

import (
	"bufio"
	"context"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"testing"
	"time"

	"goexpenses/util"
)

type receivedEmail struct {
	from string
	to   string
	data string
}

// startFakeSMTPServer accepts one SMTP session without STARTTLS or AUTH and
// reports the received envelope and message.
func startFakeSMTPServer(t *testing.T) (int, <-chan receivedEmail) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	received := make(chan receivedEmail, 1)

	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		connection.SetDeadline(time.Now().Add(10 * time.Second))
		reader := bufio.NewReader(connection)
		reply := func(line string) { io.WriteString(connection, line+"\r\n") }
		email := receivedEmail{}

		reply("220 fake.test ESMTP")
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			command := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(command, "EHLO"), strings.HasPrefix(command, "HELO"):
				reply("250-fake.test")
				reply("250 8BITMIME")
			case strings.HasPrefix(command, "MAIL FROM:"):
				email.from = strings.TrimSpace(line[len("MAIL FROM:"):])
				reply("250 OK")
			case strings.HasPrefix(command, "RCPT TO:"):
				email.to = strings.TrimSpace(line[len("RCPT TO:"):])
				reply("250 OK")
			case command == "DATA":
				reply("354 End data with <CR><LF>.<CR><LF>")
				var data strings.Builder
				for {
					dataLine, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					if dataLine == ".\r\n" {
						break
					}
					data.WriteString(strings.TrimPrefix(dataLine, "."))
				}
				email.data = data.String()
				reply("250 Queued")
				received <- email
			case command == "QUIT":
				reply("221 Bye")
				return
			default:
				reply("250 OK")
			}
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port, received
}

func TestDeliverWithSMTPSendsHTMLEmail(t *testing.T) {
	port, received := startFakeSMTPServer(t)
	previous := util.Settings
	t.Cleanup(func() { util.Settings = previous })
	util.Settings.MailHost = "127.0.0.1"
	util.Settings.MailHostPort = port
	util.Settings.MailFrom = "app@example.com"
	util.Settings.MailPassword = ""

	message := emailMessage{
		Recipient: "user@example.com",
		Subject:   "Goexpenses Промени лозинку",
		HTMLBody:  `Кликни: <a href="https://app.example.com/resetpassword?t=abc">Reset</a>`,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := deliverWithSMTP(ctx, message); err != nil {
		t.Fatalf("deliverWithSMTP: %v", err)
	}

	var email receivedEmail
	select {
	case email = <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("fake SMTP server received no message")
	}
	if !strings.Contains(email.from, "<app@example.com>") || !strings.Contains(email.to, "<user@example.com>") {
		t.Fatalf("envelope = from %q to %q", email.from, email.to)
	}

	parsed, err := mail.ReadMessage(strings.NewReader(email.data))
	if err != nil {
		t.Fatalf("parse received message: %v", err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	if err != nil || subject != message.Subject {
		t.Errorf("subject = %q, %v; want %q", subject, err, message.Subject)
	}
	if mediaType, _, _ := mime.ParseMediaType(parsed.Header.Get("Content-Type")); mediaType != "text/html" {
		t.Errorf("content type = %q, want text/html", parsed.Header.Get("Content-Type"))
	}
	var body io.Reader = parsed.Body
	if strings.EqualFold(parsed.Header.Get("Content-Transfer-Encoding"), "quoted-printable") {
		body = quotedprintable.NewReader(parsed.Body)
	}
	decodedBody, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read message body: %v", err)
	}
	if !strings.Contains(string(decodedBody), message.HTMLBody) {
		t.Errorf("body = %q, want it to contain %q", decodedBody, message.HTMLBody)
	}
	if parsed.Header.Get("Date") == "" || parsed.Header.Get("Message-Id") == "" {
		t.Errorf("message lacks Date or Message-ID headers: %v", parsed.Header)
	}
}

func TestMailUsesImplicitTLSOnlyOnSMTPSPort(t *testing.T) {
	for port, want := range map[int]bool{465: true, 587: false, 25: false, 2525: false} {
		if got := mailUsesImplicitTLS(port); got != want {
			t.Errorf("mailUsesImplicitTLS(%s) = %v, want %v", strconv.Itoa(port), got, want)
		}
	}
}
