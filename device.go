package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"caycle/internal/serialport"
	"caycle/internal/strava"

	"golang.org/x/term"
)

const deviceUsage = `Usage:
  caycle device wifi   [-port DEV]   save Wi-Fi name and password on the board
  caycle device strava [-port DEV]   log the board in to Strava (its own login)
  caycle device status [-port DEV]   show the board's settings

The board is the caycle ESP32 bike computer (see firmware/README.md),
connected over USB. Close the Arduino serial monitor first.
`

func runDevice(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, deviceUsage)
		os.Exit(2)
	}
	fs := flag.NewFlagSet("device "+args[0], flag.ExitOnError)
	portFlag := fs.String("port", "", "serial port (default: first USB serial device)")
	fs.Parse(args[1:])

	path := *portFlag
	if path == "" {
		var err error
		if path, err = serialport.Find(); err != nil {
			return err
		}
	}
	switch args[0] {
	case "wifi":
		return deviceWifi(path)
	case "strava":
		return deviceStrava(path)
	case "status":
		return withPort(path, func(p *serialport.Port) error {
			if err := p.WriteLine("status"); err != nil {
				return err
			}
			echo(p, time.Now().Add(1500*time.Millisecond), "")
			return nil
		})
	default:
		fmt.Fprint(os.Stderr, deviceUsage)
		os.Exit(2)
	}
	return nil
}

func withPort(path string, f func(*serialport.Port) error) error {
	p, err := serialport.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer p.Close()
	return f(p)
}

// echo prints the board's output until deadline or until a line contains one
// of the stop markers; it returns the matching line.
func echo(p *serialport.Port, deadline time.Time, stop ...string) string {
	for {
		line, ok := p.ReadLine(deadline)
		if !ok {
			return ""
		}
		fmt.Println("  board:", line)
		for _, s := range stop {
			if s != "" && strings.Contains(line, s) {
				return line
			}
		}
	}
}

// command sends a line and waits for the board's confirmation.
func command(p *serialport.Port, line, confirm string) error {
	if err := p.WriteLine(line); err != nil {
		return err
	}
	if echo(p, time.Now().Add(3*time.Second), confirm) == "" {
		return fmt.Errorf("board did not confirm %q; is caycle firmware running?", confirm)
	}
	return nil
}

func prompt(label string, secret bool) (string, error) {
	fmt.Print(label)
	if secret {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		return string(b), err
	}
	var s string
	_, err := fmt.Scanln(&s)
	return s, err
}

func deviceWifi(path string) error {
	ssid, err := prompt("Wi-Fi name: ", false)
	if err != nil || ssid == "" {
		return errors.New("no Wi-Fi name given")
	}
	pass, err := prompt("Wi-Fi password: ", true)
	if err != nil {
		return err
	}
	return withPort(path, func(p *serialport.Port) error {
		if err := command(p, "ssid "+ssid, "ssid saved"); err != nil {
			return err
		}
		if err := command(p, "pass "+pass, "password saved"); err != nil {
			return err
		}
		fmt.Println("testing the connection...")
		line := echo(p, time.Now().Add(40*time.Second), "clock synced", "NO WIFI", "ON STRAVA", "UPLOAD FAILED")
		switch {
		case strings.Contains(line, "NO WIFI"):
			return errors.New("the board could not join the Wi-Fi; check the name and password (2.4 GHz only)")
		case line == "":
			fmt.Println("no answer yet; check with `caycle device status` in a minute")
		default:
			fmt.Println("Wi-Fi works ✓")
		}
		return nil
	})
}

func deviceStrava(path string) error {
	mac, err := strava.Load(credsPath())
	if err != nil {
		return fmt.Errorf("%w (the board uses the same Strava API app as caycle on this Mac)", err)
	}
	// The board gets its own authorization, so its token rotation can't log
	// this Mac out (and the other way round).
	board := strava.NewClient("", mac.Creds.ClientID, mac.Creds.ClientSecret)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fmt.Println("authorize the board in the browser (same Strava app as this Mac)...")
	if err := board.Login(ctx, func(url string) {
		fmt.Println("if it does not open, visit:", url)
		openBrowser(url)
	}); err != nil {
		return err
	}

	err = withPort(path, func(p *serialport.Port) error {
		c := board.Creds
		if err := command(p, fmt.Sprintf("strava %s %s %s", c.ClientID, c.ClientSecret, c.RefreshToken), "strava: saved"); err != nil {
			return err
		}
		fmt.Println("the board is testing its Strava login over Wi-Fi...")
		line := echo(p, time.Now().Add(60*time.Second), "STRAVA OK", "STRAVA LOGIN?", "STRAVA ERROR", "NO WIFI")
		switch {
		case strings.Contains(line, "STRAVA OK"):
			fmt.Println("board is connected to Strava ✓")
		case strings.Contains(line, "NO WIFI"):
			return errors.New("the board has no Wi-Fi; run `caycle device wifi` first, then `caycle device strava` again")
		case line == "":
			fmt.Println("no answer yet; check with `caycle device status` in a minute")
		default:
			return fmt.Errorf("the board could not log in to Strava: %s", line)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Make sure the new authorization didn't invalidate this Mac's login.
	if err := mac.Refresh(ctx); err != nil {
		fmt.Println("warning: this Mac's Strava login stopped working; run `caycle strava login` again:", err)
	}
	return nil
}
