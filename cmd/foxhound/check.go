package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sadewadee/foxhound/identity"
)

// cmdCheck generates an identity profile and verifies its internal consistency,
// printing a human-readable report with PASS/FAIL indicators.
func cmdCheck(args []string) {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: foxhound check [flags]")
		fmt.Fprintln(os.Stderr, "\nTest fingerprint consistency and TLS profile.")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		fs.PrintDefaults()
	}

	browser := fs.String("browser", "firefox", "browser to test (firefox|chrome)")
	osFlag := fs.String("os", "", "OS to test (windows|macos|linux); random if not set")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	opts := []identity.Option{
		identity.WithBrowser(identity.Browser(*browser)),
	}
	if *osFlag != "" {
		opts = append(opts, identity.WithOS(identity.OS(*osFlag)))
	}

	profile := identity.Generate(opts...)

	fmt.Println("Foxhound Identity Check")
	fmt.Println(strings.Repeat("-", 50))

	printCheck("Browser", string(profile.BrowserName))
	printCheck("OS", string(profile.OS))
	printCheck("User-Agent", profile.UA)
	printCheck("TLS Profile", profile.TLSProfile)
	printCheck("Platform", profile.Platform)
	printCheck("OS Version", profile.OSVersion)
	printCheck("Browser Version", profile.BrowserVer)
	printCheck("Locale", profile.Locale)
	printCheck("Timezone", profile.Timezone)

	fmt.Println(strings.Repeat("-", 50))

	// Consistency checks.
	failures := 0

	fmt.Println("\nConsistency Checks:")
	failures += assertCheck("UA contains browser name",
		containsAny(strings.ToLower(profile.UA), []string{
			strings.ToLower(string(profile.BrowserName)), "mozilla",
		}))
	failures += assertCheck("UA contains OS hint",
		containsAny(strings.ToLower(profile.UA), []string{
			"windows", "macintosh", "linux", "x11",
		}))
	failures += assertCheck("TLS profile set",
		profile.TLSProfile != "")
	failures += assertCheck("Header order non-empty",
		len(profile.HeaderOrder) > 0)
	failures += assertCheck("Screen dimensions set",
		profile.ScreenW > 0 && profile.ScreenH > 0)
	failures += assertCheck("Locale set",
		profile.Locale != "")
	failures += assertCheck("Timezone set",
		profile.Timezone != "")

	fmt.Println()
	if failures == 0 {
		fmt.Println("All checks PASS")
	} else {
		fmt.Printf("%d check(s) FAIL\n", failures)
		os.Exit(1)
	}
}

// printCheck prints a labelled attribute value.
func printCheck(label, value string) {
	fmt.Printf("  %-20s %s\n", label+":", value)
}

// assertCheck prints a named assertion result and returns 0 on pass, 1 on fail.
func assertCheck(name string, ok bool) int {
	if ok {
		fmt.Printf("  [PASS] %s\n", name)
		return 0
	}
	fmt.Printf("  [FAIL] %s\n", name)
	return 1
}

// containsAny returns true if s contains any of the substrings in subs.
func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
