// swprobe says what Stalwart is doing on its HTTP port.
//
// WHY. After a `ReloadSettings`, `POST /jmap/` started being closed with a reset while TCP kept
// accepting. That has several possible causes and one of them is decisive: the listener having
// switched to expecting TLS. Guessing costs a restart each time; asking costs one request.
//
// Tries, in order: connect, speak plain HTTP, and speak TLS. Whichever answers says what it expects.
package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	log.SetFlags(0)
	target := envOr("SW_ADDR", "stalwart:8080")
	log.Printf("=== probe %s at %s ===", time.Now().UTC().Format(time.RFC3339), target)

	// 1. Does TCP accept?
	c, err := net.DialTimeout("tcp", target, 10*time.Second)
	if err != nil {
		log.Fatalf("1. TCP: %v", err)
	}
	c.Close()
	log.Printf("1. TCP: accepts")

	// 2. Plain HTTP.
	log.Printf("2. Plain HTTP: %s", attempt(func() (string, error) { return request("http://" + target + "/") }))

	// 3. TLS. If it answers here and not in 2, the listener switched to HTTPS — and the rest of
	//    the infrastructure that talks to it in plain HTTP stops working without changing
	//    anything on its own side.
	log.Printf("3. TLS: %s", attempt(func() (string, error) {
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", target,
			&tls.Config{InsecureSkipVerify: true})
		if err != nil {
			return "", err
		}
		defer conn.Close()
		e := conn.ConnectionState()
		names := "no certificate"
		if len(e.PeerCertificates) > 0 {
			names = e.PeerCertificates[0].Subject.CommonName
		}
		fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", target)
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("handshake OK (certificate %s) → %s", names, strings.TrimSpace(line)), nil
	}))

	// 4. The path the application actually uses.
	log.Printf("4. POST /jmap/ over plain HTTP: %s", attempt(func() (string, error) {
		resp, err := (&http.Client{Timeout: 15 * time.Second}).Post("http://"+target+"/jmap/",
			"application/json", strings.NewReader(`{"using":[],"methodCalls":[]}`))
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		return resp.Status, nil
	}))

	log.Printf("=== end ===")
	for {
		time.Sleep(time.Hour)
	}
}

func attempt(f func() (string, error)) string {
	s, err := f()
	if err != nil {
		return "FAILED: " + err.Error()
	}
	return s
}

func request(url string) (string, error) {
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	return resp.Status, nil
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
