// Command provision is the M0 provisioning tool: it generates a private
// test CA, one secp256k1 key and mTLS certificate per participant
// (CRYPTO-009, SYS-002/GAP-48), and a domain config file (theta +
// participant list in the P order frozen at Init) ready for
// internal/config.Load.
//
// Usage:
//
//	provision -out DIR -theta N -participant id=host:port[,id=host:port...]
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"delia/internal/provision"
)

func main() {
	out := flag.String("out", "./provisioned", "output directory for keys, certs and domain.json")
	theta := flag.Int("theta", 0, "required: batch size theta (DATA-007/GAP-55, no default)")
	participants := flag.String("participants", "", "comma-separated id=host:port list, in P order (e.g. p1=127.0.0.1:9001,p2=127.0.0.1:9002)")
	flag.Parse()

	if *theta <= 0 {
		fmt.Fprintln(os.Stderr, "provision: -theta is required and must be > 0 (DATA-007/GAP-55: no protocol default)")
		os.Exit(2)
	}
	if strings.TrimSpace(*participants) == "" {
		fmt.Fprintln(os.Stderr, "provision: -participants is required")
		os.Exit(2)
	}

	var specs []provision.ParticipantSpec
	for _, entry := range strings.Split(*participants, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			fmt.Fprintf(os.Stderr, "provision: invalid participant entry %q (want id=host:port)\n", entry)
			os.Exit(2)
		}
		host := parts[1]
		if idx := strings.LastIndex(host, ":"); idx >= 0 {
			host = host[:idx]
		}
		specs = append(specs, provision.ParticipantSpec{
			ID:       parts[0],
			Endpoint: parts[1],
			SANs:     []string{host},
		})
	}

	if err := provision.WriteToDir(*out, *theta, specs); err != nil {
		fmt.Fprintf(os.Stderr, "provision: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("provisioned %d participant(s) into %s\n", len(specs), *out)
}
