package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"caycle/internal/session"
	"caycle/internal/strava"

	"golang.org/x/term"
)

type key int

const (
	keyNone key = iota
	keyUp
	keyDown
	keyQuit
	keyResistance
	keyERG
	keySim
	keyRoute
	keyYes
	keyNo
)

func readKeys(out chan<- key) {
	buf := make([]byte, 16)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			out <- keyQuit
			return
		}
		b := buf[:n]
		for len(b) > 0 {
			k := keyNone
			switch {
			case len(b) >= 3 && b[0] == 0x1b && b[1] == '[':
				switch b[2] {
				case 'A', 'C':
					k = keyUp
				case 'B', 'D':
					k = keyDown
				}
				b = b[3:]
			default:
				switch b[0] {
				case '+', '=', 'k':
					k = keyUp
				case '-', '_', 'j':
					k = keyDown
				case 'q', 'Q', 0x03, 0x04: // q, Ctrl-C, Ctrl-D
					k = keyQuit
				case 'r', 'R':
					k = keyResistance
				case 'e', 'E':
					k = keyERG
				case 's', 'S', 'g', 'G':
					k = keySim
				case 'm', 'M':
					k = keyRoute
				case 'y', 'Y', '\r', '\n':
					k = keyYes
				case 'n', 'N':
					k = keyNo
				}
				b = b[1:]
			}
			if k != keyNone {
				out <- k
			}
		}
	}
}

func notifySignals(c chan<- os.Signal) {
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
}

func runRide(args []string) error {
	fs := flag.NewFlagSet("ride", flag.ExitOnError)
	f := addAppFlags(fs, ":8080")
	name := fs.String("name", "", "Strava activity name (default: route name, or Strava picks one)")
	fs.Parse(args)

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("ride needs an interactive terminal; use `caycle serve` to run headless")
	}
	a, err := startApp(f)
	if err != nil {
		return err
	}
	defer a.close()
	if _, err := a.sess.StartRide(); err != nil {
		return err
	}

	old, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	// Alternate screen, hidden cursor; restored on exit.
	fmt.Print("\x1b[?1049h\x1b[?25l")
	var restoreOnce sync.Once
	restore := func() {
		restoreOnce.Do(func() {
			fmt.Print("\x1b[?25h\x1b[?1049l")
			term.Restore(int(os.Stdin.Fd()), old)
		})
	}
	defer restore()

	keys := make(chan key, 8)
	go readKeys(keys)
	sigs := make(chan os.Signal, 1)
	notifySignals(sigs)

	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	var flash string
loop:
	for {
		select {
		case k := <-keys:
			flash = ""
			switch k {
			case keyQuit:
				break loop
			case keyUp:
				a.sess.Step(1)
			case keyDown:
				a.sess.Step(-1)
			case keyResistance:
				a.sess.SetMode(session.Resistance)
			case keyERG:
				a.sess.SetMode(session.ERG)
			case keySim:
				a.sess.SetMode(session.Sim)
			case keyRoute:
				if err := a.sess.SetMode(session.RouteMode); err != nil {
					flash = err.Error() + " (in the web dashboard or with -route)"
				}
			}
		case <-sigs:
			break loop
		case <-tick.C:
		}
		render(a.sess.Snapshot(), a.urls, flash)
	}
	restore()

	ride, err := a.sess.FinishRide()
	if errors.Is(err, session.ErrNotRecording) {
		// Finished from the web dashboard; offer the last one.
		if id := a.sess.LastRideID(); id != 0 {
			ride, err = a.store.Ride(id)
		}
	}
	if err != nil {
		return err
	}
	fmt.Printf("ride: %s moving, %.2f km, avg %.0f W, max %d W, %.0f kJ\n",
		(time.Duration(ride.MovingS) * time.Second), ride.DistanceM/1000, ride.AvgPower, ride.MaxPower, ride.EnergyKJ)
	if ride.MovingS == 0 {
		a.store.DeleteRide(ride.ID)
		return nil
	}
	fmt.Println("saved to", dbPath())
	if ride.StravaURL != nil {
		fmt.Println("on Strava:", *ride.StravaURL)
		return nil
	}
	if !a.uploader.StravaConnected() {
		fmt.Println("run `caycle strava login` once to upload rides to Strava")
		return nil
	}
	// The key reader still owns stdin; drop keys pressed before the prompt.
	for len(keys) > 0 {
		<-keys
	}
	fmt.Print("upload to Strava? [Y/n] ")
	select {
	case k := <-keys:
		if k != keyYes {
			return nil
		}
	case <-sigs:
		fmt.Println()
		return nil
	}
	fmt.Println("uploading...")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	url, err := a.uploader.Strava(ctx, ride.ID, *name)
	if err != nil {
		if errors.Is(err, strava.ErrNotLoggedIn) {
			return err
		}
		return fmt.Errorf("%w (retry with `caycle strava upload %d`)", err, ride.ID)
	}
	fmt.Println("uploaded:", url)
	return nil
}

