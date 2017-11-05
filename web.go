// web.go
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io/ioutil"
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

var uiTmpl *template.Template

func endSlash(s string) string {
	if strings.HasSuffix(s, "/") {
		return s
	}
	return s + "/"
}

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
	} else if (mtPath != "") && strings.HasPrefix(u.Path, mtUrl) {
		// URL is under the MT path:
		localPath := path.Join(mtPath, removeIfStartsWith(u.Path, mtUrl))
		http.ServeFile(rsp, req, localPath)
		return nil
	} else if strings.HasPrefix(u.Path, proxyRoot) {
		// URL is under the proxy path:
		return processProxiedRequest(rsp, req, u)
	}

	return nil
}

func processProxiedRequest(rsp http.ResponseWriter, req *http.Request, u *url.URL) *web.Error {
	relPath := removeIfStartsWith(u.Path, proxyRoot)
	localPath := path.Join(jailRoot, relPath)

	// Check if the requested path is a symlink:
	fi, err := os.Lstat(localPath)
	if fi != nil && (fi.Mode()&os.ModeSymlink) != 0 {
		localDir := path.Dir(localPath)

		// Check if file is a symlink and do 302 redirect:
		linkDest, err := os.Readlink(localPath)
		if err != nil {
			return web.AsError(err, http.StatusBadRequest)
		}

		// NOTE(jsd): Problem here for links outside the jail folder.
		if path.IsAbs(linkDest) && !strings.HasPrefix(linkDest, jailRoot) {
			return web.AsError(errors.New("Symlink points outside of jail"), http.StatusBadRequest)
		}

		linkDest = path.Join(localDir, linkDest)
		tp := translateForProxy(linkDest)

		doRedirect(req, rsp, tp, http.StatusFound)
		return nil
	}

	// Regular stat
	fi, err = os.Stat(localPath)
	if err != nil {
		return web.AsError(err, http.StatusNotFound)
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

		return nil
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
			return nil
		}

		return generateIndexHtml(rsp, req, u)
	}

	return nil
}

type IndexTemplateAudioFileJSON struct {
	Href  string `json:"mp3"`
	Name  string `json:"title"`
	Index int    `json:"index"`
}

type IndexTemplateFile struct {
	Href         string
	Name         string
	NameOnly     string
	IsAudio      bool
	IsMultitrack bool
	IsFolder     bool

	Date              string
	SizeHumanReadable template.HTML
	MimeType          string
}

type IndexTemplate struct {
	JplayerUrl string
	MtUrl      string

	Path  string
	Files []*IndexTemplateFile

	HasParent  bool
	ParentHref string

	SortName string
	SortDate string
	SortSize string

	HasAudio   bool
	AudioFiles template.JS

	HasMultitrack     bool
	MultitrackMixJson template.JS
}

func generateIndexHtml(rsp http.ResponseWriter, req *http.Request, u *url.URL) *web.Error {
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
		return web.AsError(err, http.StatusInternalServerError)
	}
	defer f.Close()

	// Read the directory entries:
	fis, err := f.Readdir(0)
	if err != nil {
		return web.AsError(err, http.StatusInternalServerError)
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

	files := make([]*IndexTemplateFile, 0, len(fis))
	audioFiles := make([]*IndexTemplateAudioFileJSON, 0, len(fis))
	multitrackFiles := make([]*IndexTemplateAudioFileJSON, 0, len(fis))

	// Check if there are MP3s in this directory:
	hasMP3s := false
	hasMultitrack := false
	mixJsonPath := ""
	i := 0
	for _, dfi := range fis {
		name := dfi.Name()

		// No hidden files:
		if len(name) > 0 && name[0] == '.' {
			continue
		}

		// Follow symlink if applicable:
		dfi = followSymlink(localPath, dfi)

		dfiPath := path.Join(localPath, name)
		// Folder has a mix.json file means we want a multitrack mixer:
		if name == "mix.json" {
			hasMultitrack = true
			mixJsonPath = dfiPath
			continue
		}

		href := translateForProxy(dfiPath)

		ext := path.Ext(name)
		onlyname := name
		if ext != "" {
			onlyname = name[0 : len(name)-len(ext)]
		}
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

		file := &IndexTemplateFile{
			Href:              href,
			Name:              name,
			NameOnly:          onlyname,
			Date:              dfi.ModTime().Format("2006-01-02 15:04:05 -0700 MST"),
			SizeHumanReadable: template.HTML(strings.Replace(html.EscapeString(sizeText), " ", "&nbsp;", -1)),
			MimeType:          mt,
			IsFolder:          dfi.IsDir(),
		}
		files = append(files, file)

		if !dfi.IsDir() {
			if isMP3(dfi.Name()) {
				hasMP3s = true
				file.IsAudio = true
				audioFiles = append(audioFiles, &IndexTemplateAudioFileJSON{
					Href:  href,
					Name:  onlyname,
					Index: i,
				})
				i++
			}
			if isMultitrack(dfi.Name()) {
				file.IsMultitrack = true
				multitrackFiles = append(multitrackFiles, &IndexTemplateAudioFileJSON{
					Href: href,
					Name: onlyname,
				})
			}
		}
	}

	// Disable extra features if we don't have the supporting code for them:
	if !useJPlayer {
		hasMP3s = false
	}
	if !useMT {
		hasMultitrack = false
	}

	audioFilesJSON, err := json.Marshal(audioFiles)
	if err != nil {
		return web.AsError(err, http.StatusInternalServerError)
	}

	// Load mix.json file:
	multitrackMixJson := []byte(nil)
	if hasMultitrack && mixJsonPath != "" {
		multitrackMixJson, err = ioutil.ReadFile(mixJsonPath)
		if err != nil {
			return web.AsError(err, http.StatusInternalServerError)
		}
	}

	templateData := &IndexTemplate{
		Path:       pathLink,
		Files:      files,
		SortName:   nameSort,
		SortDate:   dateSort,
		SortSize:   sizeSort,
		HasParent:  strings.HasPrefix(baseDir, jailRoot),
		ParentHref: endSlash(translateForProxy(baseDir)),

		JplayerUrl: jplayerUrl,
		HasAudio:   hasMP3s,
		AudioFiles: template.JS(audioFilesJSON),

		MtUrl:             mtUrl,
		HasMultitrack:     hasMultitrack,
		MultitrackMixJson: template.JS(multitrackMixJson),
	}

	// TODO: check Accepts header to reply accordingly (i.e. add JSON support)
	templateName := "index"
	if hasMultitrack {
		templateName = "index-mt"
	}

	// Render index template:
	rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
	rsp.WriteHeader(200)
	if werr := web.AsError(uiTmpl.ExecuteTemplate(rsp, templateName, templateData), http.StatusInternalServerError); werr != nil {
		return werr.AsHTML()
	}
	return nil
}
