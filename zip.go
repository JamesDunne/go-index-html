// zip.go
package main

import (
	//"archive/tar"
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
)

// Downloads contents of the current directory as a ZIP file, streamed to the client's browser:
func downloadZip(rsp http.ResponseWriter, req *http.Request, u *url.URL, dir *os.FileInfo, localPath string) {
	// Generate a decent filename based on the folder URL:
	fullName := removeIfStartsWith(localPath, jailRoot)
	fullName = removeIfStartsWith(fullName, "/")
	// Translate '/' separators into '-':
	fullName = strings.Map(func(i rune) rune {
		if i == '/' {
			return '-'
		} else {
			return i
		}
	}, fullName)

	var raw_fis []os.FileInfo
	{
		// Open the directory to read its contents:
		df, err := os.Open(localPath)
		if err != nil {
			doError(req, rsp, err.Error(), http.StatusInternalServerError)
			return
		}
		defer df.Close()

		// Read the directory entries:
		raw_fis, err = df.Readdir(0)
		if err != nil {
			doError(req, rsp, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Clear out unzippable files:
	fis := make([]os.FileInfo, 0, len(raw_fis))
	for _, fi := range raw_fis {
		name := fi.Name()
		if fi.IsDir() {
			continue
		}
		if name[0] == '.' {
			continue
		}

		// Dereference symlinks, if applicable:
		fi = followSymlink(localPath, fi)

		// Use this final file:
		fis = append(fis, fi)
	}

	// Make sure filenames are in ascending order:
	sort.Sort(ByName{fis, sortAscending})

	// Start with a 200 status and set up the download:
	h := rsp.Header()
	h.Set("Pragma", "public")
	h.Set("Expires", "0")
	h.Set("Cache-Control", "must-revalidate, post-check=0, pre-check=0")
	h.Set("Cache-Control", "public")
	h.Set("Content-Description", "File Transfer")
	h.Set("Content-Type", "application/octet-stream")
	// NOTE(jsd): Need proper HTTP value encoding here!
	h.Set("Content-Disposition", "attachment; filename=\""+fullName+".zip\"")
	h.Set("Content-Transfer-Encoding", "binary")

	// Here we estimate the final length of the ZIP file being streamed:
	const (
		fileHeaderLen      = 30 // + filename + extra
		dataDescriptorLen  = 16 // four uint32: descriptor signature, crc32, compressed size, size
		directoryHeaderLen = 46 // + filename + extra + comment
		directoryEndLen    = 22 // + comment
	)

	zipLength := 0
	for _, fi := range fis {
		zipLength += fileHeaderLen
		zipLength += len(fi.Name())
		// + extra

		// TODO(jsd): ZIP64 support
		size := fi.Size()
		zipLength += int(size)
		zipLength += dataDescriptorLen

		// Directory entries:
		zipLength += directoryHeaderLen
		zipLength += len(fi.Name())
		// + extra
		// + comment
	}
	zipLength += directoryEndLen

	h.Set("Content-Length", fmt.Sprintf("%d", zipLength))

	rsp.WriteHeader(http.StatusOK)

	// Create a zip stream writing to the HTTP response:
	zw := zip.NewWriter(rsp)
	for _, fi := range fis {
		name := fi.Name()

		fiPath := path.Join(localPath, name)

		// Open the source file for reading:
		lf, err := os.Open(fiPath)
		if err != nil {
			panic(err)
		}
		defer lf.Close()

		// Create the ZIP entry to write to:
		zfh, err := zip.FileInfoHeader(fi)
		if err != nil {
			panic(err)
		}
		// Don't bother compressing the file:
		zfh.Method = zip.Store

		zf, err := zw.CreateHeader(zfh)
		if err != nil {
			panic(err)
		}

		// Copy the file contents into the ZIP:
		_, err = io.Copy(zf, lf)
		if err != nil {
			panic(err)
		}
	}

	// Mark the end of the ZIP stream:
	zw.Close()
	return
}
