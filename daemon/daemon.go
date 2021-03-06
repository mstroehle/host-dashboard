package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/siacentral/host-dashboard/daemon/build"
	"github.com/siacentral/host-dashboard/daemon/cmd"
	"github.com/siacentral/host-dashboard/daemon/persist"
	"github.com/siacentral/host-dashboard/daemon/sync"
	"github.com/siacentral/host-dashboard/daemon/web"
	"github.com/siacentral/host-dashboard/daemon/web/router"
)

var (
	dataPath    string
	listenAddr  string
	siaAddr     string
	disableCors bool
	logStdOut   bool
	logFile     *os.File
)

func writeLine(format string, args ...interface{}) {
	if !logStdOut {
		os.Stdout.WriteString(fmt.Sprintf(format, args...) + "\n")
	}

	log.Printf(format, args...)
}

func init() {
	var err error

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	flag.StringVar(&dataPath, "data-path", "data", "the data path to use")
	flag.StringVar(&listenAddr, "listen-addr", ":8884", "the address to listen on, defaults to :8884")
	flag.StringVar(&siaAddr, "sia-api-addr", "localhost:9980", "the url used to connect to Sia. Defaults to \"localhost:9980\"")
	flag.BoolVar(&disableCors, "disable-cors", false, "disables cross-origin requests, prevents cross-origin browser requests to the API")
	flag.BoolVar(&logStdOut, "std-out", false, "sends output to stdout instead of the log file")
	flag.Parse()

	if err := os.MkdirAll(dataPath, 0770); err != nil && !os.IsExist(err) {
		log.Fatalf("error creating directory: %s", err)
	}

	if err := persist.InitializeDB(dataPath); err != nil {
		log.Fatalf("error initializing database: %s", err)
	}

	if logStdOut {
		return
	}

	logFile, err = os.OpenFile(filepath.Join(dataPath, "log.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)

	if err != nil {
		log.Fatalf("error opening log: %s", err)
	}

	log.SetOutput(logFile)
}

func openbrowser(url string) {
	var err error

	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}

	if err != nil {
		log.Printf("warn: unable to open browser: %s", err)
	}
}

func startAPI() {
	if err := web.Start(router.APIOptions{
		ListenAddress: listenAddr,
		CORS: router.CORSOptions{
			Enabled: !disableCors,
			Origins: []string{"*"},
			Methods: []string{"*"},
		},
		RateInterval: time.Second,
		RateLimit:    10,
	}); err != nil {
		writeLine("Error starting API: %s", err)
		os.Exit(1)
	}
}

func main() {
	var openAddr string

	cmd.StartedInExplorer()

	writeLine("Starting Host Dashboard %s", build.Version)
	writeLine("Revision: %s Build Time: %s", build.GitRevision, build.BuildTimestamp)

	if err := sync.Start(siaAddr); err != nil {
		log.Fatalf("error syncing data: %s", err)
	}

	go startAPI()

	if strings.Index(listenAddr, ":") == 0 {
		openAddr = fmt.Sprintf("http://localhost%s", listenAddr)
	} else {
		openAddr = fmt.Sprintf("http://%s", listenAddr)
	}

	writeLine("Host Dashboard Ready at: %s", openAddr)

	openbrowser(openAddr)

	sigChan := make(chan os.Signal, 1)

	signal.Notify(sigChan, os.Interrupt, os.Kill, syscall.SIGTERM)

	<-sigChan

	writeLine("Shutting down")

	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*5)

	defer cancelFunc()

	if err := web.Shutdown(ctx); err != nil {
		log.Fatalln(err)
	}

	if err := persist.CloseDB(); err != nil {
		log.Fatalln(err)
	}

	if logFile != nil {
		if err := logFile.Close(); err != nil {
			writeLine("closing log: %s", err)
			os.Exit(1)
		}
	}
}
