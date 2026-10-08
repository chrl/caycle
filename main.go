// Command caycle reads data from and controls a Tacx smart trainer over
// Bluetooth LE.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"caycle/internal/ble"
	"caycle/internal/hrm"
	"caycle/internal/store"
	"caycle/internal/strava"
	"caycle/internal/trainer"

	"tinygo.org/x/bluetooth"
)

const usage = `caycle — control a Tacx smart trainer from the terminal

Usage:
  caycle scan    [-timeout 10s] [-all]     list nearby trainers
  caycle inspect [-device NAME|ADDR]       list a device's BLE services
  caycle monitor [-device NAME|ADDR]       print trainer data once per second
  caycle ride    [-route FILE.gpx] ...     terminal + web dashboard, records a ride
  caycle serve   [-demo] ...               web dashboard only (start rides in the browser)
  caycle rides                             list recorded rides
  caycle strava  login|upload|logout       connect to Strava, upload rides
  caycle device  wifi|strava|status        set up the ESP32 bike computer over USB

Run "caycle <command> -h" for command flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch cmd, args := os.Args[1], os.Args[2:]; cmd {
	case "scan":
		err = runScan(args)
	case "inspect":
		err = runInspect(args)
	case "monitor":
		err = runMonitor(args)
	case "ride":
		err = runRide(args)
	case "serve":
		err = runServe(args)
	case "rides":
		err = runRides(args)
	case "device":
		err = runDevice(args)
	case "strava":
		err = runStrava(args)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func enableAdapter() (*bluetooth.Adapter, error) {
	a := bluetooth.DefaultAdapter
	if err := a.Enable(); err != nil {
		return nil, fmt.Errorf("enable bluetooth: %w", err)
	}
	return a, nil
}

func runScan(args []string) error {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	timeout := fs.Duration("timeout", 10*time.Second, "how long to scan")
	all := fs.Bool("all", false, "show all named BLE devices, not only trainers")
	fs.Parse(args)

	adapter, err := enableAdapter()
	if err != nil {
		return err
	}
	fmt.Printf("scanning for %s...\n", *timeout)

	var mu sync.Mutex
	seen := map[string]bool{}
	time.AfterFunc(*timeout, func() { adapter.StopScan() })
	err = adapter.Scan(func(_ *bluetooth.Adapter, r bluetooth.ScanResult) {
		addr := r.Address.String()
		if !*all && !trainer.LooksLikeTrainer(r) && !r.HasServiceUUID(hrm.Service) {
			return
		}
		if r.LocalName() == "" && !r.HasServiceUUID(hrm.Service) {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if seen[addr] {
			return
		}
		seen[addr] = true
		var tags []string
		for _, s := range []struct {
			uuid bluetooth.UUID
			tag  string
		}{
			{trainer.FECService, "FE-C"},
			{trainer.FTMSService, "FTMS"},
			{trainer.CyclingPowerService, "power"},
			{trainer.CyclingSpeedCadence, "speed/cadence"},
			{hrm.Service, "heart-rate"},
		} {
			if r.HasServiceUUID(s.uuid) {
				tags = append(tags, s.tag)
			}
		}
		sort.Strings(tags)
		fmt.Printf("%-24s %s  rssi %d  %s\n", r.LocalName(), addr, r.RSSI, strings.Join(tags, " "))
	})
	if err != nil {
		return err
	}
	if len(seen) == 0 {
		fmt.Println("nothing found — pedal a few strokes to wake the trainer and make sure no other app is connected")
	}
	return nil
}

func findDevice(adapter *bluetooth.Adapter, device string, timeout time.Duration) (bluetooth.ScanResult, error) {
	fmt.Println("looking for trainer...")
	r, err := ble.Find(adapter, timeout, trainer.MatchDevice(device))
	if err != nil {
		return r, fmt.Errorf("%w (is the trainer on and not connected to another app?)", err)
	}
	fmt.Printf("found %s (%s), connecting...\n", r.LocalName(), r.Address.String())
	return r, nil
}

func runInspect(args []string) error {
	fs := flag.NewFlagSet("inspect", flag.ExitOnError)
	device := fs.String("device", "", "device name substring or address (default: first trainer found)")
	timeout := fs.Duration("timeout", 15*time.Second, "how long to scan for the device")
	fs.Parse(args)

	adapter, err := enableAdapter()
	if err != nil {
		return err
	}
	r, err := findDevice(adapter, *device, *timeout)
	if err != nil {
		return err
	}
	return trainer.Inspect(adapter, r, os.Stdout)
}

func runMonitor(args []string) error {
	fs := flag.NewFlagSet("monitor", flag.ExitOnError)
	device := fs.String("device", "", "device name substring or address (default: first trainer found)")
	timeout := fs.Duration("timeout", 15*time.Second, "how long to scan for the trainer")
	duration := fs.Duration("duration", 0, "stop after this long (default: until Ctrl-C)")
	fs.Parse(args)

	adapter, err := enableAdapter()
	if err != nil {
		return err
	}
	r, err := findDevice(adapter, *device, *timeout)
	if err != nil {
		return err
	}
	t, err := trainer.Connect(adapter, r)
	if err != nil {
		return err
	}
	defer t.Close()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	var stop <-chan time.Time
	if *duration > 0 {
		stop = time.After(*duration)
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			m := t.Metrics()
			age := "no data yet"
			if !m.Updated.IsZero() {
				age = fmt.Sprintf("%.1fs ago", time.Since(m.Updated).Seconds())
			}
			fmt.Printf("%s  power %-7s cadence %-8s speed %5.1f km/h  dist %6.0f m  hr %3d  state %-7s (%s)\n",
				time.Now().Format("15:04:05"), orDash(validPtr(m.Power), "W"), orDash(validPtr(m.Cadence), "rpm"),
				m.SpeedKmh, m.DistanceM, m.HeartRate, m.State, age)
		case <-t.Done():
			return errors.New("trainer disconnected")
		case <-sigs:
			return nil
		case <-stop:
			return nil
		}
	}
}

// appDir holds caycle's saved rides and Strava credentials.
func appDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".caycle"
	}
	return filepath.Join(home, ".caycle")
}

func credsPath() string { return filepath.Join(appDir(), "strava.json") }

const stravaUsage = `Usage:
  caycle strava login  [-client-id ID -client-secret SECRET]
  caycle strava upload [-name NAME] [-force] RIDE_ID|FILE.tcx...
  caycle strava logout

