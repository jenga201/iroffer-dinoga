// Command bounce runs the SXDCC reverse-tunnel relay. It binds a tunnel
// endpoint (that a bot behind NAT dials out to) and a public client port range
// (that SXDCC clients dial). It forwards bytes between the two and never
// terminates TLS.
package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/jenga201/dinoga/internal/bounce"
)

func main() {
	tunnel := flag.String("tunnel", "0.0.0.0:40001", "tunnel endpoint (bot REGISTER/JOIN)")
	clientStart := flag.Int("client-start", 40000, "first public client port")
	clientLimit := flag.Int("client-limit", 40000, "last public client port")
	secret := flag.String("secret", "", "shared secret (required)")
	flag.Parse()
	if *secret == "" {
		log.Fatal("bounce: -secret is required")
	}

	r := bounce.NewRelay(*secret)
	if _, err := r.ServeTunnel(*tunnel); err != nil {
		log.Fatalf("bounce: tunnel listener: %v", err)
	}
	for p := *clientStart; p <= *clientLimit; p++ {
		if _, err := r.ServeClient(fmt.Sprintf("0.0.0.0:%d", p)); err != nil {
			log.Fatalf("bounce: client listener %d: %v", p, err)
		}
	}
	log.Printf("bounce: tunnel=%s clients=%d-%d", *tunnel, *clientStart, *clientLimit)
	select {}
}