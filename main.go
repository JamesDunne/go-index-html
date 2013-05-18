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

const proxyRoot = "/ftp2"

// Remove the start of the string
func removeIfStartsWith(s, start string) string {
    if (s[0:len(start)] == start) {
        return s[len(start):]
    }
    return s
}

func translateForProxy(s string) string {
    return proxyRoot + removeIfStartsWith(s, "/home/ftp")
}

// For directory entry sorting:

type Entries []os.FileInfo;

func (s Entries) Len()             int { return len(s) }
func (s Entries) Swap(i, j int)    { s[i], s[j] = s[j], s[i] }

type sortBy int32

const (
    sortByNameAscAsc  sortBy = iota
    sortByNameAscDesc
    sortByDateAsc
    sortByDateDesc
)

// Sort by last modified time descending:
type ByDateDesc struct{ Entries }

func (s ByDateDesc) Less(i, j int) bool {
    if (s.Entries[i].IsDir()) { return true }
    if (s.Entries[j].IsDir()) { return false }

    return s.Entries[i].ModTime().After(s.Entries[j].ModTime())
}

// Sort by name:
type ByNameAsc struct{ Entries }

func (s ByNameAsc) Less(i, j int) bool {
    if (s.Entries[i].IsDir()) {
        if (s.Entries[j].IsDir()) {
            return s.Entries[i].Name() < s.Entries[j].Name()
        }
        return true
    }
    if (s.Entries[j].IsDir()) { return false }

    return s.Entries[i].Name() < s.Entries[j].Name()
}

// Serves an index.html file for a directory or sends the requested file.
func indexHtml(rsp http.ResponseWriter, req *http.Request) {
    // lighttpd proxy sends us absolute path URLs
    u, err := url.Parse(req.RequestURI)
    if (err != nil) {
        log.Fatal(err)
    }

    p := u.Path
    ftpPath := removeIfStartsWith(p, proxyRoot)
    p = "/ftp" + ftpPath

    // Check if the /home/ftp/* path is a symlink:
    localPath := "/home" + p
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
    localDir := path.Dir(localPath)

    // Serve the file if it is regular:
    if (fi.Mode().IsRegular()) {
        // Send file:
        http.ServeFile(rsp, req, localPath)
        return
    }

    // Generate an index.html for directories:
    if (fi.Mode().IsDir()) {
        // Build index.html

        // Determine what mode to sory by...
        sortBy := sortByNameAscAsc

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
            sortBy = sortByNameAscDesc
        }
        sf, _  = os.Stat(path.Join(localPath, ".index-sort-name-asc"))
        if (sf != nil) {
            sortBy = sortByNameAscAsc
        }

        f, err := os.Open(localPath)
        if (err != nil) {
            http.Error(rsp, err.Error(), http.StatusInternalServerError)
            return
        }

        fis, err := f.Readdir(0)
        if (err != nil) {
            http.Error(rsp, err.Error(), http.StatusInternalServerError)
            return
        }

        // Sort the entries by the desired mode:
        switch sortBy {
            default:
                sort.Sort(ByNameAsc{fis})
            case sortByNameAscAsc:
                sort.Sort(ByNameAsc{fis})
            case sortByDateDesc:
                sort.Sort(ByDateDesc{fis})
        }

        pathHtml := html.EscapeString(p)
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

        hrefParent := translateForProxy(localDir)
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
      <tbody>
        <tr>
          <td class="n"><a href="%s">Parent Directory/</a></td>
          <td class="m"></td>
          <td class="s"></td>
          <td class="t">Directory</td>
        </tr>`, hrefParent)

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
