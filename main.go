package main

import (
	"bufio"
	"fmt"
	"html"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
)

var proxyRoot, jailRoot string

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

	// Check if the /home/ftp/* path is a symlink:
	fi, err := os.Lstat(localPath)
	if fi != nil && (fi.Mode()&os.ModeSymlink) != 0 {
		localDir := path.Dir(localPath)

		// Check if file is a symlink and do 302 redirect:
		linkDest, err := os.Readlink(localPath)
		if err != nil {
			doError(req, rsp, err.Error(), http.StatusBadRequest)
			return
		}

		// NOTE(jsd): Problem here for links outside the /home/ftp/ folder.
		if path.IsAbs(linkDest) {
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
		http.ServeFile(rsp, req, localPath)
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

		// Determine the sorting mode:
		sortBy := sortByName
		sortDir := sortAscending
		switch sortString {
		case "size-desc":
			sortBy = sortBySize
			sortDir = sortDescending
		case "size-asc":
			sortBy = sortBySize
			sortDir = sortAscending
		case "date-desc":
			sortBy = sortByDate
			sortDir = sortDescending
		case "date-asc":
			sortBy = sortByDate
			sortDir = sortAscending
		case "name-desc":
			sortBy = sortByName
			sortDir = sortDescending
		case "name-asc":
			sortBy = sortByName
			sortDir = sortAscending
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
</head>`, pathHtml)

		fmt.Fprintf(rsp, `
<body>
  <h2>Index of %s</h2>`, pathHtml)

		fmt.Fprintf(rsp, `
  <div class="list">
    <table cellpadding="0" cellspacing="0" summary="Directory Listing">
      <thead>
        <tr>
          <th class="n">Name</th>
          <th class="m">Last Modified</th>
          <th class="s">Size</th>
          <th class="t">Type</th>
        </tr>
      </thead>
      <tbody>`)

		// Add the Parent Directory link if we're above the jail root:
		if startsWith(baseDir, jailRoot) {
			hrefParent := translateForProxy(baseDir) + "/"
			fmt.Fprintf(rsp, `
        <tr>
          <td class="n"><a href="%s">Parent Directory/</a></td>
          <td class="m"></td>
          <td class="s"></td>
          <td class="t">Directory</td>
        </tr>`, hrefParent)
		}

		for _, dfi := range fis {
			name := dfi.Name()
			if name[0:1] == "." {
				continue
			}

			href := translateForProxy(path.Join(localPath, name))

			sizeText := ""
			if dfi.IsDir() {
				sizeText = "-"
				name += "/"
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
				html.EscapeString(mime.TypeByExtension(path.Ext(dfi.Name()))),
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
	//   <web root>        : absolute path prefix on URLs
	//   <filesystem root> : local fs absolute path to serve files/folders from
	//   <listen address>  : network address to listen on
	args := os.Args[1:]
	if len(args) != 3 {
		log.Fatal("Required <web root> <filesystem root> <listen address> arguments")
		return
	}
	proxyRoot, jailRoot = args[0], args[1]

	// Create the HTTP server:
	s := &http.Server{
		Addr:    args[2],
		Handler: http.HandlerFunc(indexHtml),
	}
	log.Fatal(s.ListenAndServe())
}
