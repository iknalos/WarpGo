// Command probe runs WarpGo's connection tests from a desktop, exactly as the
// app runs them, so a network can be checked without the phone.
//
//	probe -wg wgcf-profile.conf -masque masque.json
//	probe -register-masque masque.json    # create a MASQUE account
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/iknalos/WarpGo/core/warpcore"
)

func main() {
	wgConf := flag.String("wg", "", "wgcf-profile.conf (WireGuard account)")
	masqueFile := flag.String("masque", "", "MASQUE account JSON")
	register := flag.String("register-masque", "", "register a new MASQUE account and write its JSON here")
	ports := flag.String("ports", "4500,2408,500", "WireGuard ports to test")
	modes := flag.String("modes", "", "MASQUE modes to test: h3, h2 (default both)")
	timeout := flag.Int("timeout", 6000, "per-test timeout in ms")
	verbose := flag.Bool("v", false, "print the engine log")
	watchdog := flag.Duration("watchdog", 0, "dump goroutines and exit if still running after this long")
	flag.Parse()
	warpcore.EchoLogs(*verbose)
	if *watchdog > 0 {
		time.AfterFunc(*watchdog, func() {
			buf := make([]byte, 1<<20)
			fmt.Fprintf(os.Stderr, "WATCHDOG after %s:\n%s\n", *watchdog, buf[:runtime.Stack(buf, true)])
			os.Exit(3)
		})
	}

	if *register != "" {
		acct, err := warpcore.RegisterMasque()
		if err != nil {
			log.Fatalf("register: %v", err)
		}
		if err := os.WriteFile(*register, []byte(acct), 0o600); err != nil {
			log.Fatal(err)
		}
		fmt.Println("MASQUE account written to", *register)
		return
	}

	if *wgConf != "" {
		acct, err := wgAccountFromConf(*wgConf)
		if err != nil {
			log.Fatal(err)
		}
		for _, p := range strings.Split(*ports, ",") {
			port, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil {
				log.Fatalf("bad port %q", p)
			}
			fmt.Printf("WireGuard %-5d %s\n", port, warpcore.ProbeWireGuard(acct, port, *timeout))
		}
	}
	if *masqueFile != "" {
		acct, err := os.ReadFile(*masqueFile)
		if err != nil {
			log.Fatal(err)
		}
		if *modes == "" || strings.Contains(*modes, "h3") {
			fmt.Printf("MASQUE h3     %s\n", warpcore.ProbeMasque(string(acct), false, *timeout))
		}
		if *modes == "" || strings.Contains(*modes, "h2") {
			fmt.Printf("MASQUE h2     %s\n", warpcore.ProbeMasque(string(acct), true, *timeout))
		}
	}
}

// wgAccountFromConf reads PrivateKey and Address from a wgcf profile.
func wgAccountFromConf(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var priv, v4, v6 string
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "PrivateKey":
			priv = strings.TrimSpace(v)
		case "Address":
			for _, a := range strings.Split(v, ",") {
				if a = strings.TrimSpace(a); strings.Contains(a, ":") {
					v6 = a
				} else {
					v4 = a
				}
			}
		}
	}
	if priv == "" {
		return "", fmt.Errorf("%s has no PrivateKey", path)
	}
	return fmt.Sprintf(`{"private_key":%q,"address_v4":%q,"address_v6":%q}`, priv, v4, v6), nil
}
