package main

import (
	"flag"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
)

var proxyRoot, jailRoot, accelRedirect string
var jplayerUrl, jplayerPath string
var useJPlayer bool

func removeIfStartsWith(s, start string) string {
	if !strings.HasPrefix(s, start) {
		return s
	}
	return s[len(start):]
}

func followSymlink(localPath string, dfi os.FileInfo) os.FileInfo {
	// Check symlink:
	if (dfi.Mode() & os.ModeSymlink) != 0 {

		dfiPath := path.Join(localPath, dfi.Name())
		if targetPath, err := os.Readlink(dfiPath); err == nil {
			// Find the absolute path of the symlink's target:
			if !path.IsAbs(targetPath) {
				targetPath = path.Join(localPath, targetPath)
			}
			if tdfi, err := os.Stat(targetPath); err == nil {
				// Change to the target so we get its properties instead of the symlink's:
				return tdfi
			}
		}
	}

	return dfi
}

func isMP3(filename string) bool {
	ext := path.Ext(filename)
	mt := mime.TypeByExtension(ext)
	if mt != "audio/mpeg" {
		return false
	}
	if ext != ".mp3" {
		return false
	}
	return true
}

func main() {
	var socketType string
	var socketAddr string

	// TODO(jsd): Make this pair of arguments a little more elegant, like "unix:/path/to/socket" or "tcp://:8080"
	flag.StringVar(&socketType, "l", "tcp", `type of socket to listen on; "unix" or "tcp" (default)`)
	flag.StringVar(&socketAddr, "a", ":8080", `address to listen on; ":8080" (default TCP port) or "/path/to/unix/socket"`)
	flag.StringVar(&proxyRoot, "p", "/", "root of web requests to process")
	flag.StringVar(&jailRoot, "r", ".", "local filesystem path to bind to web request root path")
	flag.StringVar(&accelRedirect, "xa", "", "Root of X-Accel-Redirect paths to use)")
	flag.StringVar(&jplayerUrl, "jp-url", "", `Web path to jPlayer files (e.g. "/js")`)
	flag.StringVar(&jplayerPath, "jp-path", "", `Local filesystem path to jPlayer files`)
	flag.Parse()

	if jplayerUrl != "" {
		useJPlayer = true
	}

	// Create the socket to listen on:
	l, err := net.Listen(socketType, socketAddr)
	if err != nil {
		log.Fatal(err)
		return
	}

	// NOTE(jsd): Unix sockets must be unlink()ed before being reused again.

	// Handle common process-killing signals so we can gracefully shut down:
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, os.Kill, syscall.SIGTERM)
	go func(c chan os.Signal) {
		// Wait for a signal:
		sig := <-c
		log.Printf("Caught signal '%s': shutting down.", sig)
		// Stop listening:
		l.Close()
		// Delete the unix socket, if applicable:
		if socketType == "unix" {
			os.Remove(socketAddr)
		}
		// And we're done:
		os.Exit(0)
	}(sigc)

	// Start the HTTP server:
	log.Fatal(http.Serve(l, http.HandlerFunc(processRequest)))
}
