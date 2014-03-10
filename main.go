package main

import (
	"bufio"
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

// Logging+action functions
func doError(req *http.Request, rsp http.ResponseWriter, msg string, code int) {
	http.Error(rsp, msg, code)
}

func doRedirect(req *http.Request, rsp http.ResponseWriter, url string, code int) {
	http.Redirect(rsp, req, url, code)
}

func doOK(req *http.Request, msg string, code int) {
}

// Serves an index.html file for a directory or sends the requested file.
func indexHtml(rsp http.ResponseWriter, req *http.Request) {
	// lighttpd proxy sends us absolute path URLs
	u, err := url.Parse(req.RequestURI)
	if err != nil {
		log.Fatal(err)
	}

	relPath := removeIfStartsWith(u.Path, proxyRoot)

	localPath := path.Join(jailRoot, relPath)
	pathLink := path.Join(proxyRoot, relPath)

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

		redirPath := path.Join(accelRedirect, relPath)
		rsp.Header().Add("X-Accel-Redirect", redirPath)
		rsp.Header().Add("Content-Type", mime.TypeByExtension(path.Ext(localPath)))
		rsp.WriteHeader(200)

		return
	}

	// Generate an index.html for directories:
	if fi.Mode().IsDir() {
		// Build index.html

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
td.s, th.s {text-align: right;}
div.list { background-color: white; border-top: 1px solid #646464; border-bottom: 1px solid #646464; padding-top: 10px; padding-bottom: 14px;}
div.foot { font: 90%% monospace; color: #787878; padding-top: 4px;}
  </style>

  <link href="/js/jplayer.blue.monday.css" rel="stylesheet" type="text/css" />
  <script type="text/javascript" src="https://code.jquery.com/jquery-1.11.0.min.js"></script>
  <script type="text/javascript" src="/js/jquery.jplayer.min.js"></script>
  <script type="text/javascript">
    $(function() {
      $("#jplayer").jPlayer({
        swfPath: "/js",
		supplied: "mp3",
		wmode: "window"
      });

	  $("a.play").click(function(e) {
	     e.preventDefault();
		 $("#jplayer").jPlayer("setMedia", { mp3: $(this).attr("href") });
		 $("#jplayer").jPlayer("play");
		 return false;
	  });
	});
  </script>
</head>`, pathHtml)

		fmt.Fprintf(rsp, `
<body>
  <h2>Index of %s</h2>`, pathHtml)

  		fmt.Fprintf(rsp, `
  <div id="jplayer" class="jp-jplayer"></div>

  <div id="jp_container_1" class="jp-audio">
       <div class="jp-type-single">
           <div class="jp-gui jp-interface">
                    <ul class="jp-controls">
                        <li><a href="javascript:;" class="jp-play" tabindex="1">play</a></li>
                        <li><a href="javascript:;" class="jp-pause" tabindex="1">pause</a></li>
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

                        <ul class="jp-toggles">
                            <li><a href="javascript:;" class="jp-repeat" tabindex="1" title="repeat">repeat</a></li>
                            <li><a href="javascript:;" class="jp-repeat-off" tabindex="1" title="repeat off">repeat off</a></li>
                        </ul>
                    </div>
                </div>
                <div class="jp-title">
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
		fmt.Fprintf(rsp, `
  <div class="list">
    <table cellpadding="0" cellspacing="0" summary="Directory Listing">
      <thead>
        <tr>
		  <th class="p"></th>
          <th class="n"><a href="%s?sort=%s">Name</a></th>
          <th class="m"><a href="%s?sort=%s">Last Modified</a></th>
          <th class="s"><a href="%s?sort=%s">Size</a></th>
          <th class="t">Type</th>
        </tr>
      </thead>
      <tbody>`, pathHtml,nameSort, pathHtml,dateSort, pathHtml,sizeSort )

		// Add the Parent Directory link if we're above the jail root:
		if startsWith(baseDir, jailRoot) {
			hrefParent := translateForProxy(baseDir) + "/"
			fmt.Fprintf(rsp, `
        <tr>
		  <td class="p"></td>
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
			// Check symlink:
			if (dfi.Mode() & os.ModeSymlink) != 0 {
				if targetPath, err := os.Readlink(dfiPath); err == nil {
					// Find the absolute path of the symlink's target:
					if !path.IsAbs(targetPath) {
						targetPath = path.Join(localPath, targetPath)
					}
					if tdfi, err := os.Stat(targetPath); err == nil {
						// Change to the target so we get its properties instead of the symlink's:
						dfi = tdfi
					}
				}
			}

			href := translateForProxy(dfiPath)

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
        <tr>`)

		    // Add a "play" link if mime type is MP3:
			mt := mime.TypeByExtension(path.Ext(dfi.Name()))
		    if mt == "audio/mpeg" {
				fmt.Fprintf(rsp, `
		  <td class="p"><a href="%s" class="play">&gt;</a></td>`,
					html.EscapeString(href),
				)
			} else {
				fmt.Fprintf(rsp, `
	      <td class="p"></td>`);
			}

		    fmt.Fprintf(rsp, `
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
}

func main() {
	// Expect commandline arguments to specify:
	//   <listen socket type> : "unix" or "tcp" type of socket to listen on
	//   <listen address>     : network address to listen on if "tcp" or path to socket if "unix"
	//   <web root>           : absolute path prefix on URLs
	//   <accel redirect>     : nginx location prefix to internally redirect static file requests to
	//   <filesystem root>    : local fs absolute path to serve files/folders from
	args := os.Args[1:]
	if len(args) != 5 {
		log.Fatal("Required <listen socket type> <listen address> <web root> <accel redirect> <filesystem root> arguments")
		return
	}

	// TODO(jsd): Make this pair of arguments a little more elegant, like "unix:/path/to/socket" or "tcp://:8080"
	socketType, socketAddr := args[0], args[1]
	proxyRoot, accelRedirect, jailRoot = args[2], args[3], args[4]

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
	log.Fatal(http.Serve(l, http.HandlerFunc(indexHtml)))
}
