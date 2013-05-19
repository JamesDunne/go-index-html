package main

import (
    "fmt"
    "html"
    "mime"
    "net/http"
    "net/url"
    "log"
    "os"
    "path"
    "sort"
    "strings"
//    "time"
)

const proxyRoot = "/ftp"
const jailRoot = "/home/ftp"

func startsWith(s, start string) bool {
    if (len(s) < len(start)) { return false }
    return s[0:len(start)] == start;
}

// Remove the start of the string
func removeIfStartsWith(s, start string) string {
    if (!startsWith(s, start)) { return s }
    return s[len(start):]
}

func translateForProxy(s string) string {
    return path.Join(proxyRoot, removeIfStartsWith(s, jailRoot))
}

// For directory entry sorting:

type Entries []os.FileInfo;

func (s Entries) Len()             int { return len(s) }
func (s Entries) Swap(i, j int)    { s[i], s[j] = s[j], s[i] }

type sortBy int32

const (
    sortByNameAsc  sortBy = iota
    sortByNameDesc
    sortByDateAsc
    sortByDateDesc
)

// Sort by last modified time:
type ByDateAsc struct{ Entries }

func (s ByDateAsc) Less(i, j int) bool {
    if (s.Entries[i].IsDir() && !s.Entries[j].IsDir()) {
        return true;
    }
    if (!s.Entries[i].IsDir() && s.Entries[j].IsDir()) {
        return false;
    }

    return s.Entries[i].ModTime().Before(s.Entries[j].ModTime())
}

type ByDateDesc struct{ Entries }

func (s ByDateDesc) Less(i, j int) bool {
    if (s.Entries[i].IsDir() && !s.Entries[j].IsDir()) {
        return true;
    }
    if (!s.Entries[i].IsDir() && s.Entries[j].IsDir()) {
        return false;
    }

    return s.Entries[i].ModTime().After(s.Entries[j].ModTime())
}

// Sort by name:
type ByNameAsc struct{ Entries }

func (s ByNameAsc) Less(i, j int) bool {
    if (s.Entries[i].IsDir() && !s.Entries[j].IsDir()) {
        return true;
    }
    if (!s.Entries[i].IsDir() && s.Entries[j].IsDir()) {
        return false;
    }

    return s.Entries[i].Name() < s.Entries[j].Name()
}

type ByNameDesc struct{ Entries }

func (s ByNameDesc) Less(i, j int) bool {
    if (s.Entries[i].IsDir() && !s.Entries[j].IsDir()) {
        return true;
    }
    if (!s.Entries[i].IsDir() && s.Entries[j].IsDir()) {
        return false;
    }

    return s.Entries[i].Name() > s.Entries[j].Name()
}

