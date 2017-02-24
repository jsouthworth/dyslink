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
	"reflect"
	"sort"
	"strconv"
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
}

func setFanMode(client *client, args ...string) {
	fmode := args[0]
	if fmode != dyslink.FanModeOn &&
		fmode != dyslink.FanModeOff &&
		fmode != dyslink.FanModeAuto {
		handleError(errors.New("Invalid Fan mode " + fmode))
	}
	s := &dyslink.FanState{
		FanMode: fmode,
	}
	handleError(client.client.SetState(s))
}

func setSpeed(client *client, args ...string) {
	speed := args[0]
	sval, err := strconv.Atoi(speed)
	if err != nil {
		handleError(err)
	}
	if sval < 1 || sval > 10 {
		handleError(errors.New("Invalid fan speed " + speed))
	}
	s := &dyslink.FanState{
		FanSpeed: speed,
	}
	handleError(client.client.SetState(s))
}

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

func setMonitor(client *client, args ...string) {
	mstate := args[0]
	if mstate != dyslink.StandbyMonitorOn &&
		mstate != dyslink.StandbyMonitorOff {
		handleError(errors.New("Invalid monitor state " + mstate))
	}
	s := &dyslink.FanState{
		StandbyMonitoring: mstate,
	}
	handleError(client.client.SetState(s))
}

func setFocusedMode(client *client, args ...string) {
	fmode := args[0]
	if fmode != dyslink.FocusedModeOn &&
		fmode != dyslink.FocusedModeOff {
		handleError(errors.New("Invalid focused mode " + fmode))
	}
	s := &dyslink.FanState{
		FocusedMode: fmode,
	}
	handleError(client.client.SetState(s))
}

func setTemp(client *client, args ...string) {
	temp := args[0]
	sval, err := strconv.Atoi(temp)
	if err != nil {
		handleError(err)
	}
	if sval == 0 {
		s := &dyslink.FanState{
			HeatMode: "OFF",
		}
		handleError(client.client.SetState(s))
		return
	}
	if sval < 33 || sval > 99 {
		handleError(errors.New("Invalid fan temp " + temp))
	}
	s := &dyslink.FanState{
		HeatMode:   "HEAT",
		HeatTarget: strconv.Itoa(dyslink.ConvertTempFromFahr(sval)),
	}
	handleError(client.client.SetState(s))
}

func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Ptr:
		return v.IsNil()
	}
	return false
}

func printStruct(v reflect.Value) {
	vtype := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if isEmptyValue(field) {
			continue
		}
		sfield := vtype.Field(i)
		fmt.Printf("%s: %v\n", sfield.Name, field.Interface())
	}
}

func printProductState(state *dyslink.ProductState) {
	fmt.Println("Product State:")
	fmt.Println("--------------")
	v := reflect.ValueOf(state).Elem()
	printStruct(v)
}

func printEnvironmentState(state *dyslink.EnvironmentState) {
	fmt.Println("Environment State:")
	fmt.Println("--------------")
	v := reflect.ValueOf(state).Elem()
	printStruct(v)
}

func getState(client *client, args ...string) {
	handleError(client.client.RequestCurrentState())
	for num_msg := 0; num_msg < 2; num_msg++ {
		msg := <-client.callbackChan
		handleError(msg.Error)
		switch v := msg.Message.(type) {
		case *dyslink.ProductState:
			printProductState(v)
		case *dyslink.EnvironmentState:
			printEnvironmentState(v)
		}
		fmt.Println()
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
	"discover": {
		discover, "Find all Dyson Purifiers", 0, false},
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
	"set-temp": {
		setTemp, "Set temperature", 1, true},
	"set-focused-mode": {
		setFocusedMode, "Set focused mode", 1, true},
	"get-current-state": {
		getState, "Request the current state from the device", 0, true},
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
