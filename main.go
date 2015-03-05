package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"sort"
	"strings"
	"syscall"
)

var proxyRoot, jailRoot, accelRedirect string
var jplayerUrl, jplayerPath string
var useJPlayer bool

func startsWith(s, start string) bool {
	if len(s) < len(start) {
		return false
	}
	return s[0:len(start)] == start
}

func removeIfStartsWith(s, start string) string {
	if !startsWith(s, start) {
		return s
	}
	return s[len(start):]
}

func translateForProxy(s string) string {
	return path.Join(proxyRoot, removeIfStartsWith(s, jailRoot))
}

// For directory entry sorting:

type Entries []os.FileInfo

func (s Entries) Len() int      { return len(s) }
func (s Entries) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

type sortBy int

const (
	sortByName sortBy = iota
	sortByDate
	sortBySize
)

type sortDirection int

const (
	sortAscending sortDirection = iota
	sortDescending
)

// Sort by name:
type ByName struct {
	Entries
	dir sortDirection
}

func (s ByName) Less(i, j int) bool {
	if s.Entries[i].IsDir() && !s.Entries[j].IsDir() {
		return true
	}
	if !s.Entries[i].IsDir() && s.Entries[j].IsDir() {
		return false
	}

	if s.dir == sortAscending {
		return s.Entries[i].Name() < s.Entries[j].Name()
	} else {
		return s.Entries[i].Name() > s.Entries[j].Name()
	}
}

// Sort by last modified time:
type ByDate struct {
	Entries
	dir sortDirection
}

func (s ByDate) Less(i, j int) bool {
	if s.Entries[i].IsDir() && !s.Entries[j].IsDir() {
		return true
	}
	if !s.Entries[i].IsDir() && s.Entries[j].IsDir() {
		return false
	}

	if s.dir == sortAscending {
		return s.Entries[i].ModTime().Before(s.Entries[j].ModTime())
	} else {
		return s.Entries[i].ModTime().After(s.Entries[j].ModTime())
	}
}

// Sort by size:
type BySize struct {
	Entries
	dir sortDirection
}