// Serves an index.html file for a directory or sends the requested file.
func indexHtml(rsp http.ResponseWriter, req *http.Request) {
    // lighttpd proxy sends us absolute path URLs
    u, err := url.Parse(req.RequestURI)
    if (err != nil) {
        log.Fatal(err)
    }

    relPath := removeIfStartsWith(u.Path, proxyRoot)

    localPath := path.Join(jailRoot, relPath)
    pathLink := path.Join(proxyRoot, relPath)

    // Check if the /home/ftp/* path is a symlink:
    fi, err := os.Lstat(localPath)
    if (fi != nil && (fi.Mode() & os.ModeSymlink) != 0) {
        log.Printf("%s : %s", localPath, fi.Mode().String())
        localDir := path.Dir(localPath)

        // Check if file is a symlink and do 302 redirect:
        linkDest, err := os.Readlink(localPath)
        if (err != nil) {
            http.Error(rsp, err.Error(), http.StatusBadRequest)
            return
        }

        // NOTE(jsd): Problem here for links outside the /home/ftp/ folder.
        if (path.IsAbs(linkDest)) {
            http.Error(rsp, "Symlink points outside of jail", http.StatusBadRequest)
            return
        }

        linkDest = path.Join(localDir, linkDest)
        log.Printf("  symlink : %s", linkDest)

        tp := translateForProxy(linkDest)
        http.Redirect(rsp, req, tp, http.StatusFound)
        return
    }

    // Regular stat
    fi, err = os.Stat(localPath)
    if (err != nil) {
        http.Error(rsp, err.Error(), http.StatusNotFound)
        return
    }

    log.Printf("%s : %s", localPath, fi.Mode().String())

    // Serve the file if it is regular:
    if (fi.Mode().IsRegular()) {
        // Send file:
        http.ServeFile(rsp, req, localPath)
        return
    }

    // Generate an index.html for directories:
    if (fi.Mode().IsDir()) {
        // Build index.html

        baseDir := path.Dir(localPath)
        if (localPath[len(localPath)-1] == '/') {
            baseDir = path.Dir(localPath[0:len(localPath)-1])
        }
        if (baseDir == "") {
            baseDir = "/"
        }

        // Determine what mode to sort by...
        sortBy := sortByNameAsc

        sf, _ := os.Stat(path.Join(localPath, ".index-sort-date-desc"))
        if (sf != nil) {
            sortBy = sortByDateDesc
        }
        sf, _  = os.Stat(path.Join(localPath, ".index-sort-date-asc"))
        if (sf != nil) {
            sortBy = sortByDateAsc
        }
        sf, _  = os.Stat(path.Join(localPath, ".index-sort-name-desc"))
        if (sf != nil) {
            sortBy = sortByNameDesc
        }
        sf, _  = os.Stat(path.Join(localPath, ".index-sort-name-asc"))
        if (sf != nil) {
            sortBy = sortByNameAsc
        }

        // Use query-string 'sort' to override sorting:
        switch u.Query().Get("sort") {
            case "date-desc": sortBy = sortByDateDesc
            case "date-asc":  sortBy = sortByDateAsc
            case "name-desc": sortBy = sortByNameDesc
            case "name-asc":  sortBy = sortByNameAsc
            default:
        }

        // Open the directory to read its contents:
        f, err := os.Open(localPath)
        if (err != nil) {
            http.Error(rsp, err.Error(), http.StatusInternalServerError)
            return
        }

        // Read the directory entries:
        fis, err := f.Readdir(0)
        if (err != nil) {
            http.Error(rsp, err.Error(), http.StatusInternalServerError)
            return
        }

        // Sort the entries by the desired mode:
        switch sortBy {
            default:
                sort.Sort(ByNameAsc{fis})
            case sortByNameDesc:
                sort.Sort(ByNameDesc{fis})
            case sortByNameAsc:
                sort.Sort(ByNameAsc{fis})
            case sortByDateDesc:
                sort.Sort(ByDateDesc{fis})
            case sortByDateAsc:
                sort.Sort(ByDateAsc{fis})
        }

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
        <tr>
      </thead>
      <tbody>`)

        // Add the Parent Directory link if we're above the jail root:
        if (startsWith(baseDir, jailRoot)) {
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
            if (name[0:1] == ".") { continue; }

            href := translateForProxy(path.Join(localPath, name))

            sizeText := ""
            if (dfi.IsDir()) {
                sizeText = "-";
                name += "/"
            } else {
                size := dfi.Size();
                if (size < 1024) {
                    sizeText = fmt.Sprintf("%d  B", size)
                } else if (size < 1024 * 1024) {
                    sizeText = fmt.Sprintf("%.02f KB", float64(size) / 1024.0)
                } else if (size < 1024 * 1024 * 1024) {
                    sizeText = fmt.Sprintf("%.02f MB", float64(size) / (1024.0 * 1024.0))
                } else {
                    sizeText = fmt.Sprintf("%.02f GB", float64(size) / (1024.0 * 1024.0 * 1024.0))
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
        return
    }
}

func main() {
    s := &http.Server{
        Addr:    "localhost:8212",
        Handler: http.HandlerFunc(indexHtml),
    }
    log.Fatal(s.ListenAndServe())
}
