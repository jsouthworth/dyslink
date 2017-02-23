package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/hashicorp/mdns"
	"github.com/jsouthworth/dyslink"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"text/tabwriter"
)

const variadic = -1

var host, user, pass, model string
var debug bool

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <command> <args> \n", os.Args[0])
		flag.PrintDefaults()
		usage()
	}
	flag.StringVar(&host, "address", "", "Address [required]")
	flag.StringVar(&user, "user", "", "Username")
	flag.StringVar(&pass, "pass", "", "Password")
	flag.StringVar(&model, "model", "", "Device Model [required]")
	flag.BoolVar(&debug, "debug", false, "Enable debugging")
	log.SetOutput(ioutil.Discard)
}

type client struct {
	client       dyslink.Client
	callbackChan chan *dyslink.MessageCallback
}

func bootstrap(client *client, args ...string) {
	var ssid, key string

	fset := flag.NewFlagSet("bootstrap", flag.ExitOnError)
	fset.StringVar(&ssid, "ssid", "", "SSID [required]")
	fset.StringVar(&key, "key", "", "Wireless password [required]")
	fset.Usage = func() {
		fmt.Fprintf(os.Stderr,
			"Usage: %s [flags] bootstrap <args> \n", os.Args[0])
		fset.PrintDefaults()
	}
	handleError(fset.Parse(args))
	if ssid == "" || key == "" {
		fset.Usage()
		os.Exit(2)
	}

	client.client.WifiBootstrap(ssid, key)
	msg := <-client.callbackChan
	handleError(msg.Error)
	switch v := msg.Message.(type) {
	case *dyslink.DeviceCredentials:
		fmt.Println("Credentials for device are: SN:", v.SerialNumber,
			"Password:", v.Password)
	}
}

func setFanMode(client *client, args ...string) {}
func setSpeed(client *client, args ...string)   {}
func setOscillate(client *client, args ...string) {
	ostate := args[0]
	if ostate != dyslink.OscillateOn &&
		ostate != dyslink.OscillateOff {
		handleError(errors.New("Invalid oscillation state " + ostate))
	}
	s := &dyslink.FanState{
		Oscillate: ostate,
	}
	handleError(client.client.SetState(s))
}
func setMonitor(client *client, args ...string)      {}
func setAirQuality(client *client, args ...string)   {}
func setNightMode(client *client, args ...string)    {}
func resetFilterLife(client *client, args ...string) {}
func getState(client *client, args ...string) {
	handleError(client.client.RequestCurrentState())

	for num_msg := 0; num_msg < 2; num_msg++ {
		msg := <-client.callbackChan
		handleError(msg.Error)
		switch v := msg.Message.(type) {
		case *dyslink.ProductState:
			fmt.Printf("%#v\n", *v)
		case *dyslink.EnvironmentState:
			fmt.Printf("%#v\n", *v)
		}
	}
}

func discover(client *client, args ...string) {
	entriesCh := make(chan *mdns.ServiceEntry, 4)
	go func() {
		for entry := range entriesCh {
			fmt.Println("Name:", entry.Name)
			fmt.Println("Host:", entry.Host)
			fmt.Println("IP:", entry.AddrV4)
			fmt.Println("Port:", entry.Port)
			fmt.Printf("Address: tcp://%v:%v\n", entry.AddrV4, entry.Port)
			fmt.Println()
		}
	}()

	// Start the lookup
	mdns.Lookup("_dyson_mqtt._tcp", entriesCh)
	close(entriesCh)
}

type cmd struct {
	fn      func(*client, ...string)
	info    string
	nargs   int
	connect bool
}

var cmds = map[string]*cmd{
	"bootstrap": {
		bootstrap, "Bootstrap a new device", variadic, true},
	"set-fan-mode": {
		setFanMode, "Set the mode of the fan", 1, true},
	"set-speed": {
		setSpeed, "Set fan speed", 1, true},
	"set-oscillate": {
		setOscillate, "Toggle oscillation", 1, true},
	"set-monitor": {
		setMonitor, "Toggle standby monitoring", 1, true},
	"set-air-quality-target": {
		setAirQuality, "Set Air Quality Target in auto mode", 1, true},
	"set-night-mode": {
		setNightMode, "Toggle night mode", 1, true},
	"reset-filter-lifetime": {
		resetFilterLife, "Reset the filter's lifetime", 0, true},
	"get-current-state": {
		getState, "Request the current state from the device", 0, true},
	"discover": {
		discover, "Find all Dyson Purifiers", 0, false},
}

func usage() {
	w := tabwriter.NewWriter(os.Stderr, 0, 8, 2, '\t', 0)
	fmt.Fprintln(w, "Availble commands:")
	cmdnames := make([]string, 0, len(cmds))
	for name, _ := range cmds {
		cmdnames = append(cmdnames, name)
	}
	sort.Sort(sort.StringSlice(cmdnames))
	for _, name := range cmdnames {
		fmt.Fprintf(w, "  %s\t%s\n", name, cmds[name].info)
	}
	w.Flush()
}

func validModel(model string) bool {
	return model == dyslink.TypeModelN475 ||
		model == dyslink.TypeModelN469 ||
		model == dyslink.TypeModelN455
}

func handleError(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func main() {
	flag.Parse()
	args := flag.Args()

	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Must supply command")
		flag.Usage()
		os.Exit(1)
	}
	cmdin := args[0]

	cmd, ok := cmds[cmdin]
	if !ok {
		fmt.Fprintln(os.Stderr, "Invalid command")
		flag.Usage()
		os.Exit(1)
	}
	if len(args)-1 < cmd.nargs {
		fmt.Fprintln(os.Stderr, "Invalid number of arguements to", cmdin, "needs", cmd.nargs)
		os.Exit(1)
	}

	ch := make(chan *dyslink.MessageCallback)
	c := dyslink.NewClient(&dyslink.ClientOpts{
		DeviceAddress: host,
		Username:      user,
		Password:      pass,
		Model:         model,
		Debug:         debug,
		CallbackChan:  ch,
	})
	if cmd.connect {
		if !validModel(model) {
			fmt.Fprintln(os.Stderr, "Must supply model type")
			flag.Usage()
			os.Exit(1)
		}
		if host == "" {
			fmt.Fprintln(os.Stderr, "Must supply address")
			flag.Usage()
			os.Exit(1)
		}
		handleError(c.Connect())
	}

	cmd.fn(&client{c, ch}, args[1:]...)
}