func (s BySize) Less(i, j int) bool {
	if s.Entries[i].IsDir() && !s.Entries[j].IsDir() {
		return true
	}
	if !s.Entries[i].IsDir() && s.Entries[j].IsDir() {
		return false
	}

	if s.dir == sortAscending {
		return s.Entries[i].Size() < s.Entries[j].Size()
	} else {
		return s.Entries[i].Size() > s.Entries[j].Size()
	}
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

// Logging+action functions
func doError(req *http.Request, rsp http.ResponseWriter, msg string, code int) {
	http.Error(rsp, msg, code)
}

func doRedirect(req *http.Request, rsp http.ResponseWriter, url string, code int) {
	http.Redirect(rsp, req, url, code)
}

func doOK(req *http.Request, msg string, code int) {
}

// Marshal an object to JSON or panic.
func marshal(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
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

func generateIndexHtml(rsp http.ResponseWriter, req *http.Request, u *url.URL) {
	// Build index.html
	relPath := removeIfStartsWith(u.Path, proxyRoot)

	localPath := path.Join(jailRoot, relPath)
	pathLink := path.Join(proxyRoot, relPath)

	baseDir := path.Dir(localPath)
	if localPath[len(localPath)-1] == '/' {
		baseDir = path.Dir(localPath[0 : len(localPath)-1])
	}
	if baseDir == "" {
		baseDir = "/"
	}

	// Determine what mode to sort by...
	sortString := ""

	// Check the .index-sort file:
	if sf, err := os.Open(path.Join(localPath, ".index-sort")); err == nil {
		defer sf.Close()
		scanner := bufio.NewScanner(sf)
		if scanner.Scan() {
			sortString = scanner.Text()
		}
	}

	// Use query-string 'sort' to override sorting:
	sortStringQuery := u.Query().Get("sort")
	if sortStringQuery != "" {
		sortString = sortStringQuery
	}

	// default Sort mode for headers
	nameSort := "name-asc"
	dateSort := "date-asc"
	sizeSort := "size-asc"

	// Determine the sorting mode:
	sortBy, sortDir := sortByName, sortAscending
	switch sortString {
	case "size-desc":
		sortBy, sortDir = sortBySize, sortDescending
	case "size-asc":
		sortBy, sortDir = sortBySize, sortAscending
		sizeSort = "size-desc"
	case "date-desc":
		sortBy, sortDir = sortByDate, sortDescending
	case "date-asc":
		sortBy, sortDir = sortByDate, sortAscending
		dateSort = "date-desc"
	case "name-desc":
		sortBy, sortDir = sortByName, sortDescending
	case "name-asc":
		sortBy, sortDir = sortByName, sortAscending
		nameSort = "name-desc"
	default:
	}

	// Open the directory to read its contents:
	f, err := os.Open(localPath)
	if err != nil {
		doError(req, rsp, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	// Read the directory entries:
	fis, err := f.Readdir(0)
	if err != nil {
		doError(req, rsp, err.Error(), http.StatusInternalServerError)
		return
	}

	// Check if there are MP3s in this directory:
	hasMP3s := false
	if useJPlayer {
		for _, dfi := range fis {
			dfi = followSymlink(localPath, dfi)
			if !isMP3(dfi.Name()) {
				continue
			}
			hasMP3s = true
			break
		}
	}

	// Sort the entries by the desired mode:
	switch sortBy {
	default:
		sort.Sort(ByName{fis, sortDir})
	case sortByName:
		sort.Sort(ByName{fis, sortDir})
	case sortByDate:
		sort.Sort(ByDate{fis, sortDir})
	case sortBySize:
		sort.Sort(BySize{fis, sortDir})
	}

	// TODO: check Accepts header to reply accordingly (i.e. add JSON support)

	pathHtml := html.EscapeString(pathLink)

	rsp.Header().Add("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(rsp, `<!DOCTYPE html>

<html>
<head>
  <title>%s</title>
  <style type="text/css">
a, a:active {text-decoration: none; color: blue;}
a:visited {color: #48468F;}
a:hover, a:focus {text-decoration: underline; color: red;}
body {background-color: #F5F5F5;}
h2 {margin-bottom: 12px;}
table {margin-left: 12px;}
th, td { font: 90%% monospace; text-align: left;}
th { font-weight: bold; padding-right: 14px; padding-bottom: 3px;}
td {padding-right: 14px;}
td.n, th.n {white-space: nowrap;}
td.m, th.m {white-space: nowrap;}
td.s, th.s {white-space: nowrap; text-align: right;}
div.list { background-color: white; border-top: 1px solid #646464; border-bottom: 1px solid #646464; padding-top: 10px; padding-bottom: 14px;}
div.foot { font: 90%% monospace; color: #787878; padding-top: 4px;}
  </style>
`, pathHtml)

	if hasMP3s {
		fmt.Fprintf(rsp, `
  <link href="%[1]s/jplayer.blue.monday.css" rel="stylesheet" type="text/css" />
  <script type="text/javascript" src="//code.jquery.com/jquery-1.11.0.min.js"></script>
  <script type="text/javascript" src="%[1]s/jquery.jplayer.min.js"></script>
  <script type="text/javascript" src="%[1]s/jplayer.playlist.min.js"></script>
  <script type="text/javascript">
    $(function() {
      new jPlayerPlaylist({ jPlayer: "#jplayer" }, [
`, jplayerUrl)

		// Generate jPlayer playlist:
		first := true
		for _, dfi := range fis {
			name := dfi.Name()
			if name[0] == '.' {
				continue
			}

			dfi = followSymlink(localPath, dfi)

			dfiPath := path.Join(localPath, name)
			href := translateForProxy(dfiPath)

			if dfi.IsDir() {
				continue
			}

			if !isMP3(name) {
				continue
			}

			if !first {
				fmt.Fprintf(rsp, ", ")
			} else {
				fmt.Fprintf(rsp, "  ")
			}

			ext := path.Ext(name)
			onlyname := name
			if ext != "" {
				onlyname = name[0 : len(name)-len(ext)]
			}

			fmt.Fprintf(rsp, "{ title: %s, mp3: %s }\n",
				marshal(onlyname),
				marshal(href),
			)
			first = false
		}

		// End playlist:
		fmt.Fprintf(rsp, `
      ], {
        swfPath: "/js",
		supplied: "mp3",
		wmode: "window"
      });

	});
  </script>
`)
	}

	fmt.Fprintf(rsp, `
</head>`)

	fmt.Fprintf(rsp, `
<body>
  <h2>Index of %s</h2>`, pathHtml)

	if hasMP3s {
		fmt.Fprintf(rsp, `
  <div id="jplayer" class="jp-jplayer"></div>

  <div id="jp_container_1" class="jp-audio" style="float: left; margin-right:12px;">
    <div class="jp-type-playlist">
        <div class="jp-gui jp-interface">
            <ul class="jp-controls">
                <li><a href="javascript:;" class="jp-previous" tabindex="1">previous</a></li>
                <li><a href="javascript:;" class="jp-play" tabindex="1">play</a></li>
                <li><a href="javascript:;" class="jp-pause" tabindex="1">pause</a></li>
                <li><a href="javascript:;" class="jp-next" tabindex="1">next</a></li>
                <li><a href="javascript:;" class="jp-stop" tabindex="1">stop</a></li>
                <li><a href="javascript:;" class="jp-mute" tabindex="1" title="mute">mute</a></li>
                <li><a href="javascript:;" class="jp-unmute" tabindex="1" title="unmute">unmute</a></li>
                <li><a href="javascript:;" class="jp-volume-max" tabindex="1" title="max volume">max volume</a></li>
            </ul>
            <div class="jp-progress">
                <div class="jp-seek-bar">
                    <div class="jp-play-bar"></div>
                </div>
            </div>
            <div class="jp-volume-bar">
                <div class="jp-volume-bar-value"></div>
            </div>
            <div class="jp-time-holder">
                <div class="jp-current-time"></div>
                <div class="jp-duration"></div>
            </div>
            <ul class="jp-toggles">
                <li><a href="javascript:;" class="jp-shuffle" tabindex="1" title="shuffle">shuffle</a></li>
                <li><a href="javascript:;" class="jp-shuffle-off" tabindex="1" title="shuffle off">shuffle off</a></li>
                <li><a href="javascript:;" class="jp-repeat" tabindex="1" title="repeat">repeat</a></li>
                <li><a href="javascript:;" class="jp-repeat-off" tabindex="1" title="repeat off">repeat off</a></li>
            </ul>
        </div>
        <div class="jp-playlist">
            <ul>
                <li></li>
            </ul>
        </div>
        <div class="jp-no-solution">
            <span>Update Required</span>
            To play the media you will need to either update your browser to a recent version or update your <a href="http://get.adobe.com/flashplayer/" target="_blank">Flash plugin</a>.
        </div>
    </div>
  </div>
`)
	}

	fmt.Fprintf(rsp, `
  <div class="list" style="overflow: auto">
    <table cellpadding="0" cellspacing="0" summary="Directory Listing">
      <thead>
        <tr>
          <th class="n"><a href="%s?sort=%s">Name</a></th>
          <th class="m"><a href="%s?sort=%s">Last Modified</a></th>
          <th class="s"><a href="%s?sort=%s">Size</a></th>
          <th class="t">Type</th>
        </tr>
      </thead>
      <tbody>`, pathHtml, nameSort, pathHtml, dateSort, pathHtml, sizeSort)

	// Add the Parent Directory link if we're above the jail root:
	if startsWith(baseDir, jailRoot) {
		hrefParent := translateForProxy(baseDir) + "/"
		fmt.Fprintf(rsp, `
        <tr>
          <td class="n"><a href="%s">../</a></td>
          <td class="m"></td>
          <td class="s"></td>
          <td class="t">Directory</td>
        </tr>`, hrefParent)
	}

	for _, dfi := range fis {
		name := dfi.Name()
		if name[0] == '.' {
			continue
		}

		dfiPath := path.Join(localPath, name)
		dfi = followSymlink(localPath, dfi)

		href := translateForProxy(dfiPath)
		mt := mime.TypeByExtension(path.Ext(dfi.Name()))

		sizeText := ""
		if dfi.IsDir() {
			sizeText = "-"
			name += "/"
			href += "/"
		} else {
			size := dfi.Size()
			if size < 1024 {
				sizeText = fmt.Sprintf("%d  B", size)
			} else if size < 1024*1024 {
				sizeText = fmt.Sprintf("%.02f KB", float64(size)/1024.0)
			} else if size < 1024*1024*1024 {
				sizeText = fmt.Sprintf("%.02f MB", float64(size)/(1024.0*1024.0))
			} else {
				sizeText = fmt.Sprintf("%.02f GB", float64(size)/(1024.0*1024.0*1024.0))
			}
		}

		fmt.Fprintf(rsp, `
        <tr>
          <td class="n"><a href="%s">%s</a></td>
          <td class="m">%s</td>
          <td class="s">%s</td>
          <td class="t">%s</td>
        </tr>`,
			html.EscapeString(href),
			html.EscapeString(name),
			html.EscapeString(dfi.ModTime().Format("2006-01-02 15:04:05 -0700 MST")),
			strings.Replace(html.EscapeString(sizeText), " ", "&nbsp;", -1),
			html.EscapeString(mt),
		)
	}

	fmt.Fprintf(rsp, `
      </tbody>
    </table>
  </div>
</body>
</html>`)

	doOK(req, localPath, http.StatusOK)
	return
}

func processProxiedRequest(rsp http.ResponseWriter, req *http.Request, u *url.URL) {
	relPath := removeIfStartsWith(u.Path, proxyRoot)
	localPath := path.Join(jailRoot, relPath)

	// Check if the requested path is a symlink:
	fi, err := os.Lstat(localPath)
	if fi != nil && (fi.Mode()&os.ModeSymlink) != 0 {
		localDir := path.Dir(localPath)

		// Check if file is a symlink and do 302 redirect:
		linkDest, err := os.Readlink(localPath)
		if err != nil {
			doError(req, rsp, err.Error(), http.StatusBadRequest)
			return
		}

		// NOTE(jsd): Problem here for links outside the jail folder.
		if path.IsAbs(linkDest) && !startsWith(linkDest, jailRoot) {
			doError(req, rsp, "Symlink points outside of jail", http.StatusBadRequest)
			return
		}

		linkDest = path.Join(localDir, linkDest)
		tp := translateForProxy(linkDest)

		doRedirect(req, rsp, tp, http.StatusFound)
		return
	}

	// Regular stat
	fi, err = os.Stat(localPath)
	if err != nil {
		doError(req, rsp, err.Error(), http.StatusNotFound)
		return
	}

	// Serve the file if it is regular:
	if fi.Mode().IsRegular() {
		// Send file:

		// NOTE(jsd): using `http.ServeFile` does not appear to handle range requests well. Lots of broken pipe errors
		// that lead to a poor client experience. X-Accel-Redirect back to nginx is much better.

		if accelRedirect != "" {
			// Use X-Accel-Redirect if the cmdline option was given:
			redirPath := path.Join(accelRedirect, relPath)
			rsp.Header().Add("X-Accel-Redirect", redirPath)
			rsp.Header().Add("Content-Type", mime.TypeByExtension(path.Ext(localPath)))
			rsp.WriteHeader(200)
		} else {
			// Just serve the file directly from the filesystem:
			http.ServeFile(rsp, req, localPath)
		}

		return
	}

	// Generate an index.html for directories:
	if fi.Mode().IsDir() {
		dl := u.Query().Get("dl")
		if dl != "" {
			switch dl {
			default:
				fallthrough
			case "zip":
				downloadZip(rsp, req, u, &fi, localPath)
				//case "tar":
				//	downloadTar(rsp, req, u, &fi, localPath)
			}
			return
		}

		generateIndexHtml(rsp, req, u)
		return
	}
}

// Serves an index.html file for a directory or sends the requested file.
func processRequest(rsp http.ResponseWriter, req *http.Request) {
	// proxy sends us absolute path URLs
	u, err := url.Parse(req.RequestURI)
	if err != nil {
		log.Fatal(err)
	}

	if (jplayerPath != "") && startsWith(u.Path, jplayerUrl) {
		// URL is under the jPlayer path:
		localPath := path.Join(jplayerPath, removeIfStartsWith(u.Path, jplayerUrl))
		http.ServeFile(rsp, req, localPath)
		return
	} else if startsWith(u.Path, proxyRoot) {
		// URL is under the proxy path:
		processProxiedRequest(rsp, req, u)
		return
	}
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