func orDash(v *int, unit string) string {
	if v == nil {
		return "--"
	}
	return fmt.Sprintf("%d %s", *v, unit)
}

func render(s session.Snapshot, urls []string, flash string) {
	tab := func(label string, m session.Mode) string {
		if m == s.Control.Mode {
			return "\x1b[7m " + label + " \x1b[0m"
		}
		return " " + label + " "
	}
	hr := "\x1b[2m-- (" + s.HR.Status + ")\x1b[0m"
	if s.HeartRate != nil {
		hr = fmt.Sprintf("%d bpm", *s.HeartRate)
	}
	c := s.Control
	var target string
	switch c.Mode {
	case session.ERG:
		target = fmt.Sprintf("%.0f W", c.Power)
	case session.Sim:
		target = fmt.Sprintf("%+.1f %%", c.Grade)
	case session.RouteMode:
		target = fmt.Sprintf("difficulty %.0f %%   grade %+.1f %%", c.Difficulty, c.AppliedGrade)
	default:
		target = fmt.Sprintf("%.0f %%", c.Resistance)
	}

	r := s.Ride
	lines := []string{
		fmt.Sprintf(" \x1b[1mcaycle\x1b[0m · %s    %s", s.Trainer.Name, fmtDuration(time.Duration(r.MovingS)*time.Second)),
		" " + strings.Repeat("─", 50),
		fmt.Sprintf("  POWER      \x1b[1m%-9s\x1b[0m avg %.0f W  max %d W", orDash(s.Power, "W"), r.AvgPower, r.MaxPower),
		fmt.Sprintf("  CADENCE    %s", orDash(s.Cadence, "rpm")),
		fmt.Sprintf("  SPEED      %.1f km/h", s.SpeedKmh),
		fmt.Sprintf("  DISTANCE   %.2f km", r.DistanceM/1000),
		fmt.Sprintf("  HEART RATE %s", hr),
	}
	if s.Route != nil {
		state := fmt.Sprintf("%.1f / %.1f km, grade %+.1f %%", s.Route.PositionM/1000, s.Route.DistanceM/1000, s.Route.Grade)
		if s.Route.Done {
			state = "complete!"
		}
		lines = append(lines, fmt.Sprintf("  ROUTE      %s: %s", s.Route.Name, state))
	}
	lines = append(lines, "",
		"  MODE "+tab("[R]esistance", session.Resistance)+tab("[E]RG", session.ERG)+tab("[G]rade", session.Sim)+tab("[M]ap route", session.RouteMode),
		fmt.Sprintf("  TARGET     \x1b[1m%s\x1b[0m", target),
	)
	if c.Mode == session.Resistance {
		filled := int(c.Resistance / 5)
		lines = append(lines, "             "+strings.Repeat("█", filled)+strings.Repeat("░", 20-filled))
	}
	lines = append(lines, "")
	if !r.Recording {
		lines = append(lines, "  \x1b[33mnot recording (finished from the web dashboard)\x1b[0m")
	}
	for _, warn := range []string{s.Trainer.TargetStatus, s.Trainer.WriteError, flash} {
		if warn != "" {
			lines = append(lines, "  \x1b[33m"+warn+"\x1b[0m")
		}
	}
	if len(urls) > 0 {
		lines = append(lines, "  dashboard: "+strings.Join(urls, "  "))
	}
	lines = append(lines, "", "  ↑/+ harder   ↓/- easier   r/e/g/m switch mode   q finish")

	fmt.Print("\x1b[H\x1b[J" + strings.Join(lines, "\r\n"))
}

func fmtDuration(d time.Duration) string {
	s := int(d.Seconds())
	return fmt.Sprintf("%02d:%02d:%02d", s/3600, s/60%60, s%60)
}
