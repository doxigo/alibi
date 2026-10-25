// Alibi Companion tells Alibi the moment this computer's connection starts coming out somewhere else:
// a VPN connecting, dropping or switching exits. Covers on "Match my IP address" then move within
// seconds, instead of on Alibi's own once-a-minute GeoJS check.
//
// It speaks the protocol of Whereabouts, the Mac app that does this on macOS, and answers to the same
// native messaging host name, so Alibi needs nothing new to use it. For the same reason it doesn't
// install on macOS: that name belongs to Whereabouts there.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Set at build time from manifest.json: scripts/build-companion.mjs passes -X main.version=….
var version = "dev"

const (
	// The native messaging host Alibi connects to (js/background.js), shared with Whereabouts.
	hostName = "com.doxigo.whereabouts"
	// Alibi's ID, pinned by the key in manifest.json. Browsers start the companion for it alone.
	extensionOrigin = "chrome-extension://dhlpjjenplighmpbogjpmljbahbnjpfa/"
)

const usage = `Alibi Companion %s
Tells Alibi the moment your VPN exit changes, so "Match my IP address" follows it within seconds.

Usage:
  alibi-companion install [--force] [--timezone | --no-timezone]
                             copy it into your user folder and register it with your browsers.
                             On Windows it also starts the tray app. With --timezone, that keeps
                             Windows on your VPN's time zone while a VPN is up (the tray can
                             turn that on later too); an update keeps what you chose there
  alibi-companion uninstall [--quiet]
                             remove it again, and put the time zone back as it was
  alibi-companion status     show where it's registered and where your connection appears to be
  alibi-companion agent --dry-run
                             show what time zone sync would set now, changing nothing (Windows)
  alibi-companion version

Once it's installed, your browser starts it whenever Alibi needs it.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	// A browser starts a native host with the caller's origin as the first argument
	// (and on Windows, --parent-window=… after it).
	if len(args) > 0 && strings.HasPrefix(args[0], "chrome-extension://") {
		return host(args[0], stdin, stdout, stderr)
	}

	command, options := "", args
	if len(args) > 0 {
		command, options = args[0], args[1:]
	}
	sys := defaultSystem()
	// The agent is started by Windows, with no console to print to.
	if command == "agent" {
		if err := agentCommand(sys, options, stderr); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	force, timezone, quiet, after := false, zoneUnsaid, false, 0
	for i := 0; i < len(options); i++ {
		switch option := options[i]; {
		case command == "install" && option == "--force":
			force = true
		case command == "install" && option == "--timezone":
			timezone = zoneOn
		case command == "install" && option == "--no-timezone":
			timezone = zoneOff
		case command == "uninstall" && option == "--quiet":
			quiet = true
		case command == "uninstall" && option == "--after" && i+1 < len(options):
			i++
			after, _ = strconv.Atoi(options[i])
		default:
			fmt.Fprintf(stderr, "Unknown option %s\n\n"+usage, option, version)
			return 2
		}
	}
	// Started from Explorer or Settings > Apps, with a window of its own: keep that window open
	// at the end, long enough to read what happened.
	pause := startedFromExplorer() && !quiet

	var err error
	switch command {
	case "install":
		err = install(sys, stdout, force, timezone)
	case "uninstall":
		if after != 0 {
			waitForExit(after)
		} else if handed, herr := handOffUninstall(sys, quiet); handed || herr != nil {
			if herr != nil {
				fmt.Fprintln(stderr, herr)
				return 1
			}
			return 0 // the copy carries on in this window
		}
		err = uninstall(sys, stdout, quiet)
	case "status":
		err = status(sys, stdout, newGeoJS().lookup)
	case "version", "--version":
		fmt.Fprintln(stdout, version)
	case "help", "-h", "--help":
		fmt.Fprintf(stdout, usage, version)
	case "":
		if !pause {
			fmt.Fprintf(stdout, usage, version)
			return 0
		}
		// Double-clicked on Windows: install.
		err = install(sys, stdout, false, zoneUnsaid)
	default:
		fmt.Fprintf(stderr, "Unknown command %q\n\n"+usage, command, version)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "\n%v\n", err)
	}
	if pause {
		fmt.Fprint(stdout, "\nPress Enter to close.")
		bufio.NewReader(stdin).ReadString('\n')
	}
	if err != nil {
		return 1
	}
	return 0
}
