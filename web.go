// web.go
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"html"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
)

//import "github.com/JamesDunne/go-util/base"
import "github.com/JamesDunne/go-util/web"

func translateForProxy(s string) string {
	return path.Join(proxyRoot, removeIfStartsWith(s, jailRoot))
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

// Serves an index.html file for a directory or sends the requested file.
func processRequest(rsp http.ResponseWriter, req *http.Request) *web.Error {
	// proxy sends us absolute path URLs
	u, err := url.Parse(req.RequestURI)
	if err != nil {
		return web.AsError(err, 500)
	}

	if (jplayerPath != "") && strings.HasPrefix(u.Path, jplayerUrl) {
		// URL is under the jPlayer path:
		localPath := path.Join(jplayerPath, removeIfStartsWith(u.Path, jplayerUrl))
		http.ServeFile(rsp, req, localPath)
		return nil
	} else if strings.HasPrefix(u.Path, proxyRoot) {
		// URL is under the proxy path:
		processProxiedRequest(rsp, req, u)
		return nil
	}

	return nil
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
		if path.IsAbs(linkDest) && !strings.HasPrefix(linkDest, jailRoot) {
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
	if strings.HasPrefix(baseDir, jailRoot) {
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
