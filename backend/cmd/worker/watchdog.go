package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// Watchdog checks that Stalwart is reachable BY THE NAME clients actually use.
//
// Why the container healthcheck is not enough: mail and webmail have gone down for half an hour
// with Docker reporting `healthy` the whole time — and Docker was right. The image's healthcheck
// runs `curl http://127.0.0.1:8080/healthz/live` INSIDE the container, where the process was alive
// and responding. What was broken was Swarm's routing: a connection to `stalwart:8080` was accepted
// and then dropped.
//
// A loopback healthcheck cannot see that, by definition — it never goes through the path that
// fails. This watchdog does: it connects using the same name and the same network the API uses.
// That is the difference between "the process is up" and "the service is serving".
//
// It fixes nothing, on purpose. It only alerts. A watchdog that restarts services on its own turns
// a network problem into a cascade of restarts, and the log ends up telling the wrong story.

const (
	watchdogInterval = time.Minute
	// Only complains after failing consecutively: a network blip is not an outage, and an alarm
	// that fires on its own every now and then stops being read.
	failuresBeforeAlarm = 3
)

func startWatchdog(stalwartURL string) {
	if stalwartURL == "" {
		log.Printf("stalwart sentinel disabled (STALWART_URL unset)")
		return
	}
	target := strings.TrimRight(stalwartURL, "/") + "/healthz/live"

	go func() {
		cli := &http.Client{Timeout: 10 * time.Second}
		var consecutive int
		for {
			time.Sleep(watchdogInterval)

			err := peek(cli, target)
			if err == nil {
				if consecutive >= failuresBeforeAlarm {
					log.Printf("stalwart sentinel: RECOVERED, %s reachable again after %d failures",
						target, consecutive)
				}
				consecutive = 0
				continue
			}

			consecutive++
			// The first warning is what matters: it says what failed and, above all, distinguishes
			// it from the container's healthcheck, which will keep saying `healthy`.
			if consecutive == failuresBeforeAlarm {
				log.Printf("stalwart sentinel: UNREACHABLE by service name for %d checks: %v "+
					"(the container healthcheck may still report healthy — it probes loopback, "+
					"this probes the path clients use; suspect swarm routing, see docs/DEPLOY.md)",
					consecutive, err)
			} else if consecutive > failuresBeforeAlarm && consecutive%10 == 0 {
				log.Printf("stalwart sentinel: still unreachable after %d checks: %v", consecutive, err)
			}
		}
	}()
	log.Printf("stalwart sentinel started (checks %s every %s)", target, watchdogInterval)
}

func peek(cli *http.Client, target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Any HTTP response will do: what is being checked is that the connection reaches the end
	// and gets answered, not what Stalwart thinks of its own health.
	if resp.StatusCode >= 500 {
		return fmt.Errorf("answered %s", resp.Status)
	}
	return nil
}
