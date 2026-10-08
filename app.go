package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"caycle/internal/hrm"
	"caycle/internal/route"
	"caycle/internal/server"
	"caycle/internal/session"
	"caycle/internal/store"
	"caycle/internal/trainer"
	"caycle/internal/upload"
)

// appFlags are shared by `ride` and `serve`.
type appFlags struct {
	device     *string
	mode       *string
	resistance *float64
	power      *float64
	grade      *float64
	difficulty *float64
	weight     *float64
	bikeWeight *float64
	wheel      *float64
	hr         *string
	route      *string
	web        *string
	demo       *bool
}

func addAppFlags(fs *flag.FlagSet, defaultWeb string) *appFlags {
	return &appFlags{
		device:     fs.String("device", "", "trainer name substring or address (default: first trainer found)"),
		mode:       fs.String("mode", "resistance", "start mode: resistance, erg, sim or route"),
		resistance: fs.Float64("resistance", 10, "start resistance in %"),
		power:      fs.Float64("power", 150, "start ERG target power in W"),
		grade:      fs.Float64("grade", 0, "start grade in % (sim mode)"),
		difficulty: fs.Float64("difficulty", 100, "% of the route grade applied in route mode"),
		weight:     fs.Float64("weight", 75, "rider weight in kg (used by the trainer for grades)"),
		bikeWeight: fs.Float64("bike-weight", 9, "bike weight in kg"),
		wheel:      fs.Float64("wheel", 0.70, "wheel diameter in m"),
		hr:         fs.String("hr", "", `heart rate sensor name or address (default: first one found, "off" to disable)`),
		route:      fs.String("route", "", "route to ride: a GPX file (imported into the library) or a route ID"),
		web:        fs.String("web", defaultWeb, `web dashboard listen address ("off" to disable)`),
		demo:       fs.Bool("demo", false, "use a simulated trainer instead of Bluetooth"),
	}
}

// app is a running session with its store, devices and web server.
type app struct {
	store    *store.Store
	sess     *session.Session
	uploader *upload.Uploader
	stop     chan struct{}
	urls     []string
}

func dbPath() string { return filepath.Join(appDir(), "caycle.db") }

func newUploader(st *store.Store) *upload.Uploader {
	return &upload.Uploader{Store: st, CredsPath: credsPath(), RidesDir: filepath.Join(appDir(), "rides")}
}

func startApp(f *appFlags) (*app, error) {
	mode := session.Mode(strings.ToLower(*f.mode))
	switch mode {
	case session.Resistance, session.ERG, session.Sim, session.RouteMode:
	default:
		return nil, fmt.Errorf("unknown mode %q (want resistance, erg, sim or route)", *f.mode)
	}

	st, err := store.Open(dbPath())
	if err != nil {
		return nil, err
	}
	if err := st.FinishOpenRides(); err != nil {
		st.Close()
		return nil, err
	}
	a := &app{store: st, uploader: newUploader(st), stop: make(chan struct{})}
	fail := func(err error) (*app, error) {
		a.close()
		return nil, err
	}

	cfg := session.Config{
		Store: st, Mode: session.Resistance, Resistance: *f.resistance, Power: *f.power,
		Grade: *f.grade, Difficulty: *f.difficulty,
	}
	if *f.demo {
		demo := trainer.NewDemo()
		cfg.Trainer = func() (session.Device, string) { return demo, "connected" }
	} else {
		adapter, err := enableAdapter()
		if err != nil {
			return fail(err)
		}
		link := trainer.Keep(adapter, *f.device, func(t *trainer.Trainer) {
			t.SetUserConfig(*f.weight, *f.bikeWeight, *f.wheel)
		}, a.stop)
		cfg.Trainer = func() (session.Device, string) {
			t, status := link.Current()
			if t == nil {
				return nil, status
			}
			return t, status
		}
		if *f.hr != "off" {
			cfg.HeartRate = hrm.Run(adapter, *f.hr, a.stop).Reading
		}
	}
	if mode != session.RouteMode {
		cfg.Mode = mode
	}
	a.sess = session.New(cfg)

	if *f.route != "" {
		id, err := resolveRoute(st, *f.route)
		if err != nil {
			return fail(err)
		}
		if err := a.sess.SelectRoute(&id); err != nil {
			return fail(err)
		}
	} else if mode == session.RouteMode {
		return fail(errors.New("route mode needs -route"))
	}
	go a.sess.Run(a.stop)

	if *f.web != "off" {
		ln, err := net.Listen("tcp", *f.web)
		if err != nil {
			return fail(fmt.Errorf("web dashboard: %w", err))
		}
		srv := &http.Server{Handler: (&server.Server{Session: a.sess, Store: st, Uploader: a.uploader}).Handler()}
		go srv.Serve(ln)
		go func() { <-a.stop; srv.Close() }()
		a.urls = dashboardURLs(ln.Addr().(*net.TCPAddr))
	}
	return a, nil
}

func (a *app) close() {
	select {
	case <-a.stop:
	default:
		close(a.stop)
	}
	a.store.Close()
}

// resolveRoute accepts a stored route ID or a GPX file to import.
func resolveRoute(st *store.Store, ref string) (int64, error) {
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		if _, _, err := st.Route(id); err != nil {
			return 0, fmt.Errorf("route %d: %w", id, err)
		}
		return id, nil
	}
	f, err := os.Open(ref)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	rt, err := route.ParseGPX(f)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", ref, err)
	}
	if rt.Name == "" {
		rt.Name = strings.TrimSuffix(filepath.Base(ref), filepath.Ext(ref))
	}
	info, err := st.AddRoute(rt)
	return info.ID, err
}

// dashboardURLs lists URLs to open the dashboard from this machine and from
// phones on the same network.
func dashboardURLs(addr *net.TCPAddr) []string {
	port := strconv.Itoa(addr.Port)
	if !addr.IP.IsUnspecified() {
		return []string{"http://" + net.JoinHostPort(addr.IP.String(), port)}
	}
	urls := []string{"http://localhost:" + port}
	ifaddrs, _ := net.InterfaceAddrs()
	for _, ia := range ifaddrs {
		if ipn, ok := ia.(*net.IPNet); ok && ipn.IP.To4() != nil && ipn.IP.IsPrivate() {
			urls = append(urls, "http://"+net.JoinHostPort(ipn.IP.String(), port))
		}
	}
	return urls
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	f := addAppFlags(fs, ":8080")
	fs.Parse(args)
	if *f.web == "off" {
		return errors.New("serve needs the web dashboard; use `caycle ride -web off` for terminal only")
	}
	a, err := startApp(f)
	if err != nil {
		return err
	}
	defer a.close()
	fmt.Println("dashboard:", strings.Join(a.urls, "  "))
	fmt.Println("press Ctrl-C to stop")

	sigs := make(chan os.Signal, 1)
	notifySignals(sigs)
	<-sigs
	if _, err := a.sess.FinishRide(); err == nil {
		fmt.Println("ride saved")
	}
	time.Sleep(100 * time.Millisecond) // let the trainer disconnect cleanly
	return nil
}
