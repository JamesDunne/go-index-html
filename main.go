package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"strings"
)

import "github.com/JamesDunne/go-util/base"
import "github.com/JamesDunne/go-util/web"

var proxyRoot, jailRoot, accelRedirect string
var jplayerUrl, jplayerPath string
var useJPlayer bool

var html_path string

func removeIfStartsWith(s, start string) string {
	if !strings.HasPrefix(s, start) {
		return s
	}
	return s[len(start):]
}

func main() {
	flag.StringVar(&html_path, "html", "./html", "local path to html templates")
	flag.StringVar(&proxyRoot, "p", "/", "root of web requests to process")
	flag.StringVar(&jailRoot, "r", ".", "local filesystem path to bind to web request root path")
	flag.StringVar(&accelRedirect, "xa", "", "Root of X-Accel-Redirect paths to use)")
	flag.StringVar(&jplayerUrl, "jp-url", "", `Web path to jPlayer files (e.g. "/js")`)
	flag.StringVar(&jplayerPath, "jp-path", "", `Local filesystem path to jPlayer files`)

	fl_listen_uri := flag.String("l", "tcp://0.0.0.0:8080", "listen URI (schemes available are tcp, unix)")
	flag.Parse()

	if jplayerUrl != "" {
		useJPlayer = true
	}

	listen_addr, err := base.ParseListenable(*fl_listen_uri)
	base.PanicIf(err)

	// Watch the html templates for changes and reload them:
	_, cleanup, err := web.WatchTemplates("ui", html_path, "*.html", nil, &uiTmpl)
	if err != nil {
		log.Println(err)
		return
	}
	defer cleanup()

	// Start the server:
	_, err = base.ServeMain(listen_addr, func(l net.Listener) error {
		return http.Serve(l, web.ReportErrors(web.Log(web.DefaultErrorLog, web.ErrorHandlerFunc(processRequest))))
	})
	if err != nil {
		log.Println(err)
		return
	}
}
