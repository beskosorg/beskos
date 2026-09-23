// mailprobe sends a test message down the real mail path and says what to check next.
//
// WHY. After swapping Stalwart's block storage, looking at the configuration proves nothing: it
// proves the configuration changed. What matters is a message coming in, Stalwart writing the
// original, and that original showing up encrypted in the right bucket. That is only seen with
// real mail.
//
// Runs INSIDE the cluster because the mail ports are not reachable from outside the infrastructure.
package main

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log"
	"net/smtp"
	"os"
	"strings"
	"time"
)

func main() {
	log.SetFlags(0)
	log.Printf("=== run %s ===", time.Now().UTC().Format(time.RFC3339))

	host := env("NOTIFY_SMTP_HOST")
	port := envOr("NOTIFY_SMTP_PORT", "465")
	user := env("NOTIFY_SMTP_USER")
	pwd := env("NOTIFY_SMTP_PASSWORD")
	recipient := envOr("PROVA_PARA", user)

	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		log.Fatal(err)
	}
	marker := hex.EncodeToString(b)

	// Body large enough for the block to be recognizable in the bucket and to be worth encrypting
	// across more than one piece of content.
	body := strings.Repeat("filler line for the encryption gateway probe. ", 300)
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: gateway probe %s\r\n\r\n"+
		"Unique marker: %s\r\n%s\r\n", user, recipient, marker, marker, body)

	// Where we CONNECT may not be the same as the certificate name. From inside the VPS, going
	// out to the public IP and back in (hairpin) usually does not work — we connect to the
	// container by its internal name and keep `mail.beskos.com` as the certificate name, so
	// verification keeps holding.
	address := envOr("PROVA_ADDR", host+":"+port)
	log.Printf("sending to %s connecting to %s (certificate from %s), marker %s", recipient, address, host, marker)

	// IMPLICIT TLS on 465. Go's smtp.SendMail speaks StartTLS, which does not work here: this
	// Stalwart's 587 drops the connection (docs/DEPLOY.md), and 465 expects TLS from the first
	// byte. So the connection is made by hand.
	conn, err := tls.Dial("tcp", address, &tls.Config{ServerName: host})
	if err != nil {
		log.Fatalf("TLS dial to %s: %v", address, err)
	}
	defer conn.Close()

	cli, err := smtp.NewClient(conn, host)
	if err != nil {
		log.Fatalf("smtp: %v", err)
	}
	defer cli.Quit()

	if err := cli.Auth(smtp.PlainAuth("", user, pwd, host)); err != nil {
		log.Fatalf("authenticate: %v", err)
	}
	if err := cli.Mail(user); err != nil {
		log.Fatalf("MAIL FROM: %v", err)
	}
	if err := cli.Rcpt(recipient); err != nil {
		log.Fatalf("RCPT TO: %v", err)
	}
	w, err := cli.Data()
	if err != nil {
		log.Fatalf("DATA: %v", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		log.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		log.Fatalf("close: %v", err)
	}

	log.Printf("sent")
	log.Printf("look now: a PUT in the blobgw log, and a new object under mail/ in the bucket")
	log.Printf("marker to look for in the decrypted content: %s", marker)
	for {
		time.Sleep(time.Hour)
	}
}

func env(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("%s missing", name)
	}
	return v
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