Before the first login, create an API application at
https://www.strava.com/settings/api (any website, callback domain "localhost")
and pass its Client ID and Client Secret, or set STRAVA_CLIENT_ID and
STRAVA_CLIENT_SECRET.
`

func runStrava(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, stravaUsage)
		os.Exit(2)
	}
	switch args[0] {
	case "login":
		return stravaLogin(args[1:])
	case "upload":
		fs := flag.NewFlagSet("strava upload", flag.ExitOnError)
		name := fs.String("name", "", "activity name (default: Strava picks one)")
		force := fs.Bool("force", false, "upload a ride again even if it was uploaded before (delete the old activity on Strava first)")
		fs.Parse(args[1:])
		if fs.NArg() == 0 {
			return errors.New("give ride IDs (see `caycle rides`) or TCX files")
		}
		return stravaUpload(fs.Args(), *name, *force)
	case "logout":
		err := os.Remove(credsPath())
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	default:
		fmt.Fprint(os.Stderr, stravaUsage)
		os.Exit(2)
	}
	return nil
}

func stravaLogin(args []string) error {
	fs := flag.NewFlagSet("strava login", flag.ExitOnError)
	id := fs.String("client-id", os.Getenv("STRAVA_CLIENT_ID"), "Strava API application Client ID")
	secret := fs.String("client-secret", os.Getenv("STRAVA_CLIENT_SECRET"), "Strava API application Client Secret")
	fs.Parse(args)

	// Re-use the stored application when logging in again.
	if old, err := strava.Load(credsPath()); err == nil && *id == "" && *secret == "" {
		*id, *secret = old.Creds.ClientID, old.Creds.ClientSecret
	}
	if *id == "" || *secret == "" {
		fmt.Fprint(os.Stderr, stravaUsage)
		return errors.New("missing Strava client ID or secret")
	}

	client := strava.NewClient(credsPath(), *id, *secret)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	err := client.Login(ctx, func(url string) {
		fmt.Println("opening Strava in your browser to authorize caycle...")
		fmt.Println("if it does not open, visit:", url)
		openBrowser(url)
	})
	if err != nil {
		return err
	}
	fmt.Printf("connected to Strava as %s\n", client.Creds.Athlete)
	return nil
}

func uploadRide(client *strava.Client, path, name string) error {
	fmt.Printf("uploading %s to Strava...\n", filepath.Base(path))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	url, err := client.Upload(ctx, path, strava.UploadOptions{Name: name})
	if err != nil {
		return err
	}
	fmt.Println("uploaded:", url)
	return nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}

func validPtr(v int) *int {
	if v < 0 {
		return nil
	}
	return &v
}

// stravaUpload uploads stored rides by ID, or TCX files by path.
func stravaUpload(refs []string, name string, force bool) error {
	client, err := strava.Load(credsPath())
	if err != nil {
		return err
	}
	var st *store.Store
	for _, ref := range refs {
		id, err := strconv.ParseInt(ref, 10, 64)
		if err != nil {
			if err := uploadRide(client, ref, name); err != nil {
				return err
			}
			continue
		}
		if st == nil {
			if st, err = store.Open(dbPath()); err != nil {
				return err
			}
			defer st.Close()
		}
		if force {
			if err := st.ClearStravaURL(id); err != nil {
				return err
			}
		}
		fmt.Printf("uploading ride %d to Strava...\n", id)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		url, err := newUploader(st).Strava(ctx, id, name)
		cancel()
		if err != nil {
			return err
		}
		fmt.Println("uploaded:", url)
	}
	return nil
}

// runRides lists recorded rides.
func runRides(args []string) error {
	st, err := store.Open(dbPath())
	if err != nil {
		return err
	}
	defer st.Close()
	rides, err := st.Rides()
	if err != nil {
		return err
	}
	if len(rides) == 0 {
		fmt.Println("no rides yet")
	}
	for _, r := range rides {
		where := "free ride"
		if r.RouteName != nil {
			where = *r.RouteName
		}
		strava := ""
		if r.StravaURL != nil {
			strava = *r.StravaURL
		}
		fmt.Printf("%4d  %s  %8s  %6.2f km  %4.0f W  %-20s %s\n", r.ID, r.StartedAt.Format("2006-01-02 15:04"),
			time.Duration(r.MovingS)*time.Second, r.DistanceM/1000, r.AvgPower, where, strava)
	}
	return nil
}
